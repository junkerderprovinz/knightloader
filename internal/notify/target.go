// Package notify is the one way a fact about this instance leaves the machine
// on the operator's own instructions: an address they typed, a method they
// picked, headers they wrote and a body they composed, sent when one of the
// events internal/script already publishes actually happens.
//
// IT IS THE SECOND SUBSCRIBER internal/script/bus.go's own doc comment
// anticipated ("a message out to Matrix or ntfy ... it is a Subscribe call,
// not a change to this file"). Nothing is added to the firing side: this
// package takes a script.Firing and turns it into an HTTP request. That is why
// it imports internal/script and internal/httpx and nothing else of this app -
// the same decoupling internal/script states for itself, and the reason a
// dispatcher can be unit-tested against a httptest server with no App anywhere
// near it.
//
// WHAT IT DELIBERATELY IS NOT:
//
//   - It is NOT web/src/lib/notify.ts. That one routes an event to a toast or
//     an OS notification inside one browser tab, never leaves that browser, and
//     is unavailable outright on a plain-HTTP deployment. Two controls with one
//     name and different reach is the confusion this page must not create, so
//     this one is "event targets" (Ereignisziele) everywhere it is named.
//   - It is NOT a mail transport. HTTP only, this wave, by the owner's decision
//     (DECISIONS.md, spec12): ntfy, Gotify, Matrix and any plain webhook are one
//     HTTP subscriber; SMTP is a second credential at rest, a second TLS-mode
//     question, a second test shape and a second set of German copy, and it
//     becomes its own item. There is deliberately no Kind field, no half-wired
//     transport seam and no mail dependency here, because a seam that implies a
//     transport is coming is a promise this package has not made.
//   - It spools NOTHING to disk. A message that could not be delivered is tried
//     as often as its row allows and then dropped. Everything this reports is
//     about a moment that has already passed, and a queue that survives a
//     restart would deliver "a package finished" an hour after the operator
//     watched it finish.
//
// The file split follows what each part answers: target.go is the stored row
// and the rules about it, template.go is what a %%placeholder%% becomes,
// http.go is one request and what came back, dispatcher.go is the bus
// subscriber and the per-target worker behind it.
package notify

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/reconnect"
	"github.com/junkerderprovinz/knightloader/internal/script"
)

// RedactedValue is what Redacted puts in place of a stored header value, and
// the value Merge reads back as "the browser did not retype this".
//
// It is reconnect's own constant rather than a second one spelled the same
// way. Two placeholder constants that happen to agree today are two constants
// that can disagree tomorrow, and the day they do, one of them stops being
// recognised on the way back in and the save writes eight literal stars over a
// real token. Aliasing it here also means the round trip this package
// implements is visibly the same round trip reconnect already documents.
const RedactedValue = reconnect.RedactedPassword

// The methods this build will send, as a closed list.
//
// Closed on purpose, and it is a security decision rather than a limitation:
// POST /api/eventtargets/test fetches an address the caller names, with headers
// and a body the caller wrote, from inside the instance - and on an install
// with no password set, api.go's guard() refuses nothing. A closed method list
// plus a closed scheme list (see Validate) is what keeps that primitive to the
// three verbs a push server actually speaks. DELETE against a LAN admin API is
// not a feature anybody asked for.
const (
	MethodGET  = http.MethodGet
	MethodPOST = http.MethodPost
	MethodPUT  = http.MethodPut
)

// The delivery bounds. Each is "no opinion" at zero rather than "off", which is
// the same reading feed.Subscription's own interval has and for the same
// reason: a spinner that rewrites itself to 3 the moment somebody clears it is
// a spinner nobody can read.
const (
	// DefaultAttempts is how often one message is tried when the row says
	// nothing. Three because the failures worth retrying at all are the ones
	// that clear in seconds (a push server restarting, a dropped packet), and a
	// fourth try minutes later delivers news the operator has already seen on
	// screen.
	DefaultAttempts = 3
	MinAttempts     = 1
	MaxAttempts     = 5

	// DefaultTimeoutSeconds bounds one attempt. Fifteen rather than httpx's own
	// minute: a push server answers in milliseconds, and this timeout is also
	// how long one slow target holds up everything queued behind it on its own
	// worker.
	DefaultTimeoutSeconds = 15
	MinTimeoutSeconds     = 1
	MaxTimeoutSeconds     = 60
)

// Target is one stored row: where a message goes, what it looks like, and which
// events produce one.
//
// It is a plain value held as one field of the persisted settings and handed
// around by copy; nothing in this package mutates a Target it was given. The
// zero value is a row that sends nothing, which is what makes the whole feature
// upgrade-safe: `eventTargets` is absent from every settings.json written
// before this existed, so it decodes to nil and no goroutine is started at all.
type Target struct {
	// ID is assigned by Sanitize and is the join key for two separate things:
	// the health table (which is in memory and keyed by nothing else) and the
	// secret merge (which must be able to tell "the same row, edited" from "a
	// different row that happens to be in the same position").
	ID string `json:"id"`
	// Name is what this target is called in the list, in the status and in the
	// log. It is never sent anywhere - see Expand's placeholder table, which
	// offers the INSTANCE name and not this one, because the far end already
	// knows which of its own topics it is.
	Name string `json:"name"`
	// Enabled is false on every new row, and an absent key decodes to false.
	// Both halves matter: a target is built over several minutes and tested
	// before anybody's phone hears about it, and an upgrade must not switch
	// anything on by itself.
	Enabled bool `json:"enabled"`
	// URL is the full address, http or https only, and may contain
	// %%placeholders%% (expanded with URL escaping - see SlotURL).
	URL string `json:"url"`
	// Method is one of MethodGET/MethodPOST/MethodPUT. Empty means POST, which
	// is what ntfy, Gotify and Matrix all want, so the commonest row needs no
	// opinion here at all.
	Method string `json:"method"`
	// Headers is where a token goes. EVERY VALUE HERE IS A SECRET - see
	// Redacted and Merge - because there is no way to tell an Authorization
	// from an X-Title without reading it, and a rule that guesses is a rule
	// that guesses wrong once.
	Headers map[string]string `json:"headers,omitempty"`
	// Body is the template sent as the request body, empty for none. It is
	// escaped per the Content-Type this row's own headers declare, which is the
	// whole of trap 6: `{"message":"%%task.name%%"}` and a file called
	// `Der "Direktor" 1080p.mkv` is invalid JSON, and the far end then answers
	// 400 about the document rather than about the file.
	Body string `json:"body,omitempty"`
	// Triggers is which events reach this target. EMPTY MEANS NEVER, not
	// "everything": a target that fired on all eleven events the moment it was
	// switched on would send two hundred messages the first time somebody
	// pasted a container, and nobody would have chosen that.
	Triggers []script.Trigger `json:"triggers,omitempty"`
	// Attempts is how often one message is tried. 0 is "no opinion" and
	// resolves to DefaultAttempts; anything else is clamped into
	// MinAttempts..MaxAttempts.
	Attempts int `json:"attempts"`
	// TimeoutSeconds bounds ONE attempt, not the whole delivery. 0 is "no
	// opinion" and resolves to DefaultTimeoutSeconds.
	TimeoutSeconds int `json:"timeoutSeconds"`
}

// ResolvedAttempts is how many tries this row actually gets, with the "no
// opinion" zero resolved. A method rather than arithmetic at each call site so
// the number the worker uses and the number the page shows beside the box
// cannot part company.
func (t Target) ResolvedAttempts() int {
	if t.Attempts <= 0 {
		return DefaultAttempts
	}
	return clamp(t.Attempts, MinAttempts, MaxAttempts)
}

// Timeout is how long one attempt may take, with the "no opinion" zero
// resolved.
func (t Target) Timeout() time.Duration {
	secs := t.TimeoutSeconds
	if secs <= 0 {
		secs = DefaultTimeoutSeconds
	}
	return time.Duration(clamp(secs, MinTimeoutSeconds, MaxTimeoutSeconds)) * time.Second
}

// Wants reports whether this target is subscribed to a trigger. A disabled row
// wants nothing, which is what makes the enabled flag the only thing the
// dispatcher has to re-read when a save lands.
func (t Target) Wants(tr script.Trigger) bool {
	if !t.Enabled {
		return false
	}
	for _, want := range t.Triggers {
		if want == tr {
			return true
		}
	}
	return false
}

// Host is the URL's host, and nothing else of it.
//
// This is what the status route serves and what the collapsed row shows,
// because ntfy and Gotify both take their credential in the QUERY
// (`?token=...`). Serving the whole address in a status table would put that
// token in front of every browser on every poll of a route that exists to say
// "this target is failing" - see redact in http.go for the same rule applied to
// error strings. Empty when the address will not parse, which Validate already
// refuses separately.
func (t Target) Host() string {
	stripped, ok := stripPlaceholders(strings.TrimSpace(t.URL))
	if !ok {
		return ""
	}
	u, err := url.Parse(stripped)
	if err != nil {
		return ""
	}
	return u.Host
}

// Sanitize normalises a list that came from settings.json or from a save. It
// never explains and never refuses: everything it cannot make sense of becomes
// the safe value, and saying what is wrong is Validate's job.
//
// Deliberately NOT dropped here: a row whose address will not parse, and a row
// with an unclosed placeholder. Both are refused at save time by the API
// (validateRows), which is the same rule settings_feeds.go writes down for
// subscriptions - a row that vanishes on save is a row the operator goes on
// believing in, and for this feature that means going on believing they are
// being told about failed downloads.
func Sanitize(in []Target) []Target {
	if len(in) == 0 {
		return nil
	}
	out := make([]Target, 0, len(in))
	for _, t := range in {
		t.Name = strings.TrimSpace(t.Name)
		t.URL = strings.TrimSpace(t.URL)
		// A row with neither an address nor a name is what an untouched Add
		// button produces. Nothing about it can ever be recovered, so it is
		// dropped rather than kept as a permanent blank line; a row with a name
		// and no address yet is somebody mid-edit and is kept.
		if t.URL == "" && t.Name == "" {
			continue
		}
		t.Method = normalizeMethod(t.Method)
		t.Headers = sanitizeHeaders(t.Headers)
		t.Triggers = sanitizeTriggers(t.Triggers)
		if t.Attempts < 0 {
			t.Attempts = 0
		} else if t.Attempts > MaxAttempts {
			t.Attempts = MaxAttempts
		}
		if t.TimeoutSeconds < 0 {
			t.TimeoutSeconds = 0
		} else if t.TimeoutSeconds > MaxTimeoutSeconds {
			t.TimeoutSeconds = MaxTimeoutSeconds
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil
	}
	identify(out)
	return out
}

// normalizeMethod folds whatever arrived into the closed list, defaulting to
// POST.
//
// An unrecognised word becomes POST rather than being kept for Validate to
// complain about, because Sanitize also runs on the way OUT of settings.json on
// a machine nobody is looking at: a hand-edited "DELETE" that survived as far
// as the sender would be a request this build promised never to make.
func normalizeMethod(m string) string {
	switch strings.ToUpper(strings.TrimSpace(m)) {
	case MethodGET:
		return MethodGET
	case MethodPUT:
		return MethodPUT
	default:
		return MethodPOST
	}
}

// sanitizeHeaders drops entries whose name is blank, the same reason
// reconnect's own sanitizeHeaders does: http.Header.Set with an empty key
// produces a header line the far end answers with a parse error rather than a
// message.
//
// The VALUE is deliberately not trimmed. A token is copied and pasted, spaces
// at its edges are legal in a header value, and silently eating them produces a
// 401 from somebody else's server with nothing on this side to explain it -
// exactly the call reconnect makes for the router password.
func sanitizeHeaders(h map[string]string) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, v := range h {
		if k = strings.TrimSpace(k); k != "" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// sanitizeTriggers drops duplicates and anything this build does not fire,
// keeping the operator's own order.
//
// Dropping an unknown trigger is the one thing this sanitiser removes that the
// operator might have typed, and it is safe precisely because it changes
// nothing: a trigger the registry never fires cannot produce a message, so a
// row that keeps it is a row that lies about what it will do. Store validation
// in internal/script makes the identical call for a script bound to a trigger a
// later build defined - see Trigger.Valid's own doc comment. A row that reaches
// here through the API rather than through a hand-edited file has already been
// refused by Validate, with the word named.
func sanitizeTriggers(in []script.Trigger) []script.Trigger {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[script.Trigger]bool, len(in))
	out := make([]script.Trigger, 0, len(in))
	for _, tr := range in {
		if !tr.Valid() || seen[tr] {
			continue
		}
		seen[tr] = true
		out = append(out, tr)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// identify gives every row an ID, the first claim winning.
//
// Lifted straight from proxycfg's own identify and for the identical reason: an
// ID the API has already handed to a client has to survive an edit anywhere
// else in the list, or the health table and the secret merge both start
// pointing at the wrong row the moment somebody deletes the first target.
func identify(out []Target) {
	taken := make(map[string]bool, len(out))
	keep := make([]bool, len(out))
	for i := range out {
		if id := strings.TrimSpace(out[i].ID); id != "" && !taken[id] {
			out[i].ID = id
			taken[id] = true
			keep[i] = true
		}
	}
	next := 1
	for i := range out {
		if keep[i] {
			continue
		}
		for taken[strconv.Itoa(next)] {
			next++
		}
		out[i].ID = strconv.Itoa(next)
		taken[out[i].ID] = true
	}
}

// clamp folds a number into a band. Only ever called with a value already known
// to be non-zero, because zero means "no opinion" everywhere in this file and
// must not be folded into a floor.
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// The reasons a row cannot be used, as values.
//
// Codes rather than sentences for the reason reconnect's ConfigProblem gives at
// length: the sentence is English and the interface is forty-two languages, so
// translating on the server would need the reader's language on a settings
// save and would write the log in whatever the last browser preferred. The code
// crosses the wire, the interface picks the words
// (settings.eventTargets.problem.<code>), and the English sentence below is
// what the log and any non-browser caller get.
//
// The first five are the row being wrong. The rest are the far end being wrong
// and are set by http.go's classify, never by Validate - they share this
// namespace because the page shows them in the same place and a reader does not
// care which layer decided.
const (
	ProblemNoURL          = "noUrl"
	ProblemBadURL         = "badUrl"
	ProblemBadMethod      = "badMethod"
	ProblemBadTemplate    = "badTemplate"
	ProblemUnknownTrigger = "unknownTrigger"

	ProblemDNS         = "dns"
	ProblemRefused     = "refused"
	ProblemTLS         = "tls"
	ProblemTimeout     = "timeout"
	ProblemAuth        = "auth"
	ProblemNotFound    = "notFound"
	ProblemRejected    = "rejected"
	ProblemRateLimited = "rateLimited"
	ProblemServer      = "server"
)

// ErrBadTarget is what every Problem unwraps to, so a caller that only wants to
// know "was this the row's fault" can ask without switching on nine codes.
var ErrBadTarget = errors.New("notify: this event target cannot be used")

// Problem is why a row cannot be used: a code, and the one or two details that
// code needs.
//
// It is an error as well as a value, so validateRows can wrap it with the row
// number and writeValidationError can pull the code back out with errors.As -
// exactly the shape reconnect.ConfigProblem already has on that same path.
type Problem struct {
	Code string
	// N is the 1-based position of the offending item WITHIN the row: which
	// trigger, for ProblemUnknownTrigger. Zero when the problem is about the row
	// as a whole, which is every other code.
	N int
	// Header is the header name whose value would not expand, for
	// ProblemBadTemplate found in a header rather than in the URL or the body.
	Header string
	// Value is the word the operator actually typed, for ProblemBadMethod and
	// ProblemUnknownTrigger.
	//
	// It is the only field here carrying user input and it reaches a log and a
	// page, so it is quoted with %q rather than interpolated bare - the same
	// care ConfigProblem.Method takes for the same reason. It is never a header
	// VALUE: those are secrets, and no code in this file is about one.
	Value string
}

func (p *Problem) Error() string {
	return fmt.Sprintf("%s: %s", ErrBadTarget, p.detail())
}

func (p *Problem) detail() string {
	switch p.Code {
	case ProblemNoURL:
		return "there is no address to send to"
	case ProblemBadURL:
		return "the address is not an http or https address"
	case ProblemBadMethod:
		return fmt.Sprintf("this build sends GET, POST and PUT, not %q", p.Value)
	case ProblemBadTemplate:
		if p.Header != "" {
			return fmt.Sprintf("the %s header has a placeholder that is never closed", p.Header)
		}
		return "a placeholder is never closed"
	case ProblemUnknownTrigger:
		return fmt.Sprintf("event %d is %q, which this build never fires", p.N, p.Value)
	default:
		// Every transport code, and anything a later change forgets to
		// describe. Falling back to one of the sentences above would be a lie
		// with a plausible face, so an unrecognised code says exactly what it
		// knows.
		return fmt.Sprintf("the far end refused it (%s)", p.Code)
	}
}

// Unwrap keeps errors.Is(err, ErrBadTarget) working through validateRows' own
// fmt.Errorf wrapper.
func (p *Problem) Unwrap() error { return ErrBadTarget }

// Validate reports why a row cannot be used, or nil.
//
// Called on the WAY IN, from the settings save, before Sanitize has folded
// anything: this is the last place the word the operator actually typed still
// exists, which is the same reason reconnect validates raw input rather than a
// normalised copy.
//
// It returns *Problem rather than error so a caller cannot accidentally hand a
// typed nil back as a non-nil error interface, which is the one way this shape
// goes wrong.
func Validate(t Target) *Problem {
	raw := strings.TrimSpace(t.URL)
	if raw == "" {
		return &Problem{Code: ProblemNoURL}
	}
	// The placeholders come OUT before the address is parsed, and this is not a
	// nicety: "%%" is not a valid percent-escape, so url.Parse refuses
	// "https://ntfy.example/topic?m=%%task.name%%" outright. Parsing the raw
	// template would therefore refuse every address that uses a placeholder at
	// all, and report it as a malformed address rather than as what it is.
	//
	// It also decides the order of the two checks below it: an unclosed
	// placeholder is reported as an unclosed placeholder, which names the real
	// mistake, instead of as "this is not an http address", which sends somebody
	// to look at the scheme.
	stripped, balanced := stripPlaceholders(raw)
	if !balanced {
		return &Problem{Code: ProblemBadTemplate}
	}
	u, err := url.Parse(stripped)
	switch {
	case err != nil:
		return &Problem{Code: ProblemBadURL}
	case u.Scheme != "http" && u.Scheme != "https":
		// A closed scheme list, and it is the second half of the guard the
		// closed method list is the first half of: file:// and gopher:// are
		// not push servers, and neither is anything else somebody would think
		// to try against a route that sends from inside the instance.
		return &Problem{Code: ProblemBadURL}
	case u.Host == "":
		return &Problem{Code: ProblemBadURL}
	}
	if m := strings.TrimSpace(t.Method); m != "" {
		switch strings.ToUpper(m) {
		case MethodGET, MethodPOST, MethodPUT:
		default:
			return &Problem{Code: ProblemBadMethod, Value: m}
		}
	}
	// An unclosed %% is refused rather than left to expand into nothing.
	// reconnect's expander leaves a broken template visible on purpose and that
	// is right for a router script somebody is debugging by hand; here the
	// template is edited in a form that autosaves 600 ms after a keystroke, and
	// the operator would find out about it when their phone stopped buzzing.
	for name, value := range t.Headers {
		if !balancedTemplate(value) {
			return &Problem{Code: ProblemBadTemplate, Header: strings.TrimSpace(name)}
		}
	}
	if !balancedTemplate(t.Body) {
		return &Problem{Code: ProblemBadTemplate}
	}
	for i, tr := range t.Triggers {
		if !tr.Valid() {
			return &Problem{Code: ProblemUnknownTrigger, N: i + 1, Value: string(tr)}
		}
	}
	return nil
}

// Redacted returns a copy safe to hand to a browser: every non-empty header
// value replaced by RedactedValue.
//
// Every value, with no attempt to tell a token from a title. There is no way to
// look at "X-Priority: 5" and "Authorization: Bearer ..." and decide which is a
// secret without reading them, and a rule that guesses is a rule that hands out
// a Matrix access token the first time somebody names their header something
// this file did not anticipate.
//
// The map is copied rather than edited in place: the caller's Target is a value
// but its Headers map is not, and redacting through the shared map would blank
// the live settings the dispatcher is sending with.
func Redacted(t Target) Target {
	if len(t.Headers) == 0 {
		return t
	}
	out := make(map[string]string, len(t.Headers))
	for k, v := range t.Headers {
		if v != "" {
			v = RedactedValue
		}
		out[k] = v
	}
	t.Headers = out
	return t
}

// Merge puts back the header values Redacted removed, and is the security half
// of this whole feature.
//
// THE CARRY-OVER IS BOUND TO THE DESTINATION, not just to the row. The browser
// is deliberately never shown a header value, and the browser is also the thing
// that types the URL. If a stored token followed a row whose address changed, a
// client that was never allowed to READ that token could repoint the row at a
// machine it controls and have this server post the token there on the next
// event - and Advanced.tsx hands the whole list back as raw editable JSON, so
// that is one paste, not a theory. proxycfg.Merge already writes this rule down
// for proxy passwords in almost these words; this is the same rule with the
// address in place of host/port/username.
//
// So a value is carried only when ALL of:
//
//   - the row id matches (it is the same row, not the row that moved into its
//     position after a delete), and
//   - the trimmed URL is unchanged (it still points at the machine the operator
//     gave the secret to), and
//   - the header NAME is unchanged (Authorization renamed to X-Debug is a
//     different place to put a token), and
//   - the incoming value is exactly RedactedValue (anything else is a value the
//     operator typed, including an empty string, which is how a header is
//     cleared).
//
// When it cannot be carried the placeholder is replaced by an EMPTY value
// rather than left standing. Eight literal stars sent as a bearer token is a
// 401 the operator cannot tell from a wrong token; an empty value is visibly a
// header waiting to be filled in, and Send skips it rather than putting a bare
// `Authorization:` on the wire.
//
// Call it before Sanitize: it matches on the ids the previous Sanitize handed
// out.
func Merge(next, prev []Target) []Target {
	if len(next) == 0 {
		return next
	}
	old := make(map[string]Target, len(prev))
	for _, t := range prev {
		if t.ID != "" {
			old[t.ID] = t
		}
	}
	out := make([]Target, len(next))
	copy(out, next)
	for i := range out {
		if !hasRedacted(out[i].Headers) {
			continue
		}
		before, ok := old[out[i].ID]
		same := ok && strings.TrimSpace(before.URL) == strings.TrimSpace(out[i].URL)
		headers := make(map[string]string, len(out[i].Headers))
		for name, value := range out[i].Headers {
			if value != RedactedValue {
				headers[name] = value
				continue
			}
			if same {
				headers[name] = before.Headers[name]
				continue
			}
			headers[name] = ""
		}
		out[i].Headers = headers
	}
	return out
}

// hasRedacted keeps Merge from rebuilding a map it has nothing to change in,
// which is every row on every save that did not touch a header.
func hasRedacted(h map[string]string) bool {
	for _, v := range h {
		if v == RedactedValue {
			return true
		}
	}
	return false
}

// stripPlaceholders replaces every closed %%name%% with one harmless character
// and reports whether the template was balanced.
//
// Written as a walk rather than a count because the count is wrong on the shape
// that matters: "%%a%%%%b%%" has four markers and is fine, while "100%% done"
// has one and is not, and both have the same parity as something valid. It
// mirrors Expand's own loop exactly, so a template this accepts is a template
// that expander can finish - which is the whole point of it being one function
// rather than a second, nearly identical scanner.
//
// The replacement is a letter and not an empty string, so that an address whose
// HOST is a placeholder still parses as an address with a host rather than as
// one with none.
func stripPlaceholders(s string) (string, bool) {
	if !strings.Contains(s, marker) {
		return s, true
	}
	var b strings.Builder
	b.Grow(len(s))
	for {
		before, rest, found := strings.Cut(s, marker)
		if !found {
			b.WriteString(s)
			return b.String(), true
		}
		_, after, closed := strings.Cut(rest, marker)
		if !closed {
			return "", false
		}
		b.WriteString(before)
		b.WriteByte('x')
		s = after
	}
}

// balancedTemplate is stripPlaceholders' second answer on its own, for the
// header values and the body, where nothing needs parsing afterwards.
func balancedTemplate(s string) bool {
	_, ok := stripPlaceholders(s)
	return ok
}

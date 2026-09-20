// Package notify sends a fact about this instance to an address the operator
// typed, with the method, headers and body they composed, when one of the
// events internal/script publishes happens.
//
// It is a subscriber on script's bus: a script.Firing becomes an HTTP request.
// Nothing is added to the firing side, which is why this imports only
// internal/script and internal/httpx from the app and why a dispatcher can be
// tested against an httptest server with no App near it.
//
// It is not web/src/lib/notify.ts, which routes an event to a toast inside one
// browser tab and never leaves it. Two controls with one name and different
// reach would be confusing, so this one is called "event targets"
// (Ereignisziele) wherever it is named.
//
// HTTP only. ntfy, Gotify, Matrix and any plain webhook are one subscriber;
// SMTP would mean a second credential at rest, a second TLS question, a second
// test shape and a second set of German copy, so there is no Kind field and no
// half-wired transport seam here.
//
// Nothing is spooled to disk. A message that cannot be delivered is tried as
// often as its row allows and then dropped: everything reported here is about a
// moment that has passed, and a queue surviving a restart would announce a
// finished package an hour after the operator watched it finish.
//
// The files split by what they answer: target.go is the stored row and the
// rules about it, template.go is what a %%placeholder%% becomes, http.go is one
// request and what came back, dispatcher.go is the bus subscriber and the
// per-target worker behind it.
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
// what Merge reads back as "the browser did not retype this". It aliases
// reconnect's constant rather than repeating the spelling, because the day two
// copies disagreed one of them would stop being recognised on the way back in
// and a save would write eight literal stars over a real token.
const RedactedValue = reconnect.RedactedPassword

// The methods this build will send, as a closed list.
//
// POST /api/eventtargets/test fetches an address the caller names, with headers
// and a body the caller wrote, from inside the instance, and on an install with
// no password set api.go's guard refuses nothing. The closed method list and
// the closed scheme list in Validate keep that to the three verbs a push server
// speaks.
const (
	MethodGET  = http.MethodGet
	MethodPOST = http.MethodPost
	MethodPUT  = http.MethodPut
)

// The delivery bounds. Zero is "no opinion" rather than "off", so a box the
// operator clears does not rewrite itself to 3 under their cursor.
const (
	// DefaultAttempts is how often one message is tried when the row says
	// nothing. Three, because the failures worth retrying clear in seconds (a
	// push server restarting, a dropped packet) and a fourth try minutes later
	// delivers news the operator has already seen on screen.
	DefaultAttempts = 3
	MinAttempts     = 1
	MaxAttempts     = 5

	// DefaultTimeoutSeconds bounds one attempt. Fifteen rather than httpx's
	// minute: a push server answers in milliseconds, and this is also how long
	// one slow target holds up everything queued behind it on its worker.
	DefaultTimeoutSeconds = 15
	MinTimeoutSeconds     = 1
	MaxTimeoutSeconds     = 60
)

// Target is one stored row: where a message goes, what it looks like, and which
// events produce one.
//
// It is a plain value held as a field of the persisted settings and handed
// around by copy; nothing here mutates a Target it was given. The zero value
// sends nothing, so a settings.json written before this feature existed decodes
// to nil and starts no goroutine.
type Target struct {
	// ID is assigned by Sanitize and joins two things to a row: the in-memory
	// health table, and the secret merge, which has to tell an edited row from
	// a different row that moved into its position.
	ID string `json:"id"`
	// Name is what this target is called in the list, the status and the log.
	// It is never sent: Expand offers the instance name instead, since the far
	// end already knows which of its own topics it is.
	Name string `json:"name"`
	// Enabled is false on a new row and for an absent key. A target is built
	// over several minutes and tested before anybody's phone hears about it,
	// and an upgrade switches nothing on by itself.
	Enabled bool `json:"enabled"`
	// URL is the full address, http or https only, and may contain
	// %%placeholders%%, expanded with URL escaping; see SlotURL.
	URL string `json:"url"`
	// Method is one of MethodGET, MethodPOST and MethodPUT. Empty means POST,
	// which is what ntfy, Gotify and Matrix want.
	Method string `json:"method"`
	// Headers is where a token goes, so every value here is treated as a
	// secret: an Authorization and an X-Title cannot be told apart without
	// reading them, and a rule that guesses guesses wrong once.
	Headers map[string]string `json:"headers,omitempty"`
	// Body is the template sent as the request body, empty for none. It is
	// escaped for the Content-Type this row's headers declare, because
	// `{"message":"%%task.name%%"}` and a file called `Der "Direktor" 1080p.mkv`
	// is invalid JSON and the far end then answers 400 about the document
	// rather than about the file.
	Body string `json:"body,omitempty"`
	// Triggers is which events reach this target. An empty list means none: a
	// target subscribed to everything the moment it was switched on would send
	// hundreds of messages the first time somebody pasted a container.
	Triggers []script.Trigger `json:"triggers,omitempty"`
	// Attempts is how often one message is tried. 0 resolves to
	// DefaultAttempts; anything else is clamped into MinAttempts..MaxAttempts.
	Attempts int `json:"attempts"`
	// TimeoutSeconds bounds one attempt rather than the whole delivery. 0
	// resolves to DefaultTimeoutSeconds.
	TimeoutSeconds int `json:"timeoutSeconds"`
}

// ResolvedAttempts is how many tries this row gets, with the zero resolved. A
// method rather than arithmetic at each call site, so the number the worker
// uses and the number the page shows cannot part company.
func (t Target) ResolvedAttempts() int {
	if t.Attempts <= 0 {
		return DefaultAttempts
	}
	return clamp(t.Attempts, MinAttempts, MaxAttempts)
}

// Timeout is how long one attempt may take, with the zero resolved.
func (t Target) Timeout() time.Duration {
	secs := t.TimeoutSeconds
	if secs <= 0 {
		secs = DefaultTimeoutSeconds
	}
	return time.Duration(clamp(secs, MinTimeoutSeconds, MaxTimeoutSeconds)) * time.Second
}

// Wants reports whether this target is subscribed to a trigger. A disabled row
// wants nothing, so the dispatcher re-reads only the enabled flag on a save.
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

// Host is the URL's host and nothing else of it, which is what the status route
// serves and what the collapsed row shows.
//
// ntfy and Gotify both take their credential in the query (`?token=...`), so
// serving the whole address in a status table would put that token in front of
// every browser on every poll. http.go's redact applies the same rule to error
// strings. Empty when the address will not parse, which Validate refuses.
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

// Sanitize normalises a list that came from settings.json or from a save.
// Everything it cannot make sense of becomes the safe value; saying what is
// wrong is Validate's job.
//
// It keeps a row whose address will not parse and a row with an unclosed
// placeholder. Both are refused at save time by the API, because a row that
// vanishes on save is a row the operator goes on believing in, and here that
// means believing they are being told about failed downloads.
func Sanitize(in []Target) []Target {
	if len(in) == 0 {
		return nil
	}
	out := make([]Target, 0, len(in))
	for _, t := range in {
		t.Name = strings.TrimSpace(t.Name)
		t.URL = strings.TrimSpace(t.URL)
		// A row with neither an address nor a name is what an untouched Add
		// button leaves behind, so it goes rather than staying as a blank line.
		// A row with a name and no address yet is somebody mid-edit.
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
// complain about, because Sanitize also runs when settings.json is read on a
// machine nobody is looking at, and a hand-edited "DELETE" reaching the sender
// would be a request this build promised never to make.
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

// sanitizeHeaders drops entries whose name is blank, since http.Header.Set with
// an empty key produces a line the far end answers with a parse error.
//
// The value is left alone. Spaces at the edges of a header value are legal, and
// eating them silently produces a 401 from somebody else's server with nothing
// on this side to explain it.
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
// keeping the operator's order.
//
// A trigger the registry never fires cannot produce a message, so keeping it
// would only make the row claim something it will not do. internal/script's own
// store makes the same call for a script bound to such a trigger. A row that
// came through the API has already been refused by Validate, with the word
// named.
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

// identify gives every row an ID, the first claim winning. An ID the API has
// handed out has to survive an edit elsewhere in the list, or the health table
// and the secret merge start pointing at the wrong row as soon as somebody
// deletes the first target. proxycfg's identify does the same.
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

// clamp folds a number into a band. Callers resolve the zero first, since zero
// means "no opinion" here and folding it into a floor would lose that.
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
// Codes rather than sentences, the same call reconnect.ConfigProblem makes: the
// interface is translated into dozens of languages, so translating on the
// server would need the reader's language on a settings save and would write
// the log in whatever the last browser preferred. The code crosses the wire,
// the interface picks the words (settings.eventTargets.problem.<code>), and the
// English sentence below is what the log and a non-browser caller get.
//
// The first five are the row being wrong. The rest are the far end being wrong
// and are set by http.go's classify. They share one namespace because the page
// shows them in one place and a reader does not care which layer decided.
const (
	ProblemNoURL          = "noUrl"
	ProblemBadURL         = "badUrl"
	ProblemBadMethod      = "badMethod"
	ProblemBadTemplate    = "badTemplate"
	ProblemUnknownTrigger = "unknownTrigger"

	ProblemDNS      = "dns"
	ProblemRefused  = "refused"
	ProblemTLS      = "tls"
	ProblemTimeout  = "timeout"
	ProblemAuth     = "auth"
	ProblemNotFound = "notFound"
	// The address answered with a 3xx and the client did not follow it: every
	// value in Target.Headers is a secret this package cannot recognise and
	// httpx can only strip the four it knows, see clientFor in http.go. The one
	// thing to do about it is to type the address it points at.
	ProblemRedirect    = "redirect"
	ProblemRejected    = "rejected"
	ProblemRateLimited = "rateLimited"
	ProblemServer      = "server"
)

// ErrBadTarget is what every Problem unwraps to, so a caller that only wants to
// know whether the row was at fault can ask without switching on nine codes.
var ErrBadTarget = errors.New("notify: this event target cannot be used")

// Problem is why a row cannot be used: a code and the details that code needs.
//
// It is an error as well as a value, so validateRows can wrap it with the row
// number and writeValidationError can pull the code back out with errors.As,
// the shape reconnect.ConfigProblem has on that same path.
type Problem struct {
	Code string
	// N is the 1-based position of the offending item within the row, which
	// trigger for ProblemUnknownTrigger. Zero for a problem about the whole row.
	N int
	// Header is the header name whose value would not expand, for a
	// ProblemBadTemplate found in a header rather than the URL or the body.
	Header string
	// Value is the word the operator typed, for ProblemBadMethod and
	// ProblemUnknownTrigger. It is the only field here carrying user input and
	// it reaches a log and a page, so it is quoted with %q. It is never a
	// header value, since those are secrets.
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
		// Every transport code, and anything a later change leaves undescribed.
		// Falling back to one of the sentences above would read plausibly and
		// be wrong, so an unrecognised code says only what it knows.
		return fmt.Sprintf("the far end refused it (%s)", p.Code)
	}
}

// Unwrap keeps errors.Is(err, ErrBadTarget) working through validateRows' own
// fmt.Errorf wrapper.
func (p *Problem) Unwrap() error { return ErrBadTarget }

// Validate reports why a row cannot be used, or nil.
//
// It runs on the settings save before Sanitize has folded anything, since this
// is the last place the word the operator typed still exists. It returns
// *Problem rather than error so a caller cannot hand a typed nil back as a
// non-nil error interface.
func Validate(t Target) *Problem {
	raw := strings.TrimSpace(t.URL)
	if raw == "" {
		return &Problem{Code: ProblemNoURL}
	}
	// The placeholders come out before the address is parsed: "%%" is not a
	// valid percent-escape, so url.Parse refuses
	// "https://ntfy.example/topic?m=%%task.name%%" and every other address that
	// uses a placeholder, reporting it as a malformed address.
	//
	// It also fixes the order of the two checks below, so an unclosed
	// placeholder is reported as one instead of as "this is not an http
	// address", which would send somebody to look at the scheme.
	stripped, balanced := stripPlaceholders(raw)
	if !balanced {
		return &Problem{Code: ProblemBadTemplate}
	}
	u, err := url.Parse(stripped)
	switch {
	case err != nil:
		return &Problem{Code: ProblemBadURL}
	case u.Scheme != "http" && u.Scheme != "https":
		// The other half of the guard the closed method list starts: file://
		// and gopher:// are not push servers, and neither is anything else
		// somebody would try against a route that sends from inside the
		// instance.
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
	// reconnect's expander leaves a broken template visible, which suits a
	// router script somebody is debugging by hand; here the form autosaves
	// shortly after a keystroke and the operator would find out when their
	// phone stopped buzzing.
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

// Redacted returns a copy safe to hand to a browser, with every non-empty
// header value replaced by RedactedValue.
//
// Every value, with no attempt to tell a token from a title: "X-Priority: 5"
// and "Authorization: Bearer ..." cannot be told apart without reading them,
// and a rule that guesses hands out a Matrix access token the first time
// somebody names a header something this file did not anticipate.
//
// The map is copied rather than edited in place. The caller's Target is a value
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

// Merge puts back the header values Redacted removed.
//
// The carry-over is bound to the destination, not only to the row. The browser
// is never shown a header value and is also what types the URL, so if a stored
// token followed a row whose address changed, a client that could not read the
// token could repoint the row at a machine it controls and have this server
// post the token there on the next event. Advanced.tsx hands the whole list
// back as editable JSON, so that is one paste. proxycfg.Merge states the same
// rule for proxy passwords, with host, port and username in place of the
// address.
//
// A value is carried only when every one of these holds:
//
//   - the row id matches, so it is the same row rather than the one that moved
//     into its position after a delete, and
//   - the trimmed URL is unchanged, so it still points at the machine the
//     operator gave the secret to, and
//   - the header name is unchanged, since Authorization renamed to X-Debug is
//     a different place to put a token, and
//   - the incoming value is exactly RedactedValue. Anything else is a value the
//     operator typed, an empty string included, which is how a header is
//     cleared.
//
// When it cannot be carried the placeholder becomes an empty value rather than
// standing. Eight literal stars sent as a bearer token is a 401 the operator
// cannot tell from a wrong token; an empty value is visibly a header waiting to
// be filled in, and Send skips it rather than putting a bare `Authorization:`
// on the wire.
//
// Call it before Sanitize, since it matches on the ids Sanitize handed out.
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
// A walk rather than a count, because counting markers gets the interesting
// shapes wrong: "%%a%%%%b%%" has four and is fine, "100%% done" has one and is
// not. It follows Expand's loop, so a template this accepts is one that
// expander can finish.
//
// The replacement is a letter rather than an empty string, so an address whose
// host is a placeholder still parses as an address with a host.
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

// balancedTemplate is stripPlaceholders' second answer alone, for the header
// values and the body, where nothing needs parsing afterwards.
func balancedTemplate(s string) bool {
	_, ok := stripPlaceholders(s)
	return ok
}

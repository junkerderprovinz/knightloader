package notify

// What a %%placeholder%% is, where it may be written, and what it becomes.
//
// The expander is NOT reconnect's expandVars, and reusing that one unchanged is
// the single most expensive mistake available in this file. It does no escaping
// at all, deliberately and correctly, because a router login form wants the
// password put in verbatim. Here the same value lands in three places with
// three different escaping rules, and the one that bites is JSON:
//
//	{"message":"%%task.name%%"}   +   Der "Direktor" 1080p.mkv
//
// produces a document that is not JSON, and Matrix or Gotify then answers 400
// about the document rather than about the file - so the operator reads
// "M_NOT_JSON" at three in the morning and goes looking at their homeserver.
// The slot therefore comes from the target's OWN Content-Type header, and every
// value is escaped for it.
//
// TWO RULES THAT LOOK ALIKE AND ARE NOT:
//
//   - an UNKNOWN name is left standing exactly as it was typed. That is
//     reconnect's rule (config.go's expandVars) and it is right for the same
//     reason: %%task.nmae%% that expands to nothing produces a message that is
//     subtly wrong and looks perfectly fine, while one that arrives with the
//     typo in it is fixed in ten seconds.
//   - a KNOWN name whose payload this trigger does not carry becomes EMPTY.
//     Firing's own doc comment (script/bus.go) is the authority on which payload
//     belongs to which trigger, and a target bound to reconnect.done must not
//     send the literal text "%%task.name%%" to somebody's phone.
//
// The table below is the only place either rule is applied, and Placeholders()
// serves that same table to the picker - so the list the operator chooses from
// cannot drift from the list this build actually fills in.

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/script"
)

// marker wraps a placeholder name. JDownloader's own spelling, which reconnect
// already speaks, so an operator who has written a reconnect script here knows
// this syntax before they read a word about it.
const marker = "%%"

// Slot is WHERE a value is being put, which decides how it is escaped.
//
// SlotBodyPlain is the zero value on purpose: it escapes nothing, so a Slot
// somebody forgets to pass produces the value verbatim rather than a value
// mangled by whichever rule happened to be first.
type Slot int

const (
	// SlotBodyPlain is a body with no structure to break: ntfy's own plain-text
	// message body is the common case, and a quote or a newline in a file name
	// means nothing to it.
	SlotBodyPlain Slot = iota
	// SlotURL is anywhere in the address. Query-escaped, because that is what a
	// value interpolated into a URL is - a value, not a piece of the URL's own
	// syntax. A file name with a "&" in it would otherwise start a second query
	// parameter.
	SlotURL
	// SlotHeader is a header value. Control characters are STRIPPED rather than
	// escaped, because there is no escape: net/http refuses an invalid header
	// field value at write time, so a task name with a newline in it would fail
	// the whole request with a message naming nothing the operator recognises.
	SlotHeader
	// SlotBodyJSON is inside a JSON string in the body.
	SlotBodyJSON
	// SlotBodyForm is inside an application/x-www-form-urlencoded body.
	SlotBodyForm
)

// Placeholder is one name the picker may offer, as the route serves it.
type Placeholder struct {
	// Name is written WITHOUT the %% wrapper, so the page composes the wrapper
	// once rather than every entry carrying two copies of the same four
	// characters.
	Name string `json:"name"`
	// Scope is which payload it comes out of: "always", "task", "package",
	// "extract", "reconnect", "account" or "captcha". It is what lets the picker
	// group the list instead of showing forty flat rows.
	Scope string `json:"scope"`
	// Triggers is which events carry this name. EMPTY MEANS EVERY TRIGGER, which
	// is only true of the "always" scope. It is what the page uses to say "none
	// of the events you ticked carries this, so it will always be empty" - the
	// one warning that turns a silent empty message into a fixable mistake.
	Triggers []script.Trigger `json:"triggers,omitempty"`
}

// The scopes, as constants so the table below and any future reader agree about
// the spelling.
const (
	ScopeAlways    = "always"
	ScopeTask      = "task"
	ScopePackage   = "package"
	ScopeExtract   = "extract"
	ScopeReconnect = "reconnect"
	ScopeAccount   = "account"
	ScopeCaptcha   = "captcha"
)

// entry is one row of the table: what the picker is told, and how the value is
// produced. The two live together so that adding a placeholder is one edit and
// cannot produce a name the picker offers and the expander does not know.
type entry struct {
	Placeholder
	value func(f script.Firing, instanceName string) string
}

// taskTriggers is every trigger that CAN carry a task.
//
// "Can", not "does": Firing's doc comment lists task.done, task.failed,
// link.added, checksum.failed and manual as always carrying one, and says the
// app also sets it alongside Extract and Captcha where it could name the
// download. So a target bound only to extract.done may or may not get a task
// name depending on whether the extraction knew which download it came from,
// and the honest thing for the picker to say is "this event can carry it"
// rather than either half-truth.
var taskTriggers = []script.Trigger{
	script.TriggerTaskDone, script.TriggerTaskFailed, script.TriggerLinkAdded,
	script.TriggerChecksumFailed, script.TriggerOnDemand,
	script.TriggerExtractDone, script.TriggerCaptchaPending,
}

// table is every placeholder this build knows, in the order the picker shows
// them: what is always there first, then one block per payload.
var table = buildTable()

func buildTable() []entry {
	out := []entry{
		{Placeholder{Name: "event", Scope: ScopeAlways}, func(f script.Firing, _ string) string { return string(f.Trigger) }},
		// RFC3339 rather than a formatted local time: this string is read by
		// somebody else's server as often as by a person, and a machine-readable
		// stamp can always be reformatted where it lands. The reverse is not
		// true.
		{Placeholder{Name: "time", Scope: ScopeAlways}, func(f script.Firing, _ string) string { return f.At.Format(time.RFC3339) }},
		// The INSTANCE name, never the target's own. The far end already knows
		// which of its topics it is; what it cannot know is which of somebody's
		// three boxes just spoke.
		{Placeholder{Name: "instance", Scope: ScopeAlways}, func(_ script.Firing, name string) string { return name }},
		{Placeholder{Name: "queue.files", Scope: ScopeAlways}, func(f script.Firing, _ string) string { return itoa(f.Queue.Files) }},
		{Placeholder{Name: "queue.disabled", Scope: ScopeAlways}, func(f script.Firing, _ string) string { return itoa(f.Queue.Disabled) }},
		{Placeholder{Name: "queue.running", Scope: ScopeAlways}, func(f script.Firing, _ string) string { return itoa(f.Queue.Running) }},
	}
	out = append(out,
		taskEntry("task.id", func(v script.TaskView) string { return v.ID }),
		taskEntry("task.name", func(v script.TaskView) string { return v.Name }),
		taskEntry("task.url", func(v script.TaskView) string { return v.URL }),
		taskEntry("task.host", func(v script.TaskView) string { return v.Host }),
		taskEntry("task.package", func(v script.TaskView) string { return v.Package }),
		taskEntry("task.status", func(v script.TaskView) string { return v.Status }),
		taskEntry("task.size", func(v script.TaskView) string { return itoa64(v.Size) }),
		taskEntry("task.loaded", func(v script.TaskView) string { return itoa64(v.Loaded) }),
		taskEntry("task.speed", func(v script.TaskView) string { return itoa64(v.Speed) }),
		taskEntry("task.error", func(v script.TaskView) string { return v.Error }),
		taskEntry("task.reason", func(v script.TaskView) string { return v.Reason }),
		// Whole percent, not two decimals: this ends up in a notification
		// somebody reads on a lock screen, and "97.3149%" is not more useful
		// than "97".
		taskEntry("task.progress", func(v script.TaskView) string { return strconv.Itoa(int(v.ProgressPct())) }),
	)
	out = append(out,
		packageEntry("package.name", func(v script.PackageView) string { return v.Name }),
		packageEntry("package.files", func(v script.PackageView) string { return itoa(v.Files) }),
		packageEntry("package.done", func(v script.PackageView) string { return itoa(v.Done) }),
		packageEntry("package.failed", func(v script.PackageView) string { return itoa(v.Failed) }),
		packageEntry("package.skipped", func(v script.PackageView) string { return itoa(v.Skipped) }),
		packageEntry("package.disabled", func(v script.PackageView) string { return itoa(v.Disabled) }),
		packageEntry("package.bytes", func(v script.PackageView) string { return itoa64(v.Bytes) }),
	)
	out = append(out,
		extractEntry("extract.name", func(v script.ExtractView) string { return v.Name }),
		extractEntry("extract.dir", func(v script.ExtractView) string { return v.Dir }),
		extractEntry("extract.package", func(v script.ExtractView) string { return v.Package }),
		extractEntry("extract.ok", func(v script.ExtractView) string { return btoa(v.OK) }),
		extractEntry("extract.error", func(v script.ExtractView) string { return v.Error }),
		extractEntry("extract.files", func(v script.ExtractView) string { return itoa(v.Files) }),
		extractEntry("extract.bytes", func(v script.ExtractView) string { return itoa64(v.Bytes) }),
	)
	out = append(out,
		reconnectEntry("reconnect.ok", func(v script.ReconnectView) string { return btoa(v.OK) }),
		reconnectEntry("reconnect.changed", func(v script.ReconnectView) string { return btoa(v.Changed) }),
		reconnectEntry("reconnect.from", func(v script.ReconnectView) string { return v.From }),
		reconnectEntry("reconnect.to", func(v script.ReconnectView) string { return v.To }),
		reconnectEntry("reconnect.error", func(v script.ReconnectView) string { return v.Error }),
		reconnectEntry("reconnect.checks", func(v script.ReconnectView) string { return itoa(v.Checks) }),
	)
	out = append(out,
		accountEntry("account.service", func(v script.AccountView) string { return v.Service }),
		// The account id, for somebody with two keys on one service. Named "id"
		// rather than "account" because "%%account.account%%" reads like a
		// mistake even when it is not.
		accountEntry("account.id", func(v script.AccountView) string { return v.Account }),
		accountEntry("account.label", func(v script.AccountView) string { return v.Label }),
		accountEntry("account.tier", func(v script.AccountView) string { return v.Tier }),
		accountEntry("account.expiry", func(v script.AccountView) string { return v.Expiry }),
	)
	out = append(out,
		captchaEntry("captcha.id", func(v script.CaptchaView) string { return v.ID }),
		captchaEntry("captcha.host", func(v script.CaptchaView) string { return v.Host }),
		captchaEntry("captcha.kind", func(v script.CaptchaView) string { return v.Kind }),
		captchaEntry("captcha.prompt", func(v script.CaptchaView) string { return v.Prompt }),
		captchaEntry("captcha.expires", func(v script.CaptchaView) string { return v.ExpiresAt }),
	)
	return out
}

// The six constructors below exist so that "this payload is nil on this
// trigger" is written once per payload rather than once per field. Forty-odd
// nil checks copied by hand is forty-odd chances to write one of them the wrong
// way round, and the wrong way round here is a nil dereference on a goroutine
// that is delivering somebody's notification.
func taskEntry(name string, get func(script.TaskView) string) entry {
	return entry{
		Placeholder{Name: name, Scope: ScopeTask, Triggers: taskTriggers},
		func(f script.Firing, _ string) string {
			if f.Task == nil {
				return ""
			}
			return get(*f.Task)
		},
	}
}

func packageEntry(name string, get func(script.PackageView) string) entry {
	return entry{
		Placeholder{Name: name, Scope: ScopePackage, Triggers: []script.Trigger{script.TriggerPackageDone}},
		func(f script.Firing, _ string) string {
			if f.Package == nil {
				return ""
			}
			return get(*f.Package)
		},
	}
}

func extractEntry(name string, get func(script.ExtractView) string) entry {
	return entry{
		Placeholder{Name: name, Scope: ScopeExtract, Triggers: []script.Trigger{script.TriggerExtractDone}},
		func(f script.Firing, _ string) string {
			if f.Extract == nil {
				return ""
			}
			return get(*f.Extract)
		},
	}
}

func reconnectEntry(name string, get func(script.ReconnectView) string) entry {
	return entry{
		Placeholder{Name: name, Scope: ScopeReconnect, Triggers: []script.Trigger{script.TriggerReconnectDone}},
		func(f script.Firing, _ string) string {
			if f.Reconnect == nil {
				return ""
			}
			return get(*f.Reconnect)
		},
	}
}

func accountEntry(name string, get func(script.AccountView) string) entry {
	return entry{
		Placeholder{Name: name, Scope: ScopeAccount, Triggers: []script.Trigger{script.TriggerAccountExpired}},
		func(f script.Firing, _ string) string {
			if f.Account == nil {
				return ""
			}
			return get(*f.Account)
		},
	}
}

func captchaEntry(name string, get func(script.CaptchaView) string) entry {
	return entry{
		Placeholder{Name: name, Scope: ScopeCaptcha, Triggers: []script.Trigger{script.TriggerCaptchaPending}},
		func(f script.Firing, _ string) string {
			if f.Captcha == nil {
				return ""
			}
			return get(*f.Captcha)
		},
	}
}

func itoa(v int) string     { return strconv.Itoa(v) }
func itoa64(v int64) string { return strconv.FormatInt(v, 10) }

// btoa is "true"/"false" and not "yes"/"no": these values are as likely to be
// read by a JSON body as by a person, and `{"ok":%%extract.ok%%}` has to produce
// a document rather than a sentence.
func btoa(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// byName is the lookup Expand uses, built once. Its KEYS are what "known"
// means: a name in here expands (to empty, if this firing has no payload for
// it), a name not in here is left standing.
var byName = func() map[string]entry {
	m := make(map[string]entry, len(table))
	for _, e := range table {
		m[e.Name] = e
	}
	return m
}()

// Placeholders is the picker's list, served by GET /api/eventtargets/placeholders.
//
// It is the same table the expander reads, which is the whole point: a picker
// built from a hand-copied list offers names the server never fills in and
// omits ones it does - the identical argument script.AllTriggers makes for the
// trigger vocabulary. A fresh slice on every call, so a caller may sort it.
func Placeholders() []Placeholder {
	out := make([]Placeholder, 0, len(table))
	for _, e := range table {
		out = append(out, e.Placeholder)
	}
	return out
}

// BodySlot decides how a body's placeholders are escaped, from the target's own
// Content-Type header.
//
// From the header the operator wrote rather than from a separate "format"
// field, because those two can disagree and the header is the one the far end
// believes. A row that declares application/json and is escaped as plain text
// is the exact failure this function exists to prevent; a row that declares
// nothing gets plain text, which is what ntfy's own message body is.
func BodySlot(headers map[string]string) Slot {
	ct := ""
	for name, value := range headers {
		if strings.EqualFold(strings.TrimSpace(name), "Content-Type") {
			ct = strings.ToLower(value)
			break
		}
	}
	switch {
	case strings.Contains(ct, "json"):
		return SlotBodyJSON
	case strings.Contains(ct, "x-www-form-urlencoded"):
		return SlotBodyForm
	default:
		return SlotBodyPlain
	}
}

// Expand replaces every %%name%% in s with the value this firing carries,
// escaped for the slot it is going into.
//
// Names are matched case-insensitively and with surrounding blank space
// ignored, matching reconnect's expander, so a template copied out of a
// half-remembered example still works.
func Expand(s string, slot Slot, f script.Firing, instanceName string) string {
	if !strings.Contains(s, marker) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for {
		before, rest, found := strings.Cut(s, marker)
		if !found {
			b.WriteString(s)
			return b.String()
		}
		name, after, closed := strings.Cut(rest, marker)
		if !closed {
			// An unclosed %% is not a placeholder. Validate refuses a row that
			// has one, so reaching this is a hand-edited settings.json - and the
			// broken template is left visible rather than swallowed, exactly as
			// reconnect's expander leaves it.
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(before)
		if e, ok := byName[strings.ToLower(strings.TrimSpace(name))]; ok {
			b.WriteString(escape(e.value(f, instanceName), slot))
		} else {
			// The typo, kept. See the file comment: a name that expands to
			// nothing produces a message that is quietly wrong.
			b.WriteString(marker)
			b.WriteString(name)
			b.WriteString(marker)
		}
		s = after
	}
}

// escape makes one value safe for one slot. The literal text around the
// placeholders is never touched: it is the operator's own document, and
// escaping their JSON braces would break the only thing they wrote by hand.
func escape(v string, slot Slot) string {
	switch slot {
	case SlotURL, SlotBodyForm:
		return url.QueryEscape(v)
	case SlotHeader:
		return stripControl(v)
	case SlotBodyJSON:
		return jsonInner(v)
	default:
		return v
	}
}

// stripControl removes what net/http will not put on the wire.
//
// Removed rather than replaced with a space: a task name with a newline in it
// is a name with a newline in it, and the fix is to not send the newline, not
// to invent a character the file name never had. Everything from 0x20 up
// (0x7f aside) is a legal header field value byte, including the whole of
// UTF-8's high range, so a German file name goes through untouched.
func stripControl(v string) string {
	if strings.IndexFunc(v, isControl) < 0 {
		return v
	}
	return strings.Map(func(r rune) rune {
		if isControl(r) {
			return -1
		}
		return r
	}, v)
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

// jsonInner is v escaped for the INSIDE of a JSON string, without the quotes.
//
// encoding/json rather than a hand-written replacer, because the hand-written
// one is always missing something: the tab, the U+2028 line separator that
// breaks a JavaScript parser reading the body back, the lone surrogate that a
// strict decoder refuses. Marshal of a string cannot fail, so the error is
// dropped rather than checked into a value there is nothing sensible to do
// with.
func jsonInner(v string) string {
	b, err := json.Marshal(v)
	if err != nil || len(b) < 2 {
		return ""
	}
	return string(b[1 : len(b)-1])
}

// ExpandHeaders is every header value expanded for the header slot, with the
// names left exactly as typed.
//
// The NAME is never expanded. A templated header name is a way to build a
// header called "X-%%task.status%%" whose spelling changes per message, which
// nothing wants and which no far end can be configured against; leaving it
// alone also means the name in the row is the name on the wire, which is what
// makes Merge's "same header name" rule mean anything.
func ExpandHeaders(h map[string]string, f script.Firing, instanceName string) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for name, value := range h {
		out[name] = Expand(value, SlotHeader, f, instanceName)
	}
	return out
}

// SampleFiring is the made-up event the test button sends.
//
// It fills EVERY payload, which no real firing ever does, and that is
// deliberate: the point of the test is to see what the template produces, and a
// sample carrying only a task would silently render half of somebody's Matrix
// body as empty and teach them their placeholders were wrong. The names are
// obviously invented so that a message which reaches a real chat room reads as
// a test rather than as a download nobody remembers starting.
//
// The task name carries a double quote on purpose. It is the character that
// breaks a JSON body (trap 6), so a test against a Matrix or Gotify row
// exercises the escaping rather than only the connection.
func SampleFiring(now time.Time) script.Firing {
	return script.Firing{
		Trigger: script.TriggerTaskDone,
		At:      now,
		Queue:   script.QueueView{Files: 3, Disabled: 1, Running: 1},
		Task: &script.TaskView{
			ID: "test-task", Name: `A "test" file.mkv`, URL: "https://example.invalid/a-test-file.mkv",
			Host: "example.invalid", Package: "Test package", Status: "done",
			Size: 1024 * 1024 * 700, Loaded: 1024 * 1024 * 700, CreatedAt: now,
		},
		Package: &script.PackageView{
			Name: "Test package", Files: 3, Done: 3, Bytes: 1024 * 1024 * 700,
		},
		Extract: &script.ExtractView{
			JobID: "test-job", Name: "test-archive.rar", Dir: "/downloads/Test package",
			Package: "Test package", OK: true, Files: 3, Bytes: 1024 * 1024 * 700,
		},
		Reconnect: &script.ReconnectView{OK: true, Changed: true, From: "203.0.113.7", To: "203.0.113.9", Checks: 2},
		Account:   &script.AccountView{Service: "example", Label: "Test account", Tier: "premium", Expiry: now.Format(time.RFC3339)},
		Captcha:   &script.CaptchaView{ID: "test-captcha", Host: "example.invalid", Kind: "image", Prompt: "Type what you see"},
	}
}

// ExpandURL is the address with its placeholders filled in. Split out because
// both http.Send and any caller that wants to show what WOULD be sent need the
// identical string, and two call sites building it separately is how the test
// panel ends up describing a request the sender never made.
func ExpandURL(t Target, f script.Firing, instanceName string) string {
	return Expand(strings.TrimSpace(t.URL), SlotURL, f, instanceName)
}

// ExpandBody is the body with its placeholders filled in, escaped for whatever
// the row's own Content-Type says it is. Empty for a GET: net/http would send
// one, and a body on a GET is a request most servers and every proxy in between
// treat differently from the one the operator thinks they wrote.
func ExpandBody(t Target, f script.Firing, instanceName string) string {
	if normalizeMethod(t.Method) == http.MethodGet {
		return ""
	}
	return Expand(t.Body, BodySlot(t.Headers), f, instanceName)
}

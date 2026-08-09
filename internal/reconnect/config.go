package reconnect

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// The methods a reconnect can use. Anything else is normalised to MethodNone by
// Sanitize.
const (
	MethodNone    = "none"    // explicitly switched off
	MethodCommand = "command" // run an external program
	MethodHTTP    = "http"    // replay a list of HTTP requests (JD calls this LiveHeader/Curl)
	MethodUPnP    = "upnp"    // ask the gateway itself, over SSDP and SOAP
	MethodScript  = "script"  // hand a user-written script to an interpreter
)

// The variables every string in a Config may contain. They are written the way
// JDownloader writes them so a script copied out of a JD reconnect profile keeps
// working, and they are matched case-insensitively.
const (
	VarIP       = "ip"
	VarUsername = "username"
	VarPassword = "password"

	// VarRouter is the router's own address on the LAN, which is a different
	// thing from VarIP and the two must never be conflated: VarIP is the public
	// address the box had before the run, and pointing a login request at that
	// instead of at the gateway sends the router password out to the internet.
	// JDownloader spells this one %%%routerip%%%.
	VarRouter = "router"
)

// RedactedPassword is what Redacted puts in place of the router password, and
// the value WithSecretsFrom reads as "the user did not retype it". It is a
// visible placeholder rather than an empty string because an empty string has to
// keep meaning "clear the password" - otherwise a stored password can never be
// removed through the settings form.
const RedactedPassword = "********"

// The failures a caller is expected to tell apart. ErrNotConfigured means the
// user has not finished setting reconnect up, ErrUnchanged means the router did
// as it was told and the address stayed put anyway, and ErrNoAddress means the
// check URL answered with something that holds no address at all - three
// different things to fix, so they must not arrive as one opaque error.
var (
	ErrNotConfigured = errors.New("reconnect: not configured")
	ErrUnchanged     = errors.New("reconnect: the address did not change")
	ErrNoAddress     = errors.New("reconnect: no IP address in the check response")
)

// Request is one step of the HTTP method. Every field may contain variables.
type Request struct {
	Method  string            `json:"method"`            // empty means GET
	URL     string            `json:"url"`               // required
	Headers map[string]string `json:"headers,omitempty"` // "Host" is honoured, see below
	Body    string            `json:"body,omitempty"`
}

// Config is the user-visible reconnect configuration. It is a plain value so it
// can be stored as one field of the persisted settings and handed around by
// copy; nothing in this package mutates a Config it was given.
type Config struct {
	Method string `json:"method"`

	// Username and Password are the router login, substituted wherever the
	// %%username%% and %%password%% variables appear.
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`

	// Router is the router's address on the LAN, substituted wherever the
	// %%router%% variable appears. It is stored without a scheme so a template
	// can put it anywhere in a URL; Sanitize strips one if the user pasted the
	// address straight out of the browser's address bar.
	Router string `json:"router,omitempty"`

	// Command and Args are the external program for MethodCommand.
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`

	// Requests are replayed in order for MethodHTTP.
	Requests []Request `json:"requests,omitempty"`

	// Interpreter, InterpreterArgs and Script are MethodScript. The script is
	// written to a private temporary file and the interpreter is handed its
	// path, which is why there is no field here for a shell command line: a
	// reconnect that builds one would have to quote the router password into it.
	Interpreter     string   `json:"interpreter,omitempty"`
	InterpreterArgs []string `json:"interpreterArgs,omitempty"`
	Script          string   `json:"script,omitempty"`

	// UPnPLocation pins the gateway's device description URL and skips SSDP
	// discovery. It is optional and normally empty: the point of MethodUPnP is
	// that it works without the user knowing anything about their router. It
	// exists for the network where the multicast search is filtered but the
	// gateway is perfectly reachable, which discovery alone can never fix.
	UPnPLocation string `json:"upnpLocation,omitempty"`

	// CheckURL is fetched to learn the current public address. It has no
	// default: a self-hosted download manager should not start reporting its
	// address to a third-party service the user never chose.
	CheckURL string `json:"checkUrl,omitempty"`

	// The wait is stored in seconds rather than as a time.Duration, which JSON
	// would render as an eleven-digit nanosecond count that nobody can read or
	// hand-edit in settings.json.
	IntervalSeconds int `json:"intervalSeconds"`
	TimeoutSeconds  int `json:"timeoutSeconds"`
}

// The bounds Sanitize enforces. The interval floor exists because a poll loop
// with no floor turns an IP-check service into a target; the timeout ceiling
// exists because a reconnect that is still waiting a quarter of an hour later
// has failed, whatever the user typed.
const (
	defaultIntervalSeconds = 5
	minIntervalSeconds     = 1
	maxIntervalSeconds     = 60

	defaultTimeoutSeconds = 120
	minTimeoutSeconds     = 5
	maxTimeoutSeconds     = 15 * 60
)

// Defaults returns the configuration a fresh install starts with: switched off,
// with sensible timing already filled in so the settings form has numbers to
// show rather than two zeroes.
func Defaults() Config {
	return Config{
		Method:          MethodNone,
		IntervalSeconds: defaultIntervalSeconds,
		TimeoutSeconds:  defaultTimeoutSeconds,
	}
}

// Interval is how long to wait between two address checks.
func (c Config) Interval() time.Duration {
	return time.Duration(c.IntervalSeconds) * time.Second
}

// Timeout is how long to keep checking before giving up on the address.
func (c Config) Timeout() time.Duration {
	return time.Duration(c.TimeoutSeconds) * time.Second
}

// Sanitize normalises a configuration that came from a settings file or an API
// request. It never rejects: everything it cannot make sense of becomes the safe
// value, and the things that must be spelled out are Validate's job to report.
func Sanitize(c Config) Config {
	switch strings.ToLower(strings.TrimSpace(c.Method)) {
	case MethodCommand, "external", "batch":
		c.Method = MethodCommand
	case MethodHTTP, "liveheader", "curl":
		c.Method = MethodHTTP
	case MethodUPnP, "upnpreconnect", "ssdp", "igd":
		c.Method = MethodUPnP
	case MethodScript, "interpreter":
		c.Method = MethodScript
	default:
		// A method we cannot identify becomes "off" rather than falling through
		// to whichever branch happens to be first: guessing would fire commands
		// at a router the user never pointed us at.
		c.Method = MethodNone
	}

	c.Username = strings.TrimSpace(c.Username)
	// The password is deliberately not trimmed. Spaces are legal in a password,
	// and quietly removing them produces a login that fails with a message from
	// the router that never mentions the reason.
	c.Command = strings.TrimSpace(c.Command)
	c.CheckURL = strings.TrimSpace(c.CheckURL)
	c.Interpreter = strings.TrimSpace(c.Interpreter)
	c.UPnPLocation = strings.TrimSpace(c.UPnPLocation)
	c.Router = sanitizeRouter(c.Router)
	// The script body is deliberately not trimmed of anything but surrounding
	// blank space: an interpreter that cares about indentation would be handed a
	// different program than the one the user wrote and saved.
	c.Script = strings.TrimSpace(c.Script)

	if len(c.Requests) > 0 {
		reqs := make([]Request, 0, len(c.Requests))
		for _, q := range c.Requests {
			q.Method = strings.ToUpper(strings.TrimSpace(q.Method))
			if q.Method == "" {
				q.Method = http.MethodGet
			}
			q.URL = strings.TrimSpace(q.URL)
			q.Headers = sanitizeHeaders(q.Headers)
			reqs = append(reqs, q)
		}
		c.Requests = reqs
	}

	c.IntervalSeconds = clamp(c.IntervalSeconds, defaultIntervalSeconds, minIntervalSeconds, maxIntervalSeconds)
	c.TimeoutSeconds = clamp(c.TimeoutSeconds, defaultTimeoutSeconds, minTimeoutSeconds, maxTimeoutSeconds)
	// A timeout below the interval would end the run before a single check ran,
	// which looks exactly like a router that ignored us.
	c.TimeoutSeconds = max(c.TimeoutSeconds, c.IntervalSeconds)
	return c
}

// clamp folds an out-of-range or unset number into the allowed band.
func clamp(v, fallback, lo, hi int) int {
	if v <= 0 {
		v = fallback
	}
	if v < lo {
		v = lo
	}
	if v > hi {
		v = hi
	}
	return v
}

// sanitizeRouter reduces whatever the user pasted into the router field to a
// bare host, optionally with a port.
//
// The failure it prevents is a doubled scheme. Templates write the variable as
// "http://%%router%%/login.cgi", and somebody who copies the address out of the
// browser's address bar pastes "http://192.168.1.1/" - which expands to
// "http://http://192.168.1.1//login.cgi". That is not a URL, so the request
// fails before it is sent, and the message names a parse error rather than the
// field with the extra scheme in it.
func sanitizeRouter(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, scheme := range []string{"http://", "https://"} {
		if len(s) >= len(scheme) && strings.EqualFold(s[:len(scheme)], scheme) {
			s = s[len(scheme):]
			break
		}
	}
	// Only the path is cut, not a ":port" or an IPv6 literal's brackets.
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// sanitizeHeaders drops entries whose name is blank. A nameless header cannot be
// sent, and http.Header.Set with an empty key produces a header line the router
// answers to with a parse error rather than a login.
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

// Validate reports why a configuration cannot be run, so the caller can say what
// is missing instead of starting a reconnect that quietly does nothing.
func (c Config) Validate() error {
	switch c.Method {
	case MethodCommand:
		if strings.TrimSpace(c.Command) == "" {
			return &ConfigProblem{Code: ProblemNoCommand}
		}
	case MethodHTTP:
		if len(c.Requests) == 0 {
			return &ConfigProblem{Code: ProblemNoRequests}
		}
		for i, q := range c.Requests {
			// An entry with no URL is refused rather than skipped. Skipping it
			// would leave a script that logs in and never reboots, and that
			// failure shows up as "the address never changes" days later.
			if strings.TrimSpace(q.URL) == "" {
				return &ConfigProblem{Code: ProblemRequestNoURL, N: i + 1}
			}
		}
	case MethodUPnP:
		// Nothing is required. That is the whole reason this method exists: the
		// gateway is found by asking the network, so a user who knows none of
		// their router's details can still reconnect.
	case MethodScript:
		if strings.TrimSpace(c.Interpreter) == "" {
			return &ConfigProblem{Code: ProblemNoInterpreter}
		}
		if strings.TrimSpace(c.Script) == "" {
			return &ConfigProblem{Code: ProblemNoScript}
		}
	case MethodNone, "":
		return &ConfigProblem{Code: ProblemOff}
	default:
		// Sanitize folds a method it does not recognise into MethodNone, so a
		// caller validating raw form input is the last place the word the user
		// actually typed still exists. Answering "reconnect is switched off" to
		// somebody who typed "ssdp" sends them to the on/off toggle, which is
		// already on, and the real problem - a word three fields away that
		// nothing has normalised yet - never gets looked at.
		return &ConfigProblem{Code: ProblemUnknownMethod, Method: c.Method}
	}
	if strings.TrimSpace(c.CheckURL) == "" {
		// Without a check there is no way to tell a reconnect from a no-op, and
		// this package refuses to report a success it cannot prove.
		return &ConfigProblem{Code: ProblemNoCheckURL}
	}
	if c.Router == "" && c.usesRouterVar() {
		// An unset variable expands to nothing, so an imported script whose
		// login URL is "http://%%router%%/login.cgi" would post the router
		// password to "http:///login.cgi". Refusing here names the empty field;
		// letting it run names a URL parse error three layers down.
		return &ConfigProblem{Code: ProblemNoRouter, Var: VarRouter}
	}
	return nil
}

// The reasons a configuration cannot run, as values.
//
// They exist because the sentence is not translatable and the interface is. A
// German user reading "the command method has no program to run" is reading the
// one English string left in the product, and the alternative - translating on
// the server - would need the browser's language on a settings request and give
// the log a sentence in whatever the last reader happened to prefer. So the code
// crosses the wire and the interface picks the words, which is the same call
// this package's neighbours made for task failures.
const (
	ProblemOff           = "off"
	ProblemNoCommand     = "noCommand"
	ProblemNoRequests    = "noRequests"
	ProblemRequestNoURL  = "requestNoURL"
	ProblemNoInterpreter = "noInterpreter"
	ProblemNoScript      = "noScript"
	ProblemNoCheckURL    = "noCheckURL"
	ProblemUnknownMethod = "unknownMethod"
	ProblemNoRouter      = "noRouter"
)

// ConfigProblem is why a configuration cannot run: a code, and the one detail
// that code needs.
//
// It is an error as well as a value, and the sentence it produces is the same
// one this package produced before - so logs, tests and any caller that only
// ever printed it are unaffected, and errors.Is(err, ErrNotConfigured) still
// answers true.
type ConfigProblem struct {
	Code string
	// N is the 1-based position of the offending request, for ProblemRequestNoURL.
	N int
	// Method is the word the user actually typed, for ProblemUnknownMethod. It
	// is the only field here that carries user input, and it reaches a log and a
	// page - so it is quoted with %q rather than interpolated bare.
	Method string
	// Var is the variable name that has no value, for ProblemNoRouter.
	Var string
}

func (p *ConfigProblem) Error() string {
	return fmt.Sprintf("%s: %s", ErrNotConfigured, p.detail())
}

func (p *ConfigProblem) detail() string {
	switch p.Code {
	case ProblemNoCommand:
		return "the command method has no program to run"
	case ProblemNoRequests:
		return "the request method has no requests"
	case ProblemRequestNoURL:
		return fmt.Sprintf("request %d has no URL", p.N)
	case ProblemNoInterpreter:
		return "the script method has no interpreter to run it with"
	case ProblemNoScript:
		return "the script method has no script"
	case ProblemNoCheckURL:
		return "no IP check URL"
	case ProblemUnknownMethod:
		return fmt.Sprintf("unknown reconnect method %q", p.Method)
	case ProblemNoRouter:
		return fmt.Sprintf("the script uses %%%%%s%%%% but no router address is set", p.Var)
	default:
		// ProblemOff and anything a later change forgets to describe. Falling
		// back to the switched-off wording for an unknown code would be a lie
		// with a plausible face, so an unrecognised code says so.
		if p.Code == ProblemOff {
			return "reconnect is switched off"
		}
		return "the configuration is incomplete"
	}
}

// Unwrap keeps errors.Is(err, ErrNotConfigured) working, which is how every
// caller outside this package tells "not finished setting up" from "the router
// refused".
func (p *ConfigProblem) Unwrap() error { return ErrNotConfigured }

// usesRouterVar reports whether anything this method would run references the
// router address.
func (c Config) usesRouterVar() bool {
	switch c.Method {
	case MethodCommand:
		if containsVar(c.Command, VarRouter) {
			return true
		}
		for _, a := range c.Args {
			if containsVar(a, VarRouter) {
				return true
			}
		}
	case MethodHTTP:
		for _, q := range c.Requests {
			if containsVar(q.URL, VarRouter) || containsVar(q.Body, VarRouter) {
				return true
			}
			for k, v := range q.Headers {
				if containsVar(k, VarRouter) || containsVar(v, VarRouter) {
					return true
				}
			}
		}
	case MethodScript:
		return containsVar(c.Script, VarRouter)
	}
	return false
}

// containsVar reports whether s references the named variable, matching the
// same case-insensitively as expandVars does - a check that only found
// "%%router%%" would let "%%Router%%" through and refuse nothing.
func containsVar(s, name string) bool {
	return strings.Contains(strings.ToLower(s), "%%"+name+"%%")
}

// Redacted returns a copy with the router password replaced by
// RedactedPassword, for handing to a browser or writing to a log. Only the
// password is copied out; the returned value still shares the request list, so
// it is meant for reading, not for editing.
func (c Config) Redacted() Config {
	if c.Password != "" {
		c.Password = RedactedPassword
	}
	return c
}

// WithSecretsFrom puts back the password that Redacted removed. A settings form
// that was shown a redacted config sends the placeholder back untouched, and
// without this the save would wipe the stored password on every visit to the
// page. An empty password is left empty, which is how it is cleared on purpose.
func (c Config) WithSecretsFrom(prev Config) Config {
	if c.Password == RedactedPassword {
		c.Password = prev.Password
	}
	return c
}

// String describes a configuration without its password, so a caller that logs
// the settings struct - or leaves a %v in a line written while debugging -
// cannot put the router password into a log file.
func (c Config) String() string {
	switch c.Method {
	case MethodCommand:
		return fmt.Sprintf("reconnect{command %q with %d args, check %s}", c.Command, len(c.Args), c.CheckURL)
	case MethodHTTP:
		return fmt.Sprintf("reconnect{%d requests, check %s}", len(c.Requests), c.CheckURL)
	case MethodUPnP:
		return fmt.Sprintf("reconnect{upnp, check %s}", c.CheckURL)
	case MethodScript:
		// The script's length, never its text. A reconnect script is the one
		// place a user is likely to hard-code a router password that this
		// package's own redaction knows nothing about, because it never passed
		// through the password field.
		return fmt.Sprintf("reconnect{script %q, %d bytes, check %s}", c.Interpreter, len(c.Script), c.CheckURL)
	default:
		return "reconnect{off}"
	}
}

// vars are the substitutions for one run. The address is the one the box had
// before the method ran, because that is what a router script needs to identify
// the session it is about to drop.
func (c Config) vars(ip netip.Addr) map[string]string {
	addr := ""
	if ip.IsValid() {
		addr = ip.String()
	}
	return map[string]string{
		VarIP:       addr,
		VarUsername: c.Username,
		VarPassword: c.Password,
		VarRouter:   c.Router,
	}
}

// expandVars replaces every %%name%% in s from vars, matching names without
// regard to case. An unknown name is left verbatim for the same reason
// pathvars.Expand leaves an unknown <jd:...> in place: a typo that survives into
// the failing URL can be seen and fixed, while one that expands to nothing
// produces a request that is subtly wrong and looks perfectly fine.
//
// This deliberately does not call pathvars.Expand. That expander speaks a
// different syntax and sanitises every value into a single path segment, which
// would turn the colons and slashes of a router URL into dashes.
func expandVars(s string, vars map[string]string) string {
	const marker = "%%"
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
			// An unclosed %% is not a placeholder; leaving the rest untouched
			// keeps the broken template visible.
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(before)
		if v, ok := vars[strings.ToLower(strings.TrimSpace(name))]; ok {
			b.WriteString(v)
		} else {
			b.WriteString(marker)
			b.WriteString(name)
			b.WriteString(marker)
		}
		s = after
	}
}

// redact strips the router password out of an error before anyone can log it.
//
// It is one choke point on purpose. The password is substituted into program
// arguments, into URLs and into request bodies, so it reaches an error string
// through the command's own output, through the URL that *url.Error prints back
// and through anything a transport chooses to quote - and patching those three
// sites separately is how the fourth one gets missed.
func (c Config) redact(err error) error {
	if err == nil || c.Password == "" {
		return err
	}
	msg := err.Error()
	out := msg
	// The plain text and all three URL encodings, because they disagree with
	// each other: a space is "+" in a query and "%20" in a path, and the
	// userinfo section escapes "@" where a path leaves it alone. Looking only
	// for the plain text walks straight past a password that reached the error
	// through "http://user:pass@router/".
	secrets := []string{
		c.Password,
		url.QueryEscape(c.Password),
		url.PathEscape(c.Password),
		strings.TrimPrefix(url.UserPassword("", c.Password).String(), ":"),
	}
	for _, secret := range secrets {
		out = strings.ReplaceAll(out, secret, RedactedPassword)
	}
	if out == msg {
		return err
	}
	return &redactedError{msg: out, err: err}
}

// redactedError carries the cleaned message and keeps the original only to
// answer errors.Is, so a cancelled reconnect is still recognisable as one.
type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }

// Is answers sentinel comparisons from the original chain.
//
// There is deliberately no Unwrap and no As here, which is the whole reason this
// type is not a plain fmt.Errorf wrapper. Both of them hand the caller the
// original error itself, and that error's message - or, for a *url.Error, its
// URL field - still spells the router password out in full. A redaction that
// one errors.Unwrap steps around is not a redaction; it is a redaction that
// holds right up until the first person who wants more detail in a log line.
// Is gives callers the only thing they actually ask an error from this package,
// which sentinel it is, without ever letting go of the cleaned string.
func (e *redactedError) Is(target error) bool { return errors.Is(e.err, target) }

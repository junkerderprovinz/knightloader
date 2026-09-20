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

	// VarRouter is the router's LAN address. It must not be confused with
	// VarIP, the public address before the run: a login request sent there
	// would carry the router password out to the internet. JDownloader spells
	// it %%%routerip%%%.
	VarRouter = "router"
)

// RedactedPassword is what Redacted puts in place of the router password, and
// the value WithSecretsFrom reads as "not retyped". An empty string still
// means "clear the password".
const RedactedPassword = "********"

// The failures a caller is expected to tell apart: reconnect is not fully set
// up, the router obeyed and the address stayed the same, or the check URL
// answered without an address.
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

	// Router is the router's LAN address, substituted for %%router%%. It is
	// stored without a scheme so a template can put it anywhere in a URL;
	// Sanitize strips one pasted from the browser's address bar.
	Router string `json:"router,omitempty"`

	// Command and Args are the external program for MethodCommand.
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`

	// Requests are replayed in order for MethodHTTP.
	Requests []Request `json:"requests,omitempty"`

	// Interpreter, InterpreterArgs and Script are MethodScript. The script is
	// written to a private temporary file whose path goes to the interpreter,
	// so no shell command line ever has the router password quoted into it.
	Interpreter     string   `json:"interpreter,omitempty"`
	InterpreterArgs []string `json:"interpreterArgs,omitempty"`
	Script          string   `json:"script,omitempty"`

	// UPnPLocation pins the gateway's device description URL and skips SSDP
	// discovery, for networks that filter the multicast search. It is normally
	// empty.
	UPnPLocation string `json:"upnpLocation,omitempty"`

	// CheckURL is fetched to learn the current public address. It has no
	// default, so the address is never reported to a service the user did not
	// choose.
	CheckURL string `json:"checkUrl,omitempty"`

	// Seconds rather than time.Duration, which JSON would write as nanoseconds.
	IntervalSeconds int `json:"intervalSeconds"`
	TimeoutSeconds  int `json:"timeoutSeconds"`
}

// The bounds Sanitize enforces. The interval floor keeps the poll loop from
// hammering an IP-check service; a reconnect still waiting after a quarter of
// an hour has failed.
const (
	defaultIntervalSeconds = 5
	minIntervalSeconds     = 1
	maxIntervalSeconds     = 60

	defaultTimeoutSeconds = 120
	minTimeoutSeconds     = 5
	maxTimeoutSeconds     = 15 * 60
)

// Defaults returns the configuration a fresh install starts with: switched off,
// with the timing filled in for the settings form.
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

// Sanitize normalises a configuration from a settings file or an API request.
// It never rejects: anything it cannot make sense of becomes the safe value,
// and Validate reports what is missing.
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
		// An unknown method is off; guessing could fire commands at a router
		// the user never configured.
		c.Method = MethodNone
	}

	c.Username = strings.TrimSpace(c.Username)
	// The password is not trimmed, since spaces are legal in it.
	c.Command = strings.TrimSpace(c.Command)
	c.CheckURL = strings.TrimSpace(c.CheckURL)
	c.Interpreter = strings.TrimSpace(c.Interpreter)
	c.UPnPLocation = strings.TrimSpace(c.UPnPLocation)
	c.Router = sanitizeRouter(c.Router)
	// Only surrounding blank space is trimmed, so indentation inside the
	// script survives.
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
	// A timeout below the interval would end the run before the first check.
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

// sanitizeRouter reduces whatever was pasted into the router field to a bare
// host, optionally with a port. Templates write "http://%%router%%/login.cgi",
// and a pasted "http://192.168.1.1/" would otherwise expand to
// "http://http://192.168.1.1//login.cgi".
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

// sanitizeHeaders drops entries whose name is blank, which would produce a
// header line the router answers with a parse error.
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
			// Skipping an entry with no URL could leave a script that logs in
			// and never reboots.
			if strings.TrimSpace(q.URL) == "" {
				return &ConfigProblem{Code: ProblemRequestNoURL, N: i + 1}
			}
		}
	case MethodUPnP:
		// Nothing is required: the gateway is found by asking the network.
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
		// Only unsanitized input gets here. Naming the unknown word is more
		// useful than "switched off", which would point at the toggle.
		return &ConfigProblem{Code: ProblemUnknownMethod, Method: c.Method}
	}
	if strings.TrimSpace(c.CheckURL) == "" {
		// Without a check a reconnect cannot be told from a no-op.
		return &ConfigProblem{Code: ProblemNoCheckURL}
	}
	if c.Router == "" && c.usesRouterVar() {
		// An empty router would post the password to "http:///login.cgi" and
		// fail with a URL parse error instead of naming the empty field.
		return &ConfigProblem{Code: ProblemNoRouter, Var: VarRouter}
	}
	return nil
}

// The reasons a configuration cannot run, as codes the interface translates;
// the server does not know the browser's language.
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

// ConfigProblem is why a configuration cannot run: a code and the one detail
// it needs. As an error it reads as an English sentence and matches
// ErrNotConfigured.
type ConfigProblem struct {
	Code string
	// N is the 1-based position of the offending request, for ProblemRequestNoURL.
	N int
	// Method is the word the user typed, for ProblemUnknownMethod. Being user
	// input, it is quoted with %q.
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
		// An unknown code must not read as "switched off".
		if p.Code == ProblemOff {
			return "reconnect is switched off"
		}
		return "the configuration is incomplete"
	}
}

// Unwrap keeps errors.Is(err, ErrNotConfigured) working, which is how callers
// tell "not finished setting up" from "the router refused".
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

// containsVar reports whether s references the named variable,
// case-insensitively like expandVars.
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

// WithSecretsFrom puts back the password that Redacted removed, since a
// settings form sends the placeholder back untouched. An empty password stays
// empty, which is how it is cleared.
func (c Config) WithSecretsFrom(prev Config) Config {
	if c.Password == RedactedPassword {
		c.Password = prev.Password
	}
	return c
}

// String describes a configuration without its password, so logging the
// settings struct with %v cannot leak it.
func (c Config) String() string {
	switch c.Method {
	case MethodCommand:
		return fmt.Sprintf("reconnect{command %q with %d args, check %s}", c.Command, len(c.Args), c.CheckURL)
	case MethodHTTP:
		return fmt.Sprintf("reconnect{%d requests, check %s}", len(c.Requests), c.CheckURL)
	case MethodUPnP:
		return fmt.Sprintf("reconnect{upnp, check %s}", c.CheckURL)
	case MethodScript:
		// The script's length, never its text: users hard-code passwords in
		// scripts that redaction knows nothing about.
		return fmt.Sprintf("reconnect{script %q, %d bytes, check %s}", c.Interpreter, len(c.Script), c.CheckURL)
	default:
		return "reconnect{off}"
	}
}

// vars are the substitutions for one run. The address is the one from before
// the method ran, which a router script needs to identify the session it
// drops.
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
// regard to case. An unknown name is left verbatim so a typo stays visible in
// the failing URL. pathvars.Expand is not used because it sanitises values
// into a single path segment, which would mangle a router URL.
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
// The password reaches errors through command output, the URL a *url.Error
// prints and whatever a transport quotes, so every method's errors pass
// through this one place.
func (c Config) redact(err error) error {
	if err == nil || c.Password == "" {
		return err
	}
	msg := err.Error()
	out := msg
	// The plain text and all three URL encodings, which differ: a space is "+"
	// in a query and "%20" in a path, and userinfo escapes "@" where a path
	// does not.
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

// Is answers sentinel comparisons from the original chain. There is no Unwrap
// or As, because either would hand out the original error, whose message or
// *url.Error URL still contains the password.
func (e *redactedError) Is(target error) bool { return errors.Is(e.err, target) }

// Package hostheaders is the user's own request headers for one origin: a
// cookie from a logged-in browser session, a Referer a forum insists on, the
// Basic auth a seedbox sits behind. The engine has been able to send arbitrary
// headers since it existed (engine.Job.Headers), but the only thing that could
// ever fill that map was a resolver's own Result, so a link on a person's own
// Nextcloud, on a referrer-protected forum or behind a seedbox login was
// simply unreachable - not refused with a message, just permanently 401 or
// 403 with nothing to configure.
//
// # The two rules everything here is built around
//
// A HEADER IS A SECRET AND IT NEVER LEAVES THIS PACKAGE IN PRINTABLE FORM.
// Set, Header and Profile all carry a value somebody's session depends on, so
// all three implement String, GoString and MarshalJSON, and all three of those
// print the header NAMES and never a value. That is deliberate belt and
// braces: a log line is written by whoever is debugging at the time, and a
// redaction step that has to be remembered at every call site is a redaction
// step that is eventually forgotten. Here the type itself cannot be printed
// wrongly - fmt reaches Stringer for %v, %s and %+v, GoStringer for %#v, and
// encoding/json reaches MarshalJSON for the diagnostics bundle. Plaintext
// leaves through exactly one door, Set.Attach, which is named for the one
// thing it is for.
//
// The at-rest half follows the same reasoning as internal/hosterauth and
// internal/resolver/ytdlp's cookie jars, and reuses their store rather than
// growing a third encryption scheme: the values are sealed by
// internal/accounts.Store (AES-256-GCM under the per-install key in the data
// dir) under a pseudo-service id, so they are never in settings.json - and
// settings.json is exactly what internal/api/routes_diagnostics.go serialises
// into the bundle a person attaches to a public bug report. Staying out of
// that bundle is a property of WHERE this is stored, not of a redaction
// somebody has to remember to run.
//
// A HEADER IS SCOPED TO ONE ORIGIN AND MAY NEVER CROSS IT. This is the whole
// point of the package and the reason it, rather than a map on the task,
// exists. A forum that redirects its attachment links to a third-party CDN
// would otherwise hand that CDN the Authorization header, and an open redirect
// on any configured host would become a credential giveaway. So:
//
//   - Attach returns nothing at all for a URL that is not on the profile's own
//     origin, which covers the chain that ENDS somewhere else.
//   - checkRedirect (redirect.go) deletes every configured header name the
//     moment a hop leaves that origin, which covers the chain that merely
//     PASSES THROUGH somewhere else and comes back.
//
// The scope is a full origin - scheme, host and port - and not a host name.
// A downgrade from https to http on the same host is a different origin on
// purpose: a bearer token forwarded onto a plaintext hop is a token given to
// everyone on the path. internal/httpx.sameOrigin decides the same question
// the same way, and for the same reason.
//
// Sub-domains are NOT covered by a parent's profile, which is the one place
// this is deliberately less convenient than internal/resolver/ytdlp's cookie
// store. That store walks up the domain because a browser's cookie jar is
// scoped that way and yt-dlp is handed the jar wholesale; here a single
// hostile or merely compromised sub-domain of a configured host would be
// enough to collect the credential. A second host needs a second profile,
// which is one more thing to configure and the only version of this that
// cannot hand a credential to a host the user never named.
package hostheaders

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// Redacted is what a header value is printed as, everywhere. It mirrors
// accounts.Redacted rather than an empty string for the reason that package
// gives: empty has to keep meaning "clear this", so a placeholder is the only
// way an editing form can show that something is stored without showing what.
const Redacted = "********"

// Limits on one profile. None of these is tuning; each one bounds a paste.
//
// A header block arrives here by copy and paste out of a browser's developer
// tools, and the thing being pasted is whatever was on the clipboard. The
// value cap is generous because a real session cookie for a large site runs
// into the low thousands of bytes, and the count cap is generous because a
// browser sends a dozen headers without trying - both are set where an
// accident stops rather than where a legitimate paste would.
const (
	MaxHeaders   = 32
	MaxNameLen   = 128
	MaxValueLen  = 8192
	MaxProfileID = 64
)

// Header is one header line. Value is a secret; see the package comment for
// why this type refuses to print it.
type Header struct {
	Name  string
	Value string
}

// String, GoString and MarshalJSON are the three doors fmt and encoding/json
// take out of this type, and all three are closed on the value. A header whose
// value is empty still prints the placeholder: whether a stored header happens
// to be empty is itself something a log has no business distinguishing, and a
// conditional here would be one more branch to get wrong.
func (h Header) String() string   { return h.Name + ": " + Redacted }
func (h Header) GoString() string { return "hostheaders.Header{" + h.Name + ": " + Redacted + "}" }

func (h Header) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"name": h.Name, "value": Redacted})
}

// Set is one origin's header block: the origin the headers belong to and the
// headers themselves, sorted by name and free of duplicates (see Normalize).
//
// A slice rather than a map, because a map's iteration order is random and
// this type's whole job is to be printed the same way twice - a redacted line
// that reorders itself between two log entries reads as two different states.
type Set struct {
	// Origin is scheme://host:port, with the port always written out (see
	// OriginOf). It is the only thing that decides whether these headers may
	// be sent, so it is stored WITH the secret rather than beside it: a scope
	// kept in a second place is a scope that can be edited without re-typing
	// the credential it guards.
	Origin  string
	Headers []Header
}

func (s Set) String() string {
	return fmt.Sprintf("hostheaders.Set{origin: %s, headers: [%s]}", s.Origin, strings.Join(s.Names(), " "))
}

func (s Set) GoString() string { return s.String() }

// MarshalJSON emits the names with placeholder values, which is what an
// editing form needs to show a stored profile. The sealed form is NOT this
// (see store.go's wire type): a redacting marshaller on the type that is also
// the type being persisted would otherwise seal the placeholders and destroy
// the credential on the first save.
func (s Set) MarshalJSON() ([]byte, error) {
	out := struct {
		Origin  string   `json:"origin"`
		Headers []Header `json:"headers"`
	}{Origin: s.Origin, Headers: s.Headers}
	if out.Headers == nil {
		out.Headers = []Header{}
	}
	return json.Marshal(out)
}

// IsZero reports a set carrying nothing, which is what Store.Save reads as
// "delete this profile" - the same convention accounts.Credential.IsZero and
// accounts.Store.Set have always used for a cleared secret.
func (s Set) IsZero() bool { return s.Origin == "" && len(s.Headers) == 0 }

// Names lists the header names, in the order they are stored. Names are not
// secret - "this profile sends an Authorization and a Cookie" is exactly what
// a settings page has to be able to say - and this is the only listing of a
// Set that any caller outside the package gets.
func (s Set) Names() []string {
	out := make([]string, 0, len(s.Headers))
	for _, h := range s.Headers {
		out = append(out, h.Name)
	}
	return out
}

// Attach is the one door plaintext leaves by, and it is a door with a lock on
// it: a URL that is not on this set's own origin gets nothing.
//
// THIS IS THE ATTACH-TIME HALF OF THE CROSS-ORIGIN RULE. It covers a redirect
// chain that ends somewhere else, because the caller resolves the chain first
// and asks about the URL it landed on (see Preflight); the redirect-time half,
// for a chain that merely passes through a foreign host on its way back, is
// checkRedirect in redirect.go. Both are needed - neither one alone closes the
// other's case.
//
// A nil map rather than an empty one when nothing may be sent, because that is
// what resolver.Result.Headers means by "no headers": engine.Job hands the map
// straight to the download library, and nil is the value it already reads as
// "the caller has no opinion".
func (s Set) Attach(rawurl string) map[string]string {
	if s.Origin == "" || OriginOf(rawurl) != s.Origin {
		return nil
	}
	if len(s.Headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(s.Headers))
	for _, h := range s.Headers {
		out[h.Name] = h.Value
	}
	return out
}

// Normalize canonicalises a set: header names are put into the capitalisation
// net/http uses, blank entries are dropped, duplicates collapse to the last
// one written, and the result is sorted by name.
//
// Sorting and de-duplicating happen at save time rather than at send time so
// that a stored profile has exactly one shape. Two entries differing only in
// the case of the name are the same header to every server on earth, and a
// profile holding both would send whichever the map iteration happened to
// reach last - a profile that authenticates on Tuesdays.
//
// It reports what it had to refuse rather than silently trimming, because the
// input is a paste and a paste that half-arrived is worth saying out loud.
func Normalize(s Set) (Set, error) {
	origin := OriginOf(s.Origin)
	if origin == "" {
		return Set{}, fmt.Errorf("hostheaders: %q is not an http or https address", s.Origin)
	}
	byName := make(map[string]string, len(s.Headers))
	var order []string
	for _, h := range s.Headers {
		name := canonicalName(h.Name)
		if name == "" {
			// A nameless header cannot be sent and cannot be edited away
			// either, the same reasoning reconnect.sanitizeHeaders applies to
			// its own request headers.
			continue
		}
		if len(name) > MaxNameLen {
			return Set{}, fmt.Errorf("hostheaders: a header name is %d characters, the limit is %d", len(name), MaxNameLen)
		}
		value := strings.TrimSpace(h.Value)
		if value == "" {
			continue
		}
		if len(value) > MaxValueLen {
			// The length is named and the value is not, here and in every
			// other error in this package: an error string travels into a
			// task's Err field, from there into the log ring, and from there
			// into the diagnostics bundle.
			return Set{}, fmt.Errorf("hostheaders: the value of %s is %d characters, the limit is %d", name, len(value), MaxValueLen)
		}
		if strings.ContainsAny(value, "\r\n") {
			// Refused rather than stripped. A newline in a header value is
			// request splitting, and a value that was silently repaired is a
			// value the user believes is being sent as they pasted it.
			return Set{}, fmt.Errorf("hostheaders: the value of %s contains a line break", name)
		}
		if _, seen := byName[name]; !seen {
			order = append(order, name)
		}
		byName[name] = value
	}
	if len(byName) > MaxHeaders {
		return Set{}, fmt.Errorf("hostheaders: %d headers, the limit is %d", len(byName), MaxHeaders)
	}
	sort.Strings(order)
	out := Set{Origin: origin, Headers: make([]Header, 0, len(order))}
	for _, name := range order {
		out.Headers = append(out.Headers, Header{Name: name, Value: byName[name]})
	}
	return out, nil
}

// canonicalName puts a header name into net/http's capitalisation and refuses
// anything that is not a token.
//
// The refusal is what stops a pasted blob from becoming a header at all: a
// line out of a curl command that this package failed to split correctly would
// otherwise be saved as a header whose "name" is half a shell command, and the
// first request built from it would fail somewhere far away from the paste box
// it came from.
func canonicalName(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		return ""
	}
	for _, r := range name {
		if r <= ' ' || r >= 0x7f || strings.ContainsRune("()<>@,;:\\\"/[]?={}", r) {
			return ""
		}
	}
	// Same capitalisation net/http's textproto uses, so a profile that stores
	// "x-forum-token" and a header the engine sends as "X-Forum-Token" are one
	// entry rather than two.
	parts := strings.Split(strings.ToLower(name), "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "-")
}

// OriginOf reduces a URL to the origin a credential is scoped to: scheme,
// host and port, lower-cased, with the port always spelled out.
//
// The port is filled in from the scheme rather than left off, so that
// https://h and https://h:443 compare equal as plain strings and nothing has
// to remember to call a comparison helper. Anything that is not an http or
// https URL with a host answers "", which every caller reads as "no origin",
// never as "any origin".
func OriginOf(rawurl string) string {
	u, err := url.Parse(strings.TrimSpace(rawurl))
	if err != nil {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return ""
	}
	port := u.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return scheme + "://" + host + ":" + port
}

// ProfileID normalises the name a profile is stored and addressed under - by
// a rule action, by a task, and as the account half of the key in
// accounts.Store.
//
// Lower-cased and trimmed so that "Forum" typed into a rule finds the profile
// saved as "forum"; refused when it holds anything but letters, digits and the
// three separators, because this string is a map key that a person types in
// two different places and has to be able to get right the second time.
func ProfileID(raw string) string {
	id := strings.ToLower(strings.TrimSpace(raw))
	if id == "" || len(id) > MaxProfileID {
		return ""
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return ""
		}
	}
	return id
}

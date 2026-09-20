// Package hostheaders holds a user's own request headers for one origin: a
// session cookie, a Referer a forum insists on, the Basic auth in front of a
// seedbox.
//
// Header values are secrets. Set and Header print only header names
// through String, GoString and MarshalJSON, so no log line or diagnostics
// bundle can carry a value; plaintext leaves only through Set.Attach. At rest
// the values are sealed in accounts.Store, never in settings.json, which ends
// up in diagnostics bundles.
//
// A header is scoped to one origin (scheme, host and port) and never crosses
// it. Attach returns nothing for a URL on another origin, covering a redirect
// chain that ends elsewhere, and checkRedirect strips the configured headers
// on any hop that leaves the origin, covering a chain that passes through a
// foreign host. An https to http downgrade counts as another origin, and
// sub-domains are not covered by a parent's profile, since one compromised
// sub-domain would otherwise be enough to collect the credential.
package hostheaders

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// Redacted is what a header value is printed as. An empty string cannot be
// used because it means "clear this" in an editing form.
const Redacted = "********"

// Limits on one profile. Headers arrive by pasting from browser developer
// tools; the limits sit well above a real session cookie or header block and
// stop an accidental paste.
const (
	MaxHeaders   = 32
	MaxNameLen   = 128
	MaxValueLen  = 8192
	MaxProfileID = 64
)

// Header is one header line. Value is a secret and is never printed.
type Header struct {
	Name  string
	Value string
}

func (h Header) String() string   { return h.Name + ": " + Redacted }
func (h Header) GoString() string { return "hostheaders.Header{" + h.Name + ": " + Redacted + "}" }

func (h Header) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"name": h.Name, "value": Redacted})
}

// Set is one origin's header block, sorted by name and free of duplicates
// (see Normalize). It is a slice so that it prints the same way every time.
type Set struct {
	// Origin is scheme://host:port with the port always written out (see
	// OriginOf). It is stored together with the secret so the scope cannot be
	// edited without re-entering the credential it guards.
	Origin  string
	Headers []Header
}

func (s Set) String() string {
	return fmt.Sprintf("hostheaders.Set{origin: %s, headers: [%s]}", s.Origin, strings.Join(s.Names(), " "))
}

func (s Set) GoString() string { return s.String() }

// MarshalJSON emits the names with placeholder values for an editing form.
// The sealed form uses a separate wire type (see store.go), so saving never
// persists the placeholders.
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

// IsZero reports a set carrying nothing, which Store.Save reads as "delete
// this profile".
func (s Set) IsZero() bool { return s.Origin == "" && len(s.Headers) == 0 }

// Names lists the header names in stored order. Names are not secret.
func (s Set) Names() []string {
	out := make([]string, 0, len(s.Headers))
	for _, h := range s.Headers {
		out = append(out, h.Name)
	}
	return out
}

// Attach returns the headers as plaintext for rawurl, or nil when rawurl is
// not on the set's origin. It covers a redirect chain that ends elsewhere;
// checkRedirect covers one that only passes through a foreign host.
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

// Normalize canonicalises a set: names get net/http's capitalisation, blank
// entries are dropped, duplicates collapse to the last one written, and the
// result is sorted by name. It returns an error for anything it has to
// refuse instead of trimming a pasted block silently.
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
			// Errors name the length, never the value: they end up in the
			// log and the diagnostics bundle.
			return Set{}, fmt.Errorf("hostheaders: the value of %s is %d characters, the limit is %d", name, len(value), MaxValueLen)
		}
		if strings.ContainsAny(value, "\r\n") {
			// Refused rather than stripped: a line break is request splitting,
			// and a silently repaired value is not what the user pasted.
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

// canonicalName puts a header name into net/http's capitalisation and returns
// "" for anything that is not a token, so a badly split paste never becomes a
// header.
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
// host and port, lower-cased, with the port always spelled out so origins
// compare as plain strings. Anything but an http or https URL with a host
// yields "", which means no origin.
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

// ProfileID normalises the name a profile is stored and addressed under. It
// is lower-cased and trimmed so "Forum" in a rule finds the profile "forum",
// and it returns "" for anything but letters, digits, '-', '_' and '.'.
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

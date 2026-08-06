// Package proxycfg models JDownloader's Connection Manager: a user-ordered list
// of outbound connections that downloads are spread across. It owns the list,
// the rules for what a usable entry looks like, and a picker that hands the next
// connection to whoever is about to start a download. It never opens a socket
// itself.
//
// "none" and "direct" are not the same thing, and confusing them makes the whole
// feature behave backwards:
//
//	none   - the entry is inert. It is a row in the user's list that names no
//	         connection at all, so the picker never returns it. As far as
//	         downloads are concerned a none entry and a deleted entry are the
//	         same; the row survives only so a save does not silently delete
//	         something the user is still editing.
//	direct - the entry is a real choice: go out over the machine's own
//	         connection, deliberately bypassing every proxy. This is how a user
//	         excludes their NAS from a whole-app proxy. A direct entry whose host
//	         filter is "nas.local" claims that host, and the proxy no longer gets
//	         a say in it.
//
// That last sentence is the general rule: an entry whose filter matches the
// target host is preferred over an entry with no filter at all. Without the
// preference a catch-all proxy would keep taking its turn on a host the user
// explicitly pointed somewhere else, which is precisely the exclusion the direct
// entry was added to express.
package proxycfg

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Kind is the transport an entry uses. It is a string and not an integer
// because it is persisted in settings.json, where somebody eventually reads it
// by hand.
type Kind string

const (
	KindNone    Kind = "none"   // inert row: never picked
	KindDirect  Kind = "direct" // deliberately unproxied
	KindHTTP    Kind = "http"
	KindHTTPS   Kind = "https"
	KindSOCKS4  Kind = "socks4"
	KindSOCKS4A Kind = "socks4a"
	KindSOCKS5  Kind = "socks5"
)

// maxDownloadsCap mirrors the cap settings puts on global concurrency. A
// per-connection limit larger than anything the app will ever run at once is not
// a limit, it is a number that reads like one.
const maxDownloadsCap = 64

// Entry is one outbound connection in the user's list.
type Entry struct {
	// ID keys the in-flight counter Pick consults and is what an edited row is
	// matched against on save. Sanitize fills in a missing or duplicated one, so
	// no caller has to invent it.
	ID   string `json:"id"`
	Kind Kind   `json:"type"`
	Host string `json:"host,omitempty"`
	Port int    `json:"port,omitempty"`

	// Username and Password are the proxy's own credentials, not a hoster
	// account. The password is persisted in the clear, the way the rest of
	// settings.json is, and must go through Redacted before it leaves the
	// process.
	//
	// String keeps it out of every verb that consults a Stringer, %v and %q
	// included, so an entry logged by accident does not spill it. The two verbs
	// that walk the struct by reflection instead - %#v, and anything built on
	// reflect - still show it, so those stay out of log lines.
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`

	// Enabled is the user's on/off switch. It is deliberately separate from
	// KindNone: switching a proxy off for an evening must not throw its host and
	// credentials away.
	Enabled bool `json:"enabled"`
	// Order is the position in the list, which is the order Pick walks.
	Order int `json:"order"`
	// Filter restricts the entry to the hosters it names. Empty means the entry
	// is a catch-all, which is weaker than a filter that matches: see the
	// package comment.
	Filter []string `json:"filter,omitempty"`
	// MaxDownloads caps how many downloads may share this entry at once. Zero
	// means the picker's default.
	MaxDownloads int `json:"maxDownloads,omitempty"`
}

// Direct is the connection to use when no configured entry claims a host: an
// ordinary, unproxied download. Its ID is empty, which is how a caller tells it
// apart from a row the user configured, and it carries no limit of its own.
func Direct() Entry {
	return Entry{Kind: KindDirect, Enabled: true}
}

// kindOf folds whatever is in the file into a known Kind. The empty string
// becomes none, because a row the user added and never filled in is inert rather
// than invalid, and dropping it would delete it out from under them on the very
// next save.
func kindOf(k Kind) (Kind, bool) {
	switch out := Kind(strings.ToLower(strings.TrimSpace(string(k)))); out {
	case "":
		return KindNone, true
	case KindNone, KindDirect, KindHTTP, KindHTTPS, KindSOCKS4, KindSOCKS4A, KindSOCKS5:
		return out, true
	default:
		return out, false
	}
}

// usable reports whether the picker may ever return e. Sanitize has already
// guaranteed the kind is one we know, so this is only the two switches the user
// controls.
func (e Entry) usable() bool {
	return e.Enabled && e.Kind != KindNone
}

// scheme is the URL scheme for e, or "" for the kinds that are not a proxy.
// socks5 is spelled without the trailing h because that is the spelling every
// version of net/http and x/net/proxy accepts; both hand the host name to the
// proxy and let it resolve, which is the behaviour the socks5h spelling names.
func (e Entry) scheme() string {
	switch e.Kind {
	case KindHTTP, KindHTTPS, KindSOCKS4, KindSOCKS4A, KindSOCKS5:
		return string(e.Kind)
	}
	return ""
}

// URL builds the proxy URL for e, ready for http.Transport.Proxy (via
// http.ProxyURL) and for a SOCKS dialer built with x/net/proxy.FromURL.
//
// It returns nil for none and direct, which is exactly what Transport.Proxy
// wants back when a request must go out unproxied. Host and port are used as
// they are: only an entry that passed Validate has both, and the picker never
// hands out one that did not.
//
// Log the Entry, never this URL - url.URL.String prints the password in full.
func (e Entry) URL() *url.URL {
	scheme := e.scheme()
	if scheme == "" {
		return nil
	}
	// JoinHostPort brackets an IPv6 literal; without it url.URL would read the
	// address's own colons as a port separator and the proxy would come out as a
	// different machine entirely.
	u := &url.URL{Scheme: scheme, Host: net.JoinHostPort(e.Host, strconv.Itoa(e.Port))}
	switch {
	case e.Username == "":
	case e.Kind == KindSOCKS4 || e.Kind == KindSOCKS4A:
		// SOCKS4 carries a user id and has no password field at all, so a
		// password here could never be sent. Leaving it out keeps it from being
		// written into a URL that somebody logs.
		u.User = url.User(e.Username)
	case e.Password == "":
		u.User = url.User(e.Username)
	default:
		u.User = url.UserPassword(e.Username, e.Password)
	}
	return u
}

// NeedsOwnDialer reports whether the caller has to carry the connection itself.
// net/http understands http, https and socks5 proxy URLs and drives them from
// Transport.Proxy; it has never understood socks4, and neither does
// x/net/proxy, so an entry of that kind needs a dialer the caller supplies.
// Handing its URL to Transport.Proxy would fail every request instead.
func (e Entry) NeedsOwnDialer() bool {
	return e.Kind == KindSOCKS4 || e.Kind == KindSOCKS4A
}

// Matches reports whether e's host filter covers host. An entry with no filter
// matches everything.
//
// A pattern without a wildcard covers the domain and everything under it, so
// "example.org" is enough for "dl2.example.org" and nobody has to guess a
// hoster's CDN names. A pattern with a wildcard goes through path.Match; host
// names contain no slashes, so that function's one special case never applies
// here.
//
// Both sides are folded here rather than only in Sanitize. This method is
// exported and most callers hold an entry that came straight out of settings.json
// or off an API request, and a filter typed as "Example.ORG " that silently
// matched nothing would be indistinguishable from a filter that was never saved.
// The lists are a handful of patterns and a pick happens once per download, so
// folding per call costs nothing worth a second code path.
func (e Entry) Matches(host string) bool {
	if len(e.Filter) == 0 {
		return true
	}
	host = normalizeHost(host)
	for _, p := range e.Filter {
		if matchPattern(p, host) {
			return true
		}
	}
	return false
}

// hasWildcard reports whether a pattern is a glob rather than a plain host name.
func hasWildcard(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}

func matchPattern(pattern, host string) bool {
	pattern = normalizeHost(pattern)
	if pattern == "" || host == "" {
		return false
	}
	if hasWildcard(pattern) {
		ok, err := path.Match(pattern, host)
		// Validate refuses a pattern this cannot parse, so reaching here means an
		// entry that never went through it. It still matches nothing rather than
		// everything: a filter the user mistyped must not silently widen to the
		// whole internet and send every download through one proxy.
		return err == nil && ok
	}
	return host == pattern || strings.HasSuffix(host, "."+pattern)
}

// clone returns e with a copy of its filter. Entry is a value everywhere else,
// but the filter is a slice, so handing the same backing array to a caller and
// to a live Picker means a caller that edits what it was given re-points the
// picker's own filter - from another goroutine, and without any write the
// picker can see. The entry then stops claiming the host it was written for and
// the download quietly leaves over the connection the filter existed to avoid.
func (e Entry) clone() Entry {
	if len(e.Filter) > 0 {
		e.Filter = append([]string(nil), e.Filter...)
	}
	return e
}

// normalizeHost folds a host into the single form filters are compared in.
// Callers hand us whatever they have - app.go passes a bare host name, but an
// "example.org:443" from anywhere else would otherwise match no filter at all
// and route that download around the proxy the user picked for it.
func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	// SplitHostPort only succeeds on a real host:port pair; a bare IPv6 literal
	// has too many colons for it and is left alone.
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	if strings.HasPrefix(h, "[") && strings.HasSuffix(h, "]") {
		// Unwrap a bracketed address only when it really is one, so a filter
		// pattern that happens to open with a character class is not mangled
		// into something that matches other hosts.
		if inner := h[1 : len(h)-1]; net.ParseIP(inner) != nil {
			h = inner
		}
	}
	// The DNS root dot is legal in a URL and never typed into a filter.
	return strings.TrimSuffix(h, ".")
}

// String describes the entry for a log line or an error message. The password is
// not masked here, it is never assembled into the string at all: this type ends
// up behind %v in more places than anyone tracks, and a mask that depends on
// every call site remembering to ask for Redacted first is not a guarantee.
func (e Entry) String() string {
	// The folded kind, because an entry is logged just as often before Sanitize
	// has run as after, and a log line reading " HTTP ://proxy.lan:8080" sends
	// whoever is reading it looking for a bug that is not there.
	k, _ := kindOf(e.Kind)
	switch k {
	case KindNone, KindDirect:
		return string(k)
	}
	var b strings.Builder
	b.WriteString(string(k))
	b.WriteString("://")
	if e.Username != "" {
		b.WriteString(e.Username)
		b.WriteByte('@')
	}
	b.WriteString(net.JoinHostPort(e.Host, strconv.Itoa(e.Port)))
	return b.String()
}

// Redacted returns a copy of e with the password removed: for a log line, for an
// error and above all for the API, because a settings page that ships every
// proxy password to every client is a leak nobody notices until it is in a
// screenshot.
//
// The password is dropped, not masked, so a client that posts the list straight
// back would clear it. Merge is what puts it back.
func (e Entry) Redacted() Entry {
	e.Password = ""
	return e
}

// Validate reports why an entry cannot be used. Sanitize drops whatever this
// rejects, so the API should call it first and refuse the save with the reason:
// a row that vanishes on save is the same class of bug as a download folder that
// silently reverts, except the user blames the proxy for it weeks later.
func Validate(e Entry) error {
	k, ok := kindOf(e.Kind)
	if !ok {
		return fmt.Errorf("proxycfg: %q is not a connection type", string(e.Kind))
	}
	// The filter is checked for every kind, because it is the only field a direct
	// entry has and the whole reason that kind exists.
	if err := checkFilter(e.Filter); err != nil {
		return err
	}
	if k == KindNone || k == KindDirect {
		return nil // neither names an endpoint, so there is nothing else to check
	}
	host := normalizeHost(e.Host)
	if host == "" {
		return errors.New("proxycfg: a proxy needs a host")
	}
	if err := checkHost(host); err != nil {
		return err
	}
	if e.Port < 1 || e.Port > 65535 {
		return fmt.Errorf("proxycfg: port %d is outside 1-65535", e.Port)
	}
	return nil
}

// checkHost refuses anything that would not survive being put into a URL. The
// case it is really about is a pasted "http://proxy.lan:8080/" in the host
// field: url.URL would carry the scheme and the path along, and the proxy that
// came out would point somewhere the user never typed.
func checkHost(host string) error {
	if len(host) > 255 {
		return errors.New("proxycfg: host is too long")
	}
	if strings.ContainsAny(host, " \t\r\n/\\@?#%") {
		return fmt.Errorf("proxycfg: %q is not a host name or address", host)
	}
	// A colon that survived normalisation is either a second port or an IPv6
	// literal, and only the literal is a host.
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return fmt.Errorf("proxycfg: %q is not a host name or address", host)
	}
	return nil
}

// checkFilter refuses a pattern that could never match anything, because a
// filter that silently matches nothing is worse than no filter at all: the entry
// stops claiming the host the user pointed it at, the picker finds nothing that
// claims that host and answers with a plain download, and the traffic the filter
// was written to route goes out unproxied with nothing anywhere to say so.
//
// Two shapes turn up in practice. A mistyped wildcard ("[oops") is not a pattern
// path.Match can parse at all. A whole URL pasted into the filter box
// ("http://example.org") is worse, because normalizeHost reads its "http:" as a
// host and a port and folds it to the pattern "http", which is a perfectly valid
// pattern that matches no hoster on earth. That is why this works on what the
// user typed rather than on the folded form: by then the evidence is gone.
func checkFilter(patterns []string) error {
	for _, raw := range patterns {
		p := strings.ToLower(strings.TrimSpace(raw))
		if p == "" {
			continue // cleanFilter drops blanks; an empty row is not an opinion
		}
		if len(p) > 255 {
			return fmt.Errorf("proxycfg: host filter %q is too long", raw)
		}
		// A wildcard filter may legitimately contain * ? [ ], so checkHost's rules
		// cannot simply be reused; everything listed here is a character no host
		// name and no glob over one ever holds.
		if strings.ContainsAny(p, " \t\r\n/\\@#%") {
			return fmt.Errorf("proxycfg: host filter %q is not a host name or pattern", raw)
		}
		if hasWildcard(p) {
			if _, err := path.Match(p, "example.org"); err != nil {
				return fmt.Errorf("proxycfg: host filter %q is not a valid pattern: %w", raw, err)
			}
			continue
		}
		// A port is folded away rather than refused, so only what is left after
		// that has to be a host: same reasoning as checkHost, a surviving colon is
		// a host only when the whole thing is an IPv6 literal.
		if h := normalizeHost(p); strings.Contains(h, ":") && net.ParseIP(h) == nil {
			return fmt.Errorf("proxycfg: host filter %q is not a host name or pattern", raw)
		}
	}
	return nil
}

// Sanitize returns the entries that can actually be used, in list order, with
// the fields that cannot mean anything for their kind cleared, and with every
// entry carrying an ID and a compact order index.
//
// Unusable entries are dropped whole rather than repaired. A proxy row with no
// host or no port is not "a proxy missing a detail": kept and enabled it would
// either fail every download routed through it or, worse, be read as no proxy at
// all and send that traffic out over the connection the user was hiding.
//
// It is idempotent, so a caller that is unsure whether a list has been through
// it can simply call it again.
func Sanitize(in []Entry) []Entry {
	out := make([]Entry, 0, len(in))
	for _, e := range in {
		// Judged before it is folded, and on exactly what the API judges, so the
		// two can never disagree about a row. The order matters: folding a filter
		// turns a pasted "http://example.org" into the innocent-looking pattern
		// "http", so a check that ran afterwards would keep the very row the API
		// had just refused.
		if Validate(e) != nil {
			continue
		}
		out = append(out, clean(e))
	}
	if len(out) == 0 {
		return nil
	}
	// Stable, so two entries the user left on the same order index keep the
	// sequence they were written in instead of swapping about between saves.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	identify(out)
	return out
}

// clean normalises one entry. A port typed into the host field is dropped by
// normalizeHost; the port field is the one that counts.
func clean(e Entry) Entry {
	if k, ok := kindOf(e.Kind); ok {
		e.Kind = k
	}
	e.ID = strings.TrimSpace(e.ID)
	e.Host = normalizeHost(e.Host)
	e.Username = strings.TrimSpace(e.Username)
	e.Filter = cleanFilter(e.Filter)
	if e.MaxDownloads < 0 {
		e.MaxDownloads = 0
	}
	if e.MaxDownloads > maxDownloadsCap {
		e.MaxDownloads = maxDownloadsCap
	}
	switch e.Kind {
	case KindNone, KindDirect:
		// Neither kind connects to anywhere, so an endpoint left behind from
		// when the row was a proxy is not configuration, it is a trap for
		// whoever reads the file next. The filter stays: on a direct entry it is
		// the entire point.
		e.Host, e.Port, e.Username, e.Password = "", 0, "", ""
	case KindSOCKS4, KindSOCKS4A:
		// SOCKS4 has a user id field and no password field, so keeping one would
		// persist a secret that can never be sent anywhere.
		e.Password = ""
	}
	// A password with no user name to send it as cannot be used by any of these
	// protocols. The password itself is never trimmed: leading and trailing
	// spaces are legal in one, and eating them would lock the user out of a
	// proxy that works everywhere else.
	if e.Username == "" {
		e.Password = ""
	}
	return e
}

// cleanFilter drops blanks and duplicates and folds every pattern into the form
// Matches compares against, so a filter typed as "Example.ORG " is not a filter
// that quietly never matches anything.
func cleanFilter(in []string) []string {
	var out []string
	seen := make(map[string]bool, len(in))
	for _, p := range in {
		p = normalizeHost(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// identify gives every entry an ID and renumbers the order.
//
// The first entry to claim an ID keeps it, so an ID the API already handed to a
// client survives an edit anywhere else in the list; anything blank or claimed
// twice takes the lowest free number. Renumbering the order matters because a UI
// that reorders by drag and drop writes the whole list back and does not always
// renumber it: left alone, two entries sharing an order index would be walked in
// whatever sequence the sort happened to leave them in, not the one the user is
// looking at.
func identify(out []Entry) {
	taken := make(map[string]bool, len(out))
	keep := make([]bool, len(out))
	for i := range out {
		if id := out[i].ID; id != "" && !taken[id] {
			taken[id] = true
			keep[i] = true
		}
	}
	next := 1
	for i := range out {
		if !keep[i] {
			for taken[strconv.Itoa(next)] {
				next++
			}
			out[i].ID = strconv.Itoa(next)
			taken[out[i].ID] = true
		}
		out[i].Order = i
	}
}

// Merge carries the passwords of prev into next for the entries that came back
// without one.
//
// Redacted strips the password before the list leaves the process, so a settings
// page that reads the list, flips one checkbox and posts it back would otherwise
// clear every proxy password on the way through.
//
// A password is carried over only while the row still describes the same
// connection with the same credentials: same kind, same host, same port, same
// user name. Anything else and it is dropped, for two reasons. A changed user
// name plainly means different credentials, so clearing the user name is also
// how a password is cleared. And the client posting this back is the one the
// password was withheld from: if a changed host kept it, that client could aim a
// secret it was never allowed to read at a machine it controls and have the app
// send it there on the next download.
//
// Call it before Sanitize: it matches on the IDs the previous Sanitize handed
// out.
func Merge(next, prev []Entry) []Entry {
	if len(next) == 0 || len(prev) == 0 {
		return next
	}
	old := make(map[string]Entry, len(prev))
	for _, e := range prev {
		if e.ID != "" && e.Password != "" && e.Username != "" {
			old[e.ID] = e
		}
	}
	out := make([]Entry, len(next))
	copy(out, next)
	for i := range out {
		if out[i].Password != "" {
			continue
		}
		if e, ok := old[out[i].ID]; ok && sameConnection(e, out[i]) {
			out[i].Password = e.Password
		}
	}
	return out
}

// sameConnection reports whether two versions of a row still point at the same
// place with the same credentials. Both sides are folded first because prev has
// been through Sanitize and next has not: a user name the client sent back with
// its spaces intact is the same user name.
func sameConnection(a, b Entry) bool {
	ak, _ := kindOf(a.Kind)
	bk, _ := kindOf(b.Kind)
	return ak == bk &&
		normalizeHost(a.Host) == normalizeHost(b.Host) &&
		a.Port == b.Port &&
		strings.TrimSpace(a.Username) == strings.TrimSpace(b.Username)
}

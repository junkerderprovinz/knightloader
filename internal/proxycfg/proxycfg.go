// Package proxycfg models JDownloader's Connection Manager: a user-ordered list
// of outbound connections that downloads are spread across. It owns the list,
// the rules for a usable entry, and a picker that hands out the next
// connection for a download.
//
// Nothing here carries a download. Only Probe (probe.go) opens a socket, to
// tell the connection page whether a proxy works.
//
// "none" and "direct" are different:
//
//	none:   an inert row that names no connection; the picker never returns
//	        it. It is kept so a save does not delete a row still being edited.
//	direct: a real choice to go out over the machine's own connection,
//	        bypassing every proxy. A direct entry filtered to "nas.local"
//	        keeps that host off a whole-app proxy.
//
// In general an entry whose filter matches the target host is preferred over
// an entry with no filter, or a catch-all proxy would still take its turn on
// a host the user routed elsewhere.
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

// Kind is the transport an entry uses. It is a string because settings.json is
// read by hand.
type Kind string

const (
	KindNone    Kind = "none"   // inert row: never picked
	KindDirect  Kind = "direct" // unproxied
	KindHTTP    Kind = "http"
	KindHTTPS   Kind = "https"
	KindSOCKS4  Kind = "socks4"
	KindSOCKS4A Kind = "socks4a"
	KindSOCKS5  Kind = "socks5"
)

// maxDownloadsCap mirrors the cap settings puts on global concurrency; a
// larger per-connection limit could never take effect.
const maxDownloadsCap = 64

// DirectID is the identity of the direct gateway: the machine's own
// connection, offered in the same list as the configured rows. Tasks and
// columns name connections by id, and an empty id could not tell "not routed
// yet" from "chosen to go out unproxied".
//
// identify only hands out decimal ids and reserves this one, so a posted row
// claiming "direct" is renumbered instead of shadowing the gateway.
const DirectID = "direct"

// Entry is one outbound connection in the user's list.
type Entry struct {
	// ID keys the in-flight counter Pick consults and matches an edited row on
	// save. Sanitize fills in a missing or duplicated one.
	ID   string `json:"id"`
	Kind Kind   `json:"type"`
	Host string `json:"host,omitempty"`
	Port int    `json:"port,omitempty"`

	// Username and Password are the proxy's own credentials. The password is
	// stored in the clear like the rest of settings.json and must go through
	// Redacted before it leaves the process. String omits it, so %v and %q are
	// safe; %#v and other reflection still show it.
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`

	// HasPassword says a password is stored without revealing it, so the form
	// can tell a redacted entry from one with no password and the user does
	// not retype it. It is derived: clean clears it on the way in, Redacted
	// sets it on the way out, and a client cannot set it.
	HasPassword bool `json:"hasPassword,omitempty"`

	// Enabled is the user's on/off switch, separate from KindNone so switching
	// a proxy off keeps its host and credentials.
	Enabled bool `json:"enabled"`
	// Order is the position in the list, which is the order Pick walks.
	Order int `json:"order"`
	// Filter restricts the entry to the hosts it names. Empty makes it a
	// catch-all, which ranks below a matching filter.
	Filter []string `json:"filter,omitempty"`
	// MaxDownloads caps how many downloads may share this entry at once. Zero
	// means the picker's default.
	MaxDownloads int `json:"maxDownloads,omitempty"`
}

// Direct is the direct gateway: an ordinary, unproxied download. It is the
// answer when no entry claims a host and a connection a task may name
// outright. It is not in the picker's rotation, where it would act as an
// unfiltered catch-all and send a share of every list's downloads out
// unproxied.
func Direct() Entry {
	return Entry{ID: DirectID, Kind: KindDirect, Enabled: true}
}

// isGateway tells the built-in direct gateway from a direct row the user
// configured, which has a filter, a place in the rotation and its own limit.
func (e Entry) isGateway() bool { return e.ID == DirectID }

// kindOf folds whatever is in the file into a known Kind. An empty kind is
// none: a row added and never filled in is inert, and dropping it would
// delete it on the next save.
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

// usable reports whether the picker may ever return e.
func (e Entry) usable() bool {
	return e.Enabled && e.Kind != KindNone
}

// scheme is the URL scheme for e, or "" for the kinds that are not a proxy.
// "socks5" rather than "socks5h" because every version of net/http and
// x/net/proxy accepts it, and both let the proxy resolve the host anyway.
func (e Entry) scheme() string {
	switch e.Kind {
	case KindHTTP, KindHTTPS, KindSOCKS4, KindSOCKS4A, KindSOCKS5:
		return string(e.Kind)
	}
	return ""
}

// URL builds the proxy URL for e, for http.ProxyURL or x/net/proxy.FromURL.
// It returns nil for none and direct, which is what Transport.Proxy expects
// for an unproxied request. Log the Entry, never this URL: url.URL.String
// prints the password.
func (e Entry) URL() *url.URL {
	scheme := e.scheme()
	if scheme == "" {
		return nil
	}
	// JoinHostPort brackets an IPv6 literal, whose colons url.URL would
	// otherwise read as a port.
	u := &url.URL{Scheme: scheme, Host: net.JoinHostPort(e.Host, strconv.Itoa(e.Port))}
	switch {
	case e.Username == "":
	case e.Kind == KindSOCKS4 || e.Kind == KindSOCKS4A:
		// SOCKS4 has no password field, so the password stays out of the URL.
		u.User = url.User(e.Username)
	case e.Password == "":
		u.User = url.User(e.Username)
	default:
		u.User = url.UserPassword(e.Username, e.Password)
	}
	return u
}

// NeedsOwnDialer reports whether the caller has to carry the connection
// itself. Neither net/http nor x/net/proxy speaks SOCKS4, so such an entry
// needs a dialer the caller supplies.
func (e Entry) NeedsOwnDialer() bool {
	return e.Kind == KindSOCKS4 || e.Kind == KindSOCKS4A
}

// Matches reports whether e's host filter covers host. An entry with no
// filter matches everything.
//
// A plain pattern covers the domain and everything under it, so "example.org"
// matches "dl2.example.org" without guessing CDN names. A wildcard pattern
// goes through path.Match. Both sides are folded here because callers often
// hold an entry that has not been through Sanitize.
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
		// Validate refuses unparseable patterns; one that slipped past matches
		// nothing rather than everything.
		return err == nil && ok
	}
	return host == pattern || strings.HasSuffix(host, "."+pattern)
}

// clone returns e with its own copy of the filter, so a caller editing what
// it was given cannot change a live Picker's filter from another goroutine.
func (e Entry) clone() Entry {
	if len(e.Filter) > 0 {
		e.Filter = append([]string(nil), e.Filter...)
	}
	return e
}

// normalizeHost folds a host into the form filters are compared in. A caller
// passing "example.org:443" would otherwise match no filter and bypass the
// proxy chosen for it.
func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	// SplitHostPort fails on a bare IPv6 literal, which is left alone.
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	if strings.HasPrefix(h, "[") && strings.HasSuffix(h, "]") {
		// Only a real bracketed address is unwrapped, not a pattern that
		// starts with a character class.
		if inner := h[1 : len(h)-1]; net.ParseIP(inner) != nil {
			h = inner
		}
	}
	// The DNS root dot is legal in a URL and never typed into a filter.
	return strings.TrimSuffix(h, ".")
}

// String describes the entry for a log line or an error message. The password
// is never part of it, since entries end up behind %v in many places.
func (e Entry) String() string {
	// The folded kind, since entries are logged before Sanitize too.
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

// Redacted returns a copy of e with the password removed, for logs, errors and
// above all the API. The password is dropped rather than masked; Merge puts it
// back when the list is posted again, and HasPassword tells the client there
// is one.
func (e Entry) Redacted() Entry {
	e.HasPassword = e.Password != ""
	e.Password = ""
	return e
}

// Validate reports why an entry cannot be used. Sanitize drops whatever this
// rejects, so the API should call it first and refuse the save with the
// reason instead of letting the row vanish.
func Validate(e Entry) error {
	k, ok := kindOf(e.Kind)
	if !ok {
		return fmt.Errorf("proxycfg: %q is not a connection type", string(e.Kind))
	}
	// The filter is checked for every kind; it is the only field a direct
	// entry has.
	if err := checkFilter(e.Filter); err != nil {
		return err
	}
	if k == KindNone || k == KindDirect {
		return nil // neither names an endpoint
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

// checkHost refuses anything that would not survive being put into a URL,
// such as a pasted "http://proxy.lan:8080/", whose scheme and path would
// otherwise point the proxy somewhere else.
func checkHost(host string) error {
	if len(host) > 255 {
		return errors.New("proxycfg: host is too long")
	}
	if strings.ContainsAny(host, " \t\r\n/\\@?#%") {
		return fmt.Errorf("proxycfg: %q is not a host name or address", host)
	}
	// A colon left after normalisation is a host only in an IPv6 literal.
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return fmt.Errorf("proxycfg: %q is not a host name or address", host)
	}
	return nil
}

// checkFilter refuses a pattern that could never match. Such a filter would
// stop the entry claiming its host, and the traffic would go out unproxied
// without a word.
//
// It works on what the user typed, not the folded form: normalizeHost folds a
// pasted "http://example.org" into the valid but useless pattern "http".
func checkFilter(patterns []string) error {
	for _, raw := range patterns {
		p := strings.ToLower(strings.TrimSpace(raw))
		if p == "" {
			continue // cleanFilter drops blanks
		}
		if len(p) > 255 {
			return fmt.Errorf("proxycfg: host filter %q is too long", raw)
		}
		// Characters no host name or glob over one ever holds; checkHost's
		// rules do not fit, since globs use * ? [ ].
		if strings.ContainsAny(p, " \t\r\n/\\@#%") {
			return fmt.Errorf("proxycfg: host filter %q is not a host name or pattern", raw)
		}
		if hasWildcard(p) {
			if _, err := path.Match(p, "example.org"); err != nil {
				return fmt.Errorf("proxycfg: host filter %q is not a valid pattern: %w", raw, err)
			}
			continue
		}
		// A port is folded away; a colon left after that must be an IPv6
		// literal.
		if h := normalizeHost(p); strings.Contains(h, ":") && net.ParseIP(h) == nil {
			return fmt.Errorf("proxycfg: host filter %q is not a host name or pattern", raw)
		}
	}
	return nil
}

// Sanitize returns the usable entries in list order, with fields meaningless
// for their kind cleared and every entry given an ID and a compact order
// index. It is idempotent.
//
// Unusable entries are dropped, not repaired: an enabled proxy row without a
// host or port would fail every download or be read as no proxy at all.
func Sanitize(in []Entry) []Entry {
	out := make([]Entry, 0, len(in))
	for _, e := range in {
		// Judged before folding, exactly as the API judges it, since folding
		// turns a pasted URL filter into a harmless-looking pattern.
		if Validate(e) != nil {
			continue
		}
		out = append(out, clean(e))
	}
	if len(out) == 0 {
		return nil
	}
	// Stable, so entries sharing an order index keep their written sequence.
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
	// A client cannot claim a stored password; Redacted sets this on the way
	// out.
	e.HasPassword = false
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
		// Neither connects anywhere, so a leftover endpoint is cleared. The
		// filter stays; on a direct entry it is the point.
		e.Host, e.Port, e.Username, e.Password = "", 0, "", ""
	case KindSOCKS4, KindSOCKS4A:
		// SOCKS4 has no password field, so the password could never be sent.
		e.Password = ""
	}
	// No protocol here sends a password without a user name. The password is
	// never trimmed, since spaces are legal in it.
	if e.Username == "" {
		e.Password = ""
	}
	return e
}

// cleanFilter drops blanks and duplicates and folds every pattern into the
// form Matches compares against.
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
// The first entry to claim an ID keeps it, so IDs already handed to a client
// survive; blank or duplicate IDs get the lowest free number. The order is
// renumbered because a drag-and-drop UI writes the list back without always
// renumbering it.
func identify(out []Entry) {
	taken := make(map[string]bool, len(out)+1)
	// Reserved, so a row cannot shadow the direct gateway and take over tasks
	// pinned to it.
	taken[DirectID] = true
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

// Merge carries the passwords of prev into the entries of next that came back
// without one, since Redacted strips them before the list reaches the client.
//
// A password is carried over only while kind, host, port and user name are
// unchanged. Clearing the user name is how a password is cleared, and a
// client that never saw the password must not be able to point it at a host
// it controls.
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

// sameConnection reports whether two versions of a row point at the same place
// with the same credentials. Both are folded, since next has not been through
// Sanitize yet.
func sameConnection(a, b Entry) bool {
	ak, _ := kindOf(a.Kind)
	bk, _ := kindOf(b.Kind)
	return ak == bk &&
		normalizeHost(a.Host) == normalizeHost(b.Host) &&
		a.Port == b.Port &&
		strings.TrimSpace(a.Username) == strings.TrimSpace(b.Username)
}

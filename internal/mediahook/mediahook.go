// Package mediahook is the stored address a category drawer points at, called
// once after the last file of a package has arrived and been moved into the
// folder it belongs in.
//
// It exists because a media server does not notice new files. Jellyfin and Plex
// both scan on a timer or when told, and "told" is one HTTP call - so the whole
// feature is: keep an address, keep one header value for it out of harm's way,
// and make the call at the one moment it is worth making. This package knows
// nothing about Jellyfin, Plex, Emby or anything else by name, and that is
// deliberate: the moment a build ships a "Jellyfin" mode it owes everybody an
// "Emby" mode, a "Kodi" mode and an answer about the next one. An address, a
// method and a header covers all of them and dates badly for none.
//
// # Where the value is, and why it is not here
//
// Hook has NO field for the header VALUE, and that is a property of this type
// rather than of whichever route renders it. Hook is a SETTINGS field: it is
// serialised into settings.json, it travels into the diagnostics bundle people
// attach to public bug reports (routes_diagnostics.go serialises
// settings.Settings.Redacted(), and Redacted() covers exactly Reconnect and
// Connections), and routes_features.go reflects over settings.Settings to build
// the Advanced key table, so a string field added here would ALSO become an
// editable text row in that table. A token has no business in any of the three.
// So the value is sealed in the shared accounts.Store under the pseudo service
// id below, exactly the arrangement hostheaders, hosterauth and the yt-dlp
// cookie jars already use, and store.go is the only file in this package that
// ever holds one.
//
// The URL itself IS in settings.json in the clear, and there is nothing to be
// done about that here - it is the one field a person has to be able to read on
// the settings page and in their own settings.json. Which is why the interface's
// hint for it says out loud that a token belongs in the header and not in the
// query part of the address: Plex's own documented refresh call carries its
// token in the query string, so the obvious paste is the harmful one.
package mediahook

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Service is the pseudo catalogue id the header values are filed under in the
// shared accounts.Store, with the hook id as the "account" half of the (service,
// account) key that store already indexes by.
//
// The same arrangement hostheaders.Service, hosterauth.Service and
// ytdlp.CookieService use, and for the same reason: accounts.Catalogue is a
// short, hand-maintained list of real services a picker searches, while this is
// one row per address from a list only the user's own typing decides.
const Service = "mediahook"

// MaxHooks caps the table. It is a menu a drawer picks one entry from, and the
// same argument settings.MaxCategories makes applies harder here: a box has one
// media server, maybe two, and sixteen is already generous enough that the
// ceiling can only ever be hit by a file somebody generated.
const MaxHooks = 16

// MaxHookID and maxHookName bound the two strings a person types. The id
// travels in Category.Notify and is compared on every finished package, so it
// is kept short; the name is only ever read.
const (
	MaxHookID   = 64
	maxHookName = 120
)

// MaxWaitSeconds is the longest coalesce window a row may ask for. An hour,
// because the window's purpose is to fold a burst of packages into one call and
// no burst worth folding lasts longer than that - and because a window longer
// than the ceiling below would be a call the runner promises and a restart
// silently eats.
const MaxWaitSeconds = 3600

// The two methods this build sends. A closed list rather than "whatever the
// user types", because every other verb raises a question this feature has no
// answer to: PUT and PATCH need a body, DELETE against a library scan endpoint
// is a request nobody meant to make, and a HEAD that works proves nothing about
// whether the scan ran.
const (
	MethodGet  = "GET"
	MethodPost = "POST"
)

// Hook is one stored address.
//
// It is a settings row, so every field here is safe to write to disk, to show on
// a page and to put in a bug report. See the package comment for the one field
// that is therefore NOT here.
type Hook struct {
	// ID is the stable key a drawer points at (settings.Category.Notify) and the
	// account half of the credential key the header value is sealed under. It is
	// the one field that may not change: renaming it would leave every drawer
	// pointing at an address that no longer answers, and would orphan the sealed
	// value under the old name.
	ID string `json:"id"`
	// Name is what a picker shows. It may change freely; nothing points at it.
	Name string `json:"name,omitempty"`
	// URL is the whole address that gets called, http or https, host and port
	// included. Absolute always: a relative address here would be resolved
	// against nothing at all.
	URL string `json:"url"`
	// Method is MethodGet or MethodPost. A POST is sent with no body - every
	// library-refresh endpoint this is aimed at takes the instruction in the path
	// and the credential in a header, and a body invented here would only be one
	// more thing for the far end to reject.
	Method string `json:"method"`
	// HeaderName is the one header sent with the call, "X-Emby-Token" and
	// "X-Plex-Token" being the two anybody actually types. Empty sends no extra
	// header, which is right for a server on a trusted LAN that asks for nothing.
	//
	// ONE header and not a list, on purpose. hostheaders already exists for "a
	// set of headers for one origin" and is a whole subsystem with an origin
	// scope, a redirect guard and an importer; a second one grown here by
	// accretion would end up as a worse copy of it. A media server that needs two
	// headers is a case for wiring this at hostheaders instead, and nobody has
	// one yet.
	HeaderName string `json:"headerName,omitempty"`
	// WaitSeconds is how long this address is left alone after a package
	// finishes, so that twenty packages finishing in one sweep become one call.
	//
	// 0 is a real answer and not "unset": it means call after every package. That
	// distinction is why this has no omitempty - a client has to be able to see
	// the field and set it to zero on purpose.
	WaitSeconds int `json:"waitSeconds"`
}

// Methods is the menu GET /api/options serves. Built from the constants above so
// that a method this build cannot send can never be offered as a choice.
func Methods() []string { return []string{MethodGet, MethodPost} }

// HookID folds the spellings of one id into one, and is the only place that
// decides what an id may contain.
//
// The same rule hostheaders.ProfileID applies, deliberately verbatim rather than
// settings.CategoryID's fold: this id is also an accounts.Store account key and
// is typed into a settings page by hand, so "letters, digits and - _ or ., or it
// is not an id" is a rule a person can hold in their head and a refusal can
// quote. CategoryID's rewrite-anything-into-dashes fold is right for a key
// DERIVED from a name and wrong for one somebody typed: it would silently turn
// "jellyfin lan" into "jellyfin-lan" and leave them wondering which of the two
// their drawer is pointing at.
//
// The empty string is the answer for anything unusable, and every caller reads
// it as "there is no such id" rather than as a value.
func HookID(raw string) string {
	id := strings.ToLower(strings.TrimSpace(raw))
	if id == "" || len(id) > MaxHookID {
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

// Validate reports the first thing wrong with one row, in words meant for
// whoever is looking at the form.
//
// It refuses rather than repairs, for the reason settings.ValidateCategories
// does: a row that silently loses a field on save is a row the user goes on
// believing in. Sanitize is the second line of defence, for a settings.json
// somebody edited by hand, and it drops what this would have refused.
func (h Hook) Validate() error {
	if HookID(h.ID) == "" {
		return fmt.Errorf("the name %q holds something other than letters, digits and - _ or . (at most %d characters), so nothing could point at it", h.ID, MaxHookID)
	}
	u, err := url.Parse(strings.TrimSpace(h.URL))
	if err != nil {
		// The parse error is not quoted. It names the offending byte position in
		// a string this field is exactly the wrong place to echo: a pasted Plex
		// address carries its token in the query, and this sentence reaches the
		// log ring and from there the diagnostics bundle.
		return fmt.Errorf("%s: that is not an address this build can call. Give the whole address, scheme included (http://jellyfin.lan:8096/...)", h.ID)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("%s: the address has to start with http:// or https://; nothing else is called from here", h.ID)
	}
	if u.Host == "" {
		return fmt.Errorf("%s: the address names no host. A relative address is resolved against nothing at all here", h.ID)
	}
	if m := strings.ToUpper(strings.TrimSpace(h.Method)); m != MethodGet && m != MethodPost {
		return fmt.Errorf("%s: %q is not a method this build sends; use %s or %s", h.ID, h.Method, MethodGet, MethodPost)
	}
	if name := strings.TrimSpace(h.HeaderName); name != "" && !isHeaderToken(name) {
		// The name IS echoed and the value never is: "this address sends an
		// X-Emby-Token" is exactly what a settings page has to be able to say,
		// and headerNameForError next door in routes_hostheaders.go draws the
		// same line for the same field.
		return fmt.Errorf("%s: %q is not a header name. A header name holds letters, digits and - _ . and nothing else", h.ID, name)
	}
	if h.WaitSeconds < 0 || h.WaitSeconds > MaxWaitSeconds {
		return fmt.Errorf("%s: a wait of %d seconds is outside 0..%d", h.ID, h.WaitSeconds, MaxWaitSeconds)
	}
	return nil
}

// isHeaderToken reports whether name is an RFC 7230 token as far as this field
// needs to care: the shape net/http will send unmangled, and narrow enough that
// a whole pasted header line ("X-Emby-Token: abc") is refused rather than sent
// as a header whose name contains a colon and a secret.
func isHeaderToken(name string) bool {
	if len(name) > 128 {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}

// Sanitize bounds the table and drops the rows that could never be called or
// could never be pointed at.
//
// The slice is rebuilt rather than edited in place, matching sanitizeCategories
// and sanitizeHostRules: what the caller handed in is still holding the same
// backing array, and a settings document that keeps changing underneath whoever
// submitted it is a bug people find months later.
//
// A duplicate id is refused loudly by ValidateMediaHooks long before it gets
// here, so the drop below only ever fires on a hand-edited settings.json. The
// FIRST one is kept, because it is the one a picker built from this slice shows
// first, which makes the surviving row the one the person looking at the page
// would have expected.
func Sanitize(in []Hook) []Hook {
	if len(in) == 0 {
		return in
	}
	out := make([]Hook, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, h := range in {
		id := HookID(h.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		h.ID = id
		h.Name = trimTo(h.Name, maxHookName)
		h.URL = strings.TrimSpace(h.URL)
		h.Method = strings.ToUpper(strings.TrimSpace(h.Method))
		if h.Method != MethodGet && h.Method != MethodPost {
			// Not dropped and not left as typed: a row whose method this build
			// cannot send would otherwise be a stored address that never calls
			// anything, and GET is the request that changes least at the far end.
			h.Method = MethodGet
		}
		h.HeaderName = strings.TrimSpace(h.HeaderName)
		if !isHeaderToken(h.HeaderName) {
			// The NAME is dropped rather than the row. A header this build cannot
			// send is one the far end was never going to accept anyway, and taking
			// the whole address away over it would lose the drawer that points at
			// it as well.
			h.HeaderName = ""
		}
		if h.WaitSeconds < 0 {
			h.WaitSeconds = 0
		}
		if h.WaitSeconds > MaxWaitSeconds {
			h.WaitSeconds = MaxWaitSeconds
		}
		out = append(out, h)
		if len(out) == MaxHooks {
			break
		}
	}
	return out
}

// trimTo trims the whitespace and then the length, on a rune boundary so a
// multi-byte name cannot be cut into invalid UTF-8 and land in the JSON as a
// replacement character. The same helper settings.trimTo is, copied rather than
// exported from there because this package must not import internal/settings -
// settings imports this one.
func trimTo(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return strings.ToValidUTF8(s[:max], "")
}

// Host is host[:port] as the call will actually reach it, or "" for an address
// that does not parse. It is what the card prints as "the call goes to X", which
// is the one line that catches a typo before it costs anybody an evening.
func (h Hook) Host() string {
	u, err := url.Parse(strings.TrimSpace(h.URL))
	if err != nil {
		return ""
	}
	return u.Host
}

// IsPrivateTarget reports whether the address is on this machine or on a private
// network, so the interface can say out loud when it is NOT.
//
// It answers false for a host name it cannot resolve from the address alone, and
// that direction is deliberate: "jellyfin.lan" is almost certainly private and
// answering true for it would mean guessing on the strength of a name. False
// draws one extra sentence saying the call leaves this machine, which is a
// sentence worth reading twice about an address that carries a token; true
// withholds it, and withholding it wrongly is the mistake that cannot be seen.
//
// No DNS lookup, ever. This is called from a settings route and a card render,
// and a resolver hanging on a LAN name would stall both - and the answer would
// be a fact about this moment's DNS rather than about the address.
func (h Hook) IsPrivateTarget() bool {
	u, err := url.Parse(strings.TrimSpace(h.URL))
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

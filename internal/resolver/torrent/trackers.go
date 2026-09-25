package torrent

import (
	"net/url"
	"strings"
)

// ValidTracker reports whether raw is an announce address a torrent can be
// given: http, https, udp, ws or wss, with a host.
func ValidTracker(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && usableTrackerSchemes[strings.ToLower(u.Scheme)] && u.Hostname() != ""
}

// BannedHost is the host one line of a banned-tracker list names: a bare host
// name, a host with a port, or a whole announce address. It is "" for a line
// that names none.
func BannedHost(line string) string {
	// A wildcard adds nothing, since a banned host covers its subdomains.
	line = strings.TrimPrefix(strings.TrimSpace(line), "*.")
	if line == "" {
		return ""
	}
	if !strings.Contains(line, "://") {
		// Parsed as the authority of an address, so "tracker.example.org:6969"
		// is a host and a port rather than a scheme and an opaque rest.
		line = "//" + line
	}
	u, err := url.Parse(line)
	if err != nil {
		return ""
	}
	return trackerHostname(u.Hostname())
}

// Banned returns the host of the first tracker that a line of banned names,
// and whether there is one. A banned host covers its subdomains, since a
// tracker that answers on several names is one tracker.
func Banned(trackers, banned []string) (string, bool) {
	hosts := make([]string, 0, len(banned))
	for _, line := range banned {
		if h := BannedHost(line); h != "" {
			hosts = append(hosts, h)
		}
	}
	if len(hosts) == 0 {
		return "", false
	}
	for _, t := range trackers {
		u, err := url.Parse(strings.TrimSpace(t))
		if err != nil {
			continue
		}
		host := trackerHostname(u.Hostname())
		if host == "" {
			continue
		}
		for _, h := range hosts {
			if host == h || strings.HasSuffix(host, "."+h) {
				return host, true
			}
		}
	}
	return "", false
}

func trackerHostname(h string) string {
	return strings.TrimSuffix(strings.ToLower(h), ".")
}

// ExtraTrackers is extra as the torrent behind uri may be given it: without
// the trackers it announces to already, without banned ones, without
// duplicates, and nothing at all for a private torrent.
//
// An uploaded .torrent says whether it is private. A magnet does not until its
// metadata arrives, which is after the engine has been handed its trackers, so
// a magnet counts as private when one of its own trackers carries a passkey.
// That key is how a private tracker tells its members apart, and no public
// tracker asks for one. A private tracker that knows its members by address or
// cookie instead is not spotted, and its magnet gets the extra trackers.
func ExtraTrackers(uri string, extra, banned []string) []string {
	if len(extra) == 0 {
		// Before Describe, which for an uploaded torrent parses the whole file
		// again, on every start of one, under the app's lock.
		return nil
	}
	md, err := (Resolver{}).Describe(uri)
	if err != nil || md.Private {
		return nil
	}
	if IsMagnet(uri) {
		for _, t := range md.Trackers {
			if carriesPasskey(t) {
				return nil
			}
		}
	}
	seen := make(map[string]bool, len(md.Trackers)+len(extra))
	for _, t := range md.Trackers {
		seen[t] = true
	}
	var out []string
	for _, t := range extra {
		t = strings.TrimSpace(t)
		if seen[t] || !ValidTracker(t) {
			continue
		}
		seen[t] = true
		if _, bad := Banned([]string{t}, banned); bad {
			continue
		}
		out = append(out, t)
		if len(out) == MaxTrackers {
			break
		}
	}
	return out
}

// passkeyParams are the query keys private trackers put a member's key under.
var passkeyParams = []string{"passkey", "pk", "authkey", "torrent_pass", "uk", "key", "auth", "token", "secure", "uid"}

// carriesPasskey reports whether an announce address identifies whoever
// announces: one of the query keys private trackers use for it, or a path
// segment or query value in the shape of a passkey (see looksLikeKey).
func carriesPasskey(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	q := u.Query()
	for _, k := range passkeyParams {
		if q.Get(k) != "" {
			return true
		}
	}
	for _, vs := range q {
		for _, v := range vs {
			if looksLikeKey(v) {
				return true
			}
		}
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if looksLikeKey(seg) {
			return true
		}
	}
	return false
}

// looksLikeKey is 16 or more ASCII letters and digits with at least one digit.
// Passkeys are 32 hex or alphanumeric characters; a public tracker's path is a
// word like "announce".
func looksLikeKey(s string) bool {
	if len(s) < 16 {
		return false
	}
	digit := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			digit = true
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		default:
			return false
		}
	}
	return digit
}

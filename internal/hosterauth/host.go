package hosterauth

import "strings"

// normalizeHost lower-cases a domain and strips a leading "www.", the same
// rule internal/resolver/jd.normalizeHost applies for the same reason: a
// stored host id, a browser-pasted URL's host and JD's own reported hostname
// all need to compare equal regardless of case or a leading "www.".
// Lower-cased before the prefix is stripped so an all-caps "WWW." still
// matches.
func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	return strings.TrimPrefix(h, "www.")
}

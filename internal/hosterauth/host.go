package hosterauth

import "strings"

// normalizeHost lower-cases a domain and strips a leading "www.", as
// internal/resolver/jd.normalizeHost does, so stored hosts, pasted URLs and
// JD's reported hostnames compare equal.
func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	return strings.TrimPrefix(h, "www.")
}

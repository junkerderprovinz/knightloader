package hosterauth

import "strings"

// normalizeHost lower-cases a domain and strips a leading "www.", so stored
// hosts, pasted URLs and JD's reported hostnames compare equal. It keeps an
// alias domain as typed, since a login is stored under the host it was entered
// for; routing folds aliases with internal/hostalias.
func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	return strings.TrimPrefix(h, "www.")
}

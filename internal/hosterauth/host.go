package hosterauth

import (
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/hostalias"
)

// normalizeHost lower-cases a domain and strips a leading "www.", so stored
// hosts, pasted URLs and JD's reported hostnames compare equal. It keeps an
// alias domain as typed, since a login is stored under the host it was entered
// for; routing folds aliases with internal/hostalias.
func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	return strings.TrimPrefix(h, "www.")
}

// accountKey is what a stored login and JD's account are matched on. JD files
// an account under the hoster's main domain whatever it was added as, so a
// login saved as rg.to is the account JD reports as rapidgator.net.
func accountKey(h string) string {
	return hostalias.Canonical(h)
}

package app

import "strings"

// The multihoster list: which of JDownloader's ~714 known hosts are services
// that unlock OTHER hosts rather than hosting files themselves.
//
// It is kept by hand, and that is a decision rather than an oversight. The
// automatic route was tried first and measured: JD's own deprecated API answers
// accounts/getAccountInfo with `{"hostname":"ddownload.com","infoMap":null}` -
// the per-account detail it would need to carry (JD tracks a multiHostSupport
// list internally) never reaches the wire. Nothing else in the API distinguishes
// the two kinds either; listPremiumHoster is one flat list of names.
//
// So the choice was between a maintained list and no distinction at all, and
// jdp took the list (2026-09-07: "hast du wirklich alle debrid konten aus der
// hoster liste in die debrid liste verschoben? in der hoster liste sind nämlich
// noch einige?", then "Gepflegte Liste anlegen").
//
// What this list is NOT for: hiding these services. KnightLoader has no backend
// of its own for any of them, so JD is the only way to use them at all, and the
// hoster picker is the only place they can be configured. Filtering them out
// would make them unreachable. They are marked, not removed - the services that
// KnightLoader does drive itself are the ones debridServiceDomains
// (app_hosterauth.go) takes out, because those genuinely appear twice.
//
// Entries are bare hostnames as JD names them, lower-case, without www. The
// list came from reading JD's own premium-hoster list on the preview instance
// on 2026-09-07 and picking out the services that advertise themselves as
// multihosters or debriders. It will go stale; a name that disappears from JD
// costs nothing here, and a new one simply is not marked until somebody adds it.
var multihosterDomains = map[string]bool{
	"bestdebrid.com":    true,
	"cocoleech.com":     true,
	"cooldebrid.com":    true,
	"dailyleech.com":    true,
	"debriditalia.com":  true,
	"debridplanet.com":  true,
	"deepbrid.com":      true,
	"fakirdebrid.net":   true,
	"leechall.io":       true,
	"mega-debrid.eu":    true,
	"multiup.io":        true,
	"multivip.net":      true,
	"mydebrid.com":      true,
	"neodebrid.com":     true,
	"premium.rpnet.biz": true,
	"proleech.link":     true,
	"put.io":            true,
	"simply-debrid.com": true,
	"zevera.com":        true,
}

// IsMultihoster reports whether a host is a service that unlocks other hosts.
//
// Compared through serviceKey, the same normalisation the debrid filter uses,
// so a stored "https://www.zevera.com/" and JD's own "zevera.com" are the same
// service here as well - the mismatch that let premiumize.me through the debrid
// filter for a whole release.
func IsMultihoster(host string) bool {
	return multihosterDomains[serviceKey(host)]
}

// multihosterCount is how many of the given hosts are multihosters. Used by the
// tests to catch a list that has drifted so far from JD's own that it no longer
// matches anything - a list nobody can see is not maintained, it is decoration.
func multihosterCount(hosts []string) int {
	n := 0
	for _, h := range hosts {
		if IsMultihoster(strings.TrimSpace(h)) {
			n++
		}
	}
	return n
}

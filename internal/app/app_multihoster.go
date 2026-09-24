package app

import "strings"

// multihosterDomains lists the JDownloader hosts that unlock other hosts rather
// than hosting files themselves. It is kept by hand because JD's API does not
// expose the distinction: accounts/getAccountInfo returns a null infoMap and
// listPremiumHoster is one flat list.
//
// These services are marked, not hidden, since for most of them JD is the only
// way in. The ones KnightLoader drives itself are taken out by
// debridServiceDomains (app_hosterauth.go) before the marking is read.
//
// Entries are bare lower-case hostnames as JD names them, without www. A name
// that disappears from JD costs nothing; a new one is unmarked until added.
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
	"simply-debrid.com": true,
	"zevera.com":        true,
}

// closedMultihosters are multihosters JD still lists that are out of service:
// the debridplanet.com domain is parked, simply-debrid.com has its API
// switched off, multivip.net does not answer, and the dailyleech.com pages JD
// logs in through are gone. The picker leaves them out, since an account there
// cannot fetch anything.
var closedMultihosters = map[string]bool{
	"dailyleech.com":    true,
	"debridplanet.com":  true,
	"multivip.net":      true,
	"simply-debrid.com": true,
}

// IsMultihoster reports whether a host is a service that unlocks other hosts.
// It compares through serviceKey, the normalisation the debrid filter uses, so
// "https://www.zevera.com/" and "zevera.com" are the same service.
func IsMultihoster(host string) bool {
	return multihosterDomains[serviceKey(host)]
}

// multihosterCount is how many of the given hosts are multihosters. The tests
// use it to notice a list that no longer matches anything JD reports.
func multihosterCount(hosts []string) int {
	n := 0
	for _, h := range hosts {
		if IsMultihoster(strings.TrimSpace(h)) {
			n++
		}
	}
	return n
}

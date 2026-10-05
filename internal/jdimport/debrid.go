package jdimport

import (
	"regexp"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
)

// debridKey says how to find a service's API key in a JDownloader account.
// Every JDownloader plugin with a key-only login keeps the key in the password
// field; key tells a key from a website password an older plugin version
// stored there instead.
type debridKey struct {
	key *regexp.Regexp
	// property is where the plugin keeps a key it fetched itself, tried when
	// the password is not a key.
	property string
}

// debridLogin marks a service whose client logs in with the account's own
// user name and password, which JDownloader stores as typed.
type debridLogin struct {
	// pair is the user and password shape of an API pair stored in the two
	// fields; apiUser and apiKey are the properties the plugin fills when the
	// fields hold a website login instead. Both are empty for a plain login.
	pair            *regexp.Regexp
	apiUser, apiKey string
}

// debridHosts maps the host JDownloader files an account under onto
// KnightLoader's service. Real-Debrid and Debrid-Link are listed though
// nothing can be taken over: JDownloader signs both in through OAuth and keeps
// only a token that expires, so the preview names the page the key is on.
var debridHosts = map[string]string{
	"torbox.app":        "torbox",
	"alldebrid.com":     "alldebrid",
	"real-debrid.com":   "realdebrid",
	"debrid-link.com":   "debridlink",
	"debrid-link.fr":    "debridlink",
	"premiumize.me":     "premiumize",
	"linksnappy.com":    "linksnappy",
	"offcloud.com":      "offcloud",
	"bestdebrid.com":    "bestdebrid",
	"cocoleech.com":     "cocoleech",
	"cooldebrid.com":    "cooldebrid",
	"debriditalia.com":  "debriditalia",
	"deepbrid.com":      "deepbrid",
	"fakirdebrid.net":   "fakirdebrid",
	"mega-debrid.eu":    "megadebrid",
	"multiup.io":        "multiup",
	"multiup.org":       "multiup",
	"mydebrid.com":      "mydebrid",
	"neodebrid.com":     "neodebrid",
	"proleech.link":     "proleech",
	"premium.rpnet.biz": "rpnet",
	"rpnet.biz":         "rpnet",
	"zevera.com":        "zevera",
}

var debridKeys = map[string]debridKey{
	"torbox":      {key: regexp.MustCompile(`^[a-f0-9]{32}$`)},
	"alldebrid":   {key: regexp.MustCompile(`^[a-zA-Z0-9]{20}$`), property: "apiv4_apikey"},
	"premiumize":  {key: regexp.MustCompile(`^[a-z0-9]{16}$`)},
	"zevera":      {key: regexp.MustCompile(`^[a-z0-9]{16}$`)},
	"offcloud":    {key: regexp.MustCompile(`^[a-zA-Z0-9]{16,32}$`)},
	"bestdebrid":  {key: regexp.MustCompile(`^\S{8,}$`)},
	"cocoleech":   {key: regexp.MustCompile(`^[a-f0-9]{24}$`)},
	"cooldebrid":  {key: regexp.MustCompile(`^[a-zA-Z0-9]{40}$`)},
	"deepbrid":    {key: regexp.MustCompile(`^[a-f0-9]{64}$`)},
	"fakirdebrid": {key: regexp.MustCompile(`^[a-f0-9]{20,}$`)},
}

var debridLogins = map[string]debridLogin{
	"linksnappy":   {},
	"debriditalia": {},
	"megadebrid":   {},
	"multiup":      {},
	"mydebrid":     {},
	"neodebrid":    {},
	"rpnet":        {},
	"proleech":     {pair: regexp.MustCompile(`^[a-z0-9]{21}$`), apiUser: "apiuser", apiKey: "apikey"},
}

// DebridService returns the catalogue id of the debrid service JDownloader
// files accounts under host for.
func DebridService(host string) (string, bool) {
	id, ok := debridHosts[strings.TrimPrefix(strings.ToLower(strings.TrimSpace(host)), "www.")]
	return id, ok
}

// DebridCredential builds the credential KnightLoader's client for service
// needs out of a JDownloader account. The second result is false when the
// account holds nothing that client can use.
func DebridCredential(service string, acc Account) (accounts.Credential, bool) {
	if k, ok := debridKeys[service]; ok {
		pw := strings.TrimSpace(acc.Password)
		if service == "torbox" {
			// The plugin stores the key without its dashes.
			pw = strings.ReplaceAll(pw, "-", "")
		}
		key := ""
		switch {
		case k.key.MatchString(pw):
			key = pw
		case k.property != "":
			if v, ok := acc.Properties[k.property].(string); ok {
				key = strings.TrimSpace(v)
			}
		}
		if key == "" {
			return accounts.Credential{}, false
		}
		if service == "torbox" && len(key) == 32 {
			key = key[:8] + "-" + key[8:12] + "-" + key[12:16] + "-" + key[16:20] + "-" + key[20:]
		}
		return accounts.Credential{APIKey: key}, true
	}
	l, ok := debridLogins[service]
	if !ok {
		return accounts.Credential{}, false
	}
	user, pw := strings.TrimSpace(acc.User), acc.Password
	if l.pair != nil && !l.pair.MatchString(pw) {
		u, _ := acc.Properties[l.apiUser].(string)
		k, _ := acc.Properties[l.apiKey].(string)
		user, pw = strings.TrimSpace(u), strings.TrimSpace(k)
	}
	if user == "" || pw == "" {
		return accounts.Credential{}, false
	}
	return accounts.Credential{Username: user, Password: pw}, true
}

// Package hostalias maps the alias domains of a file hoster onto the domain the
// hoster is known by, such as rg.to onto rapidgator.net.
//
// JDownloader's host list and the curated login list name each hoster once,
// by its main domain, while a link may use any domain the hoster answers
// under. Most debrid services list the aliases themselves, so this table only
// needs the hosters people link to under a second name. Each entry is taken
// from the domains JDownloader's own plugin for that hoster accepts.
package hostalias

import "strings"

var canonical = map[string]string{
	"rg.to":           "rapidgator.net",
	"rapidgator.asia": "rapidgator.net",

	"ul.to":       "uploaded.net",
	"uploaded.to": "uploaded.net",

	"k2s.cc":    "keep2share.cc",
	"keep2s.cc": "keep2share.cc",

	"ddl.to":         "ddownload.com",
	"mega.co.nz":     "mega.nz",
	"nitro.download": "nitroflare.com",
	"turb.to":        "turbobit.net",
	"hil.to":         "hitfile.net",

	"alterupload.com": "1fichier.com",
	"cjoint.net":      "1fichier.com",
	"desfichiers.com": "1fichier.com",
	"dfichiers.com":   "1fichier.com",
	"dl4free.com":     "1fichier.com",
	"megadl.fr":       "1fichier.com",
	"mesfichiers.org": "1fichier.com",
	"piecejointe.net": "1fichier.com",
	"pjointe.com":     "1fichier.com",
	"tenvoi.com":      "1fichier.com",
}

// Canonical returns the main domain of the hoster host belongs to, lower-cased
// and without a leading "www.". Other subdomains of an alias are left alone,
// as JDownloader's plugins do not accept them either. Any other host comes
// back normalised but otherwise unchanged.
func Canonical(host string) string {
	host = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(host)), "www.")
	if main, ok := canonical[host]; ok {
		return main
	}
	return host
}

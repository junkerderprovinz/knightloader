// Package hostericons holds the hoster icons that ship in the binary, each
// named <host>.png or <host>.svg: the ones the owner puts in overrides by
// hand, and the set scripts/hostericons fetches and writes. The same script
// writes the list of offline hosters.
package hostericons

import (
	"embed"
	"io/fs"
)

//go:embed all:overrides all:set
var files embed.FS

var (
	Overrides, _ = fs.Sub(files, "overrides")
	Set, _       = fs.Sub(files, "set")
)

// Offline lists the hosters whose site did not answer, one per line as
// the host, a tab and the reason.
//
//go:embed offline.txt
var Offline string

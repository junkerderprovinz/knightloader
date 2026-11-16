// Package hostericons holds the hoster icons that ship in the binary, each
// named <host>.png or <host>.svg: the ones the owner puts in overrides by
// hand, and the set scripts/hostericons fetches and writes.
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

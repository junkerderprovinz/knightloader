// Package web embeds the built SPA so KnightLoader ships as a single binary.
// The dist directory is produced by `npm run build` in this folder.
package web

import "embed"

//go:embed all:dist
var Dist embed.FS

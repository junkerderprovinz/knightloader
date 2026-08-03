// Package web embeds the built React/Carbon SPA so KnightLoader ships as a
// single self-contained binary. The dist/ directory is produced by `npm run
// build` in this folder; a placeholder ships until then.
package web

import "embed"

//go:embed all:dist
var Dist embed.FS

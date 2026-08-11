// Package extension embeds the Manifest V3 browser extension's source so the
// Go binary can serve it as a downloadable zip (internal/api/routes_browsertools.go)
// with no external hosting and no browser-store dependency — the same
// self-contained shape as package web for the SPA. src/ is also loadable
// straight from a git checkout via each browser's "load unpacked" developer
// mode, unzipped, which is why it is plain files rather than anything a build
// step produces.
package extension

import "embed"

//go:embed all:src
var Dist embed.FS

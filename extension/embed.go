// Package extension embeds the Manifest V3 browser extension so the server can
// offer it as a zip download (internal/api/routes_browsertools.go). src/ holds
// plain files with no build step, so it also loads unpacked straight from a
// checkout.
package extension

import "embed"

//go:embed all:src
var Dist embed.FS

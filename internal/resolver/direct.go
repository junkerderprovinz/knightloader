package resolver

import (
	"context"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// fileLike matches a URL path that ends in a plausible file extension. It
// accepts any extension because an allowlist would send unlisted ones (.md,
// .bin) to the media extractor, which answers "Unsupported URL".
var fileLike = regexp.MustCompile(`\.[a-z0-9]{1,8}$`)

// pageExt lists the suffixes that mean "web page", not "file". These stay with
// the media extractor, which is what actually handles a watch page.
var pageExt = map[string]bool{
	".html": true, ".htm": true, ".php": true, ".asp": true, ".aspx": true,
	".jsp": true, ".cgi": true, ".xhtml": true, ".shtml": true,
}

// Direct handles plain http(s) links whose path names a file; the URL is already
// the download target and is fetched by the embedded engine.
type Direct struct{}

func (Direct) Info() Info { return Info{ID: "direct", Prio: 40} }

func (Direct) Match(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false
	}
	base := strings.ToLower(path.Base(u.Path))
	if base == "" || base == "/" || base == "." {
		return false
	}
	ext := fileLike.FindString(base)
	return ext != "" && !pageExt[ext]
}

func (Direct) Resolve(_ context.Context, req Request) (Result, error) {
	name := "download"
	if u, err := url.Parse(req.URL); err == nil {
		if b := strings.TrimSpace(path.Base(u.Path)); b != "" && b != "/" && b != "." {
			name = b
		}
	}
	// Connections is left unset: the dispatcher reads it as a host ceiling,
	// and this resolver knows nothing about the host, so the user's setting
	// applies.
	return Result{Name: name, DirectURL: req.URL}, nil
}

// HTTPFallback takes any http(s) link that no other backend managed to fetch
// and hands it to the engine as is. It catches plain files whose URL has no
// extension, which Direct cannot recognise, and runs last by priority.
type HTTPFallback struct{}

func (HTTPFallback) Info() Info { return Info{ID: "http", Prio: -100} }

func (HTTPFallback) Match(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != ""
}

func (HTTPFallback) Resolve(_ context.Context, req Request) (Result, error) {
	return Result{DirectURL: req.URL}, nil
}

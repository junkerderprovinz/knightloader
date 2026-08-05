package resolver

import (
	"context"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// fileLike matches a URL path that ends in a plausible file extension. The rule
// is deliberately open — anything that looks like a file is a file — because an
// allowlist of known extensions silently sends unlisted ones (.md, .bin, .xyz)
// to the media extractor, which then reports "Unsupported URL".
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
	return Result{
		Name:        name,
		DirectURL:   req.URL,
		Connections: 4,
	}, nil
}

// HTTPFallback is the last resort: it takes any http(s) link that nothing else
// managed to fetch and simply asks the engine to download it. This is what
// catches a plain file whose URL carries no extension — a shape the strict
// Direct rule cannot recognise, and which a media extractor cannot handle
// either. It sits at the bottom of the priority list, so it only ever runs
// after every real backend has had its turn.
type HTTPFallback struct{}

func (HTTPFallback) Info() Info { return Info{ID: "http", Prio: -100} }

func (HTTPFallback) Match(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != ""
}

func (HTTPFallback) Resolve(_ context.Context, req Request) (Result, error) {
	return Result{DirectURL: req.URL, Connections: 4}, nil
}

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
type Direct struct {
	// Leave names hosts Direct does not claim, however much the path looks like
	// a file: a file hoster answers a plain GET with its landing page and a
	// video site with its player, and either would be saved as the file. It is
	// part of the claim rather than the priority, so no hand-arranged order can
	// put Direct in front of the backend such a link needs. Nil leaves nothing
	// out.
	Leave func(host string) bool
}

func (Direct) Info() Info { return Info{ID: "direct", Prio: 40} }

func (d Direct) Match(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false
	}
	if d.Leave != nil && d.Leave(u.Hostname()) {
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
type HTTPFallback struct {
	// Leave is Direct.Leave. It matters more here, since the fallback is what
	// a link reaches after every backend above it has declined.
	Leave func(host string) bool
}

func (HTTPFallback) Info() Info { return Info{ID: "http", Prio: -100} }

func (h HTTPFallback) Match(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return false
	}
	return h.Leave == nil || !h.Leave(u.Hostname())
}

func (HTTPFallback) Resolve(_ context.Context, req Request) (Result, error) {
	return Result{DirectURL: req.URL}, nil
}

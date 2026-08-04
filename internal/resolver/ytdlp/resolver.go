package ytdlp

import (
	"context"
	"net/url"

	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// Resolver routes non-file links to the yt-dlp backend. It matches any http(s)
// URL but sits below the Direct resolver (which claims plain file links), so it
// effectively picks up media/streaming pages.
type Resolver struct{}

func (Resolver) Info() resolver.Info { return resolver.Info{ID: "ytdlp", Prio: 10} }

func (Resolver) Match(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	// yt-dlp extracts and downloads; the real title/size arrive from its
	// progress stream (mirrored by the backend).
	return resolver.Result{DirectURL: req.URL, Name: req.URL}, nil
}

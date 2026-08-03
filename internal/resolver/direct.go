package resolver

import (
	"context"
	"net/url"
	"path"
	"strings"
)

// Direct handles plain http(s) links where the URL is already the download
// target. It is the lowest-priority catch-all for the http/https schemes.
type Direct struct{}

func (Direct) Info() Info { return Info{ID: "direct", Prio: 0} }

func (Direct) Match(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
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

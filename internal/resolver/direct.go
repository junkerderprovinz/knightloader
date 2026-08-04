package resolver

import (
	"context"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// fileExt matches URL paths that end in a downloadable file extension, so the
// Direct resolver claims plain file links and leaves media pages to yt-dlp.
var fileExt = regexp.MustCompile(`\.(zip|rar|r\d\d|7z|tar|gz|tgz|bz2|xz|iso|img|bin|dat|exe|msi|dmg|pkg|deb|rpm|apk|` +
	`mp4|mkv|avi|mov|webm|flv|wmv|m4v|mp3|m4a|flac|wav|ogg|opus|aac|` +
	`pdf|epub|mobi|cbz|cbr|jpg|jpeg|png|gif|webp|txt|srt|nfo)$`)

// Direct handles plain http(s) links whose path is a downloadable file; the URL
// is already the download target and is fetched by the embedded engine.
type Direct struct{}

func (Direct) Info() Info { return Info{ID: "direct", Prio: 20} }

func (Direct) Match(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false
	}
	return fileExt.MatchString(strings.ToLower(u.Path))
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

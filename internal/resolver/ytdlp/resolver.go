package ytdlp

import (
	"context"
	"net/url"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// Resolver routes media/streaming pages to yt-dlp. It matches any http(s) URL
// except hosts handled by a debrid/JD hoster backend (ExcludeHosts), so file
// hosters fall through to those instead of being mis-sent to yt-dlp.
type Resolver struct {
	ExcludeHosts map[string]bool
	// Leave names further file hosters, ones that are only known while the
	// app runs, such as JDownloader's host list. Nil leaves nothing more out.
	Leave func(host string) bool
}

func (Resolver) Info() resolver.Info { return resolver.Info{ID: "ytdlp", Prio: 30} }

func (r Resolver) Match(raw string) bool {
	host := hostname(raw)
	if host == "" || r.Leave != nil && r.Leave(host) {
		return false
	}
	return !hostInSet(host, r.ExcludeHosts)
}

// TakesPinned takes every web address: a link pinned to yt-dlp, such as the
// video the browser extension's button sends, was asked of yt-dlp by name,
// on a file hoster's host too.
func (Resolver) TakesPinned(raw string) bool {
	return hostname(raw) != ""
}

// hostname is the host of an http(s) address, and "" for anything else.
func hostname(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return u.Hostname()
}

func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	// yt-dlp extracts and downloads; the real title/size arrive from its
	// progress stream (mirrored by the backend).
	return resolver.Result{DirectURL: req.URL, Name: req.URL}, nil
}

// Resolver implements no resolver.Checker. "yt-dlp --simulate" is a full
// extraction per link, anti-bot checks included, with no batched form, so
// checking a large collector would get the address rate-limited for the real
// downloads. Backend.ProbeTitle runs once per staged link to fill in names,
// but it does not report availability.

// hostInSet reports whether host or any parent domain is in set.
func hostInSet(host string, set map[string]bool) bool {
	if len(set) == 0 {
		return false
	}
	host = strings.ToLower(strings.TrimPrefix(host, "www."))
	for host != "" {
		if set[host] {
			return true
		}
		i := strings.IndexByte(host, '.')
		if i < 0 {
			break
		}
		host = host[i+1:]
	}
	return false
}

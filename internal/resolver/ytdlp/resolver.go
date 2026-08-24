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
}

func (Resolver) Info() resolver.Info { return resolver.Info{ID: "ytdlp", Prio: 30} }

func (r Resolver) Match(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return false
	}
	return !hostInSet(u.Hostname(), r.ExcludeHosts)
}

func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	// yt-dlp extracts and downloads; the real title/size arrive from its
	// progress stream (mirrored by the backend).
	return resolver.Result{DirectURL: req.URL, Name: req.URL}, nil
}

// No resolver.Checker here, and unusually the reason is not money.
//
// "yt-dlp --simulate" costs the user nothing, but it is not a check: it is the
// whole extraction, one process per link, including whatever anti-bot gauntlet
// the site puts in front of it. yt-dlp has no batched form of that, so a
// fifty-link collector is fifty full extractions fired at a handful of sites -
// which is the rate-limiting this seam was made batched to avoid, arriving by
// the other door and getting the address blocked for the downloads that were
// actually asked for.
//
// A check worth wiring would have to be cheaper than the download it is meant to
// save. This one is the download, minus the bytes.
//
// A per-task ASYNC probe is a different shape, and it does exist: see
// Backend.ProbeTitle (backend.go) and app.probeYtdlpTitle, which the
// collector fires once per staged link as it is staged rather than once for
// a whole pasted batch at once. What makes that safe where a batched Checker
// here would not be is exactly the batching this comment is about - one
// process per link, never one process per link times however many were
// pasted together - so the per-link cost this comment already accepts is
// still paid once, not multiplied by a paste's size. It fills in the name
// this Resolve still cannot promise; it does not check whether the link is
// still there, which stays exactly the gap described above.

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

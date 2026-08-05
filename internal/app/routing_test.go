package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torbox"
	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
)

// TestRouting pins the M3b routing decision: files -> engine (direct), supported
// hosters -> TorBox, everything else -> yt-dlp, with JD as the backup when no
// TorBox key is present.
func TestRouting(t *testing.T) {
	hosts := map[string]bool{"rapidgator.net": true}

	full := resolver.NewRegistry()
	full.Register(resolver.Direct{})
	full.Register(ytdlp.Resolver{ExcludeHosts: hosts})
	full.Register(torbox.Resolver{Hosts: hosts})
	full.Register(jd.Resolver{})

	cases := []struct{ url, want string }{
		{"https://example.com/movie.mp4", "direct"},
		{"https://example.com/archive.zip", "direct"},
		{"https://rapidgator.net/file/abc123", "torbox"},
		{"https://www5.rapidgator.net/file/abc123", "torbox"}, // subdomain walks to parent
		{"https://youtube.com/watch?v=x", "ytdlp"},
		{"https://soundcloud.com/a/b", "ytdlp"},
	}
	for _, c := range cases {
		got := full.For(c.url)
		if got == nil {
			t.Errorf("%s: no resolver matched", c.url)
			continue
		}
		if got.Info().ID != c.want {
			t.Errorf("%s -> %s, want %s", c.url, got.Info().ID, c.want)
		}
	}

	// No TorBox key: the hoster resolver isn't registered, yt-dlp still excludes
	// the hoster host, so the link falls through to JD (the backup).
	noKey := resolver.NewRegistry()
	noKey.Register(resolver.Direct{})
	noKey.Register(ytdlp.Resolver{ExcludeHosts: hosts})
	noKey.Register(jd.Resolver{})
	got := noKey.For("https://rapidgator.net/file/abc")
	if got == nil || got.Info().ID != "jd" {
		id := "nil"
		if got != nil {
			id = got.Info().ID
		}
		t.Errorf("hoster link without TorBox -> %s, want jd", id)
	}
}

package app

// Which links the backends that take nearly anything may claim. The direct
// download and the HTTP fallback fetch a URL as it stands, which on a file
// hoster saves the landing page and on a video site the player, reported as a
// finished download. yt-dlp tries its generic extractor on a hoster's page.
// Keeping them off those hosts is part of what they claim rather than of the
// priority order, so every row of the priority card can be moved and no order
// sends such a link down the wrong path.

import (
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/hosterauth"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
)

// hostClaims holds what rewireBackends learned about hosts. The resolvers ask
// it from Match, under the registry's lock and sometimes the app's, so it
// takes no lock but its own.
type hostClaims struct {
	mu sync.RWMutex
	// hosters are the file hosters a debrid service or TorBox lists, matched
	// with their subdomains as those services match them.
	hosters map[string]bool
	// media are the video sites yt-dlp serves. They are left to it, so the set
	// is empty while yt-dlp is not running.
	media map[string]bool
}

func (c *hostClaims) set(hosters, media map[string]bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hosters, c.media = hosters, media
}

// fileHoster reports whether host is a file hoster: one JD fetches ahead of a
// plain GET (jd.FileHoster), one on the curated list, or one a debrid service
// or TorBox lists. These are the hosts the automatic order already gives to a
// hoster backend before the direct download.
func (c *hostClaims) fileHoster(host string) bool {
	if jd.FileHoster(host) || hosterauth.Curated(host) {
		return true
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return debrid.HostInSet(host, c.hosters)
}

// pageOnly reports whether a plain GET on host fetches a page instead of the
// file: a file hoster, or a video site while yt-dlp runs.
func (c *hostClaims) pageOnly(host string) bool {
	if c.fileHoster(host) {
		return true
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return debrid.HostInSet(host, c.media)
}

package settings

import (
	"net/url"
	"slices"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/reconnect"
)

// CrossSeed has a finished torrent seeded on more trackers from the same
// files: RED and OPS ask each other's API for the twin of a torrent from the
// other site, and any torrent is searched for by name on the Torznab
// indexers. With a daemon set, the user's own cross-seed daemon does the
// searching instead.
type CrossSeed struct {
	Enabled bool        `json:"enabled"`
	RED     GazelleSite `json:"red"`
	OPS     GazelleSite `json:"ops"`
	// Indexers are Prowlarr or Jackett Torznab feeds. No omitempty, see
	// CrawlInclude.
	Indexers []TorznabIndexer `json:"indexers"`
	// LinkMode is how a cross-seed reaches the files: LinkNone, or empty,
	// seeds from where they are, the others link them into LinkDir first.
	LinkMode string `json:"linkMode"`
	LinkDir  string `json:"linkDir"`
	// Category files every cross-seed in qBittorrent; empty files it nowhere.
	Category string          `json:"category"`
	Daemon   CrossSeedDaemon `json:"daemon"`
}

// The link modes, as crossseed.LinkMode names them.
const (
	LinkNone     = "none"
	LinkHardlink = "hardlink"
	LinkSymlink  = "symlink"
	LinkReflink  = "reflink"
)

// GazelleSite is the user's account on RED or OPS: the API key, and the
// announce address with the passkey that a torrent made for the site
// announces to. Both are secrets.
type GazelleSite struct {
	APIKey   string `json:"apiKey"`
	Announce string `json:"announce"`
}

// Ready reports whether the site can be asked and a match announced.
func (g GazelleSite) Ready() bool { return g.APIKey != "" && g.Announce != "" }

// TorznabIndexer is one Torznab feed and its API key.
type TorznabIndexer struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	APIKey string `json:"apiKey"`
}

// CrossSeedDaemon is the user's cross-seed daemon, which takes each finished
// torrent through its webhook. Folder, when set, is a folder the daemon can
// reach too, where KnightLoader leaves the torrents and the daemon leaves
// the twins it finds.
type CrossSeedDaemon struct {
	URL    string `json:"url"`
	APIKey string `json:"apiKey"`
	Folder string `json:"folder"`
}

func mask(v string) string {
	if v == "" {
		return v
	}
	return reconnect.RedactedPassword
}

// Redacted masks every key and both announce addresses.
func (c CrossSeed) Redacted() CrossSeed {
	for _, g := range []*GazelleSite{&c.RED, &c.OPS} {
		g.APIKey, g.Announce = mask(g.APIKey), mask(g.Announce)
	}
	if len(c.Indexers) > 0 {
		out := make([]TorznabIndexer, len(c.Indexers))
		for i, x := range c.Indexers {
			x.APIKey = mask(x.APIKey)
			out[i] = x
		}
		c.Indexers = out
	}
	c.Daemon.APIKey = mask(c.Daemon.APIKey)
	return c
}

// WithSecretsFrom puts back what Redacted masked. A site's key belongs to the
// site, whose address is fixed; an indexer's key and the daemon's come back
// only to the address they were stored for, so a client that was never shown
// them cannot have them sent to another host.
func (c CrossSeed) WithSecretsFrom(prev CrossSeed) CrossSeed {
	keep := func(v *string, was string) {
		if *v == reconnect.RedactedPassword {
			*v = was
		}
	}
	keep(&c.RED.APIKey, prev.RED.APIKey)
	keep(&c.RED.Announce, prev.RED.Announce)
	keep(&c.OPS.APIKey, prev.OPS.APIKey)
	keep(&c.OPS.Announce, prev.OPS.Announce)
	if len(c.Indexers) > 0 {
		out := make([]TorznabIndexer, len(c.Indexers))
		for i, x := range c.Indexers {
			if x.APIKey == reconnect.RedactedPassword {
				x.APIKey = ""
				for _, p := range prev.Indexers {
					if strings.TrimSpace(p.URL) == strings.TrimSpace(x.URL) && p.APIKey != "" {
						x.APIKey = p.APIKey
						break
					}
				}
			}
			out[i] = x
		}
		c.Indexers = out
	}
	if c.Daemon.APIKey == reconnect.RedactedPassword {
		c.Daemon.APIKey = ""
		if strings.TrimSpace(c.Daemon.URL) == strings.TrimSpace(prev.Daemon.URL) {
			c.Daemon.APIKey = prev.Daemon.APIKey
		}
	}
	return c
}

// sanitized trims every field, drops blank indexer rows, names an indexer
// without a name after its host, and seeds in place under a link mode this
// build does not know.
func (c CrossSeed) sanitized() CrossSeed {
	for _, g := range []*GazelleSite{&c.RED, &c.OPS} {
		g.APIKey, g.Announce = strings.TrimSpace(g.APIKey), strings.TrimSpace(g.Announce)
	}
	out := c.Indexers[:0:0]
	for _, x := range c.Indexers {
		x.Name, x.URL, x.APIKey = strings.TrimSpace(x.Name), strings.TrimSpace(x.URL), strings.TrimSpace(x.APIKey)
		if x.URL == "" && x.Name == "" && x.APIKey == "" {
			continue
		}
		if x.Name == "" {
			if u, err := url.Parse(x.URL); err == nil {
				x.Name = u.Hostname()
			}
		}
		out = append(out, x)
	}
	c.Indexers = out
	switch c.LinkMode {
	case "", LinkNone, LinkHardlink, LinkSymlink, LinkReflink:
	default:
		c.LinkMode = LinkNone
	}
	c.LinkDir = strings.TrimSpace(c.LinkDir)
	c.Category = strings.TrimSpace(c.Category)
	c.Daemon.URL, c.Daemon.APIKey, c.Daemon.Folder = strings.TrimSpace(c.Daemon.URL), strings.TrimSpace(c.Daemon.APIKey), strings.TrimSpace(c.Daemon.Folder)
	return c
}

// mapSecrets hands apply the keys, the announce addresses and the indexer
// and daemon addresses, which may carry a key in their query.
func (c *CrossSeed) mapSecrets(apply func(*string)) {
	for _, g := range []*GazelleSite{&c.RED, &c.OPS} {
		apply(&g.APIKey)
		apply(&g.Announce)
	}
	c.Indexers = slices.Clone(c.Indexers)
	for i := range c.Indexers {
		apply(&c.Indexers[i].URL)
		apply(&c.Indexers[i].APIKey)
	}
	apply(&c.Daemon.URL)
	apply(&c.Daemon.APIKey)
}

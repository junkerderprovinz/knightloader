package settings

import (
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
)

// settings_resolvers.go: per-resolver configuration that a resolver backend
// reads but had no field on Settings to read it FROM - see
// docs/jd-feature-census.md's "(per-plugin option list)" and "Variante"
// rows. yt-dlp is the only resolver with anything real to configure here:
// Direct and HTTPFallback (internal/resolver) take no options at all, the
// debrid and TorBox backends are pure credential+API clients (internal/
// accounts owns those, not this file), and the headless-JD backend
// delegates entirely to JD's own settings. A resolver that later grows a
// real, per-instance knob gets a field here the same way Ytdlp did, rather
// than a second settings page for one field.

func sanitizeResolvers(n Settings) Settings {
	n.Ytdlp = n.Ytdlp.Sanitize()
	presets := make(map[string]ytdlp.HosterPreset, len(n.YtdlpPresets))
	for host, p := range n.YtdlpPresets {
		host = strings.TrimSpace(strings.ToLower(host))
		if host == "" {
			continue
		}
		presets[host] = p.Sanitize()
	}
	n.YtdlpPresets = presets
	return n
}

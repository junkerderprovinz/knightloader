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
	n.ResolverOrder = cleanResolverOrder(n.ResolverOrder)
	return n
}

// cleanResolverOrder keeps ResolverOrder a well-formed sequence: trimmed,
// lower-cased, no blank, no repeat, and nil rather than an empty slice so an
// empty order reads the same on disk however it got there.
//
// A repeat is the one thing that genuinely breaks the order rather than merely
// looking untidy: dispatch walks it as "try these in turn", and a duplicate id
// would hand the same resolver two different ranks, so which one a stable sort
// used would depend on where the duplicate sat. Unknown ids are left alone on
// purpose - see the field's own comment in settings.go for why this package
// has no business deciding which resolver ids exist.
func cleanResolverOrder(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, id := range in {
		id = strings.TrimSpace(strings.ToLower(id))
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

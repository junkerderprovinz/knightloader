package settings

import (
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
)

// Per-resolver configuration. yt-dlp is the only resolver with anything to
// configure here: Direct and HTTPFallback take no options, the debrid and
// TorBox backends are credential and API clients that internal/accounts owns,
// and the headless-JD backend delegates to JD's own settings. A resolver that
// grows a per-instance knob gets a field here the way Ytdlp did, rather than a
// second settings page for one field.

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
	n.ResolverOrder = cleanIDList(n.ResolverOrder)
	return n
}

// cleanIDList keeps a list of ids such as ResolverOrder or ModulesOff
// well-formed: trimmed, lower-cased, no blank, no repeat, and nil rather than
// an empty slice so an empty list reads the same on disk however it got there.
//
// A repeat is what breaks the order rather than merely looking untidy:
// dispatch walks it as "try these in turn", and a duplicate id hands the same
// resolver two ranks, so which one a stable sort used would depend on where the
// duplicate sat. Unknown ids are left alone, see ResolverOrder in settings.go
// for why this package does not decide which resolver ids exist.
func cleanIDList(in []string) []string {
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

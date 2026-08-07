package settings

// What the interface looks like: the one shape knob, the accent, and the
// rainbow palette that stands in for it.

import (
	"regexp"
	"strings"
)

// The three shapes the interface offers. Anything else falls back to round
// rather than producing an interface with no radius rule at all.
const (
	ShapeRound  = "round"
	ShapeSoft   = "soft"
	ShapeSquare = "square"
)

// accentPattern is a plain six-digit hex colour. Accepting anything else would
// put attacker-chosen text straight into a CSS custom property.
var accentPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// RainbowSize is how many hues the palette has. It is fixed: the colours are
// handed out by position, so a palette that can change length would silently
// re-colour every existing row whenever the user added one.
const RainbowSize = 8

// sanitizePalette accepts a custom palette only in full. A palette with one
// unusable entry is not a palette with seven good colours, it is a palette that
// turns one row invisible, so the whole override is dropped back to the
// built-in hues.
func sanitizePalette(p []string) []string {
	if len(p) != RainbowSize {
		return nil
	}
	out := make([]string, 0, RainbowSize)
	for _, c := range p {
		c = strings.TrimSpace(c)
		if !accentPattern.MatchString(c) {
			return nil
		}
		out = append(out, c)
	}
	return out
}

func sanitizeAppearance(n Settings) Settings {
	switch n.Shape {
	case ShapeRound, ShapeSoft, ShapeSquare:
	default:
		n.Shape = ShapeRound
	}
	n.Accent = strings.TrimSpace(n.Accent)
	if n.Accent != "" && !accentPattern.MatchString(n.Accent) {
		n.Accent = ""
	}
	n.RainbowPalette = sanitizePalette(n.RainbowPalette)
	// The seed is only ever read modulo the palette length, so it is folded here
	// and stored small enough to read at a glance in settings.json.
	if n.RainbowSeed < 0 {
		n.RainbowSeed = -n.RainbowSeed
	}
	n.RainbowSeed %= RainbowSize
	return n
}

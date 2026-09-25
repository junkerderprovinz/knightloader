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

// How much of a navigation entry is drawn, in the sidebar and in the settings
// rail, the app's two sets of tabs.
//
// NavLabelsHover is not the collapsing rail it sounds like: nothing resizes.
// The tile and the sidebar row keep the size they have in NavLabelsBoth; at
// rest the glyph sits centred in that space, and on hover it moves aside, up in
// a settings tile and left in a sidebar row, with the label appearing in the
// room it leaves. A rail that grows or overlays on hover moves the page under
// the pointer, and this one cannot.
const (
	NavLabelsBoth  = "both"
	NavLabelsGlyph = "glyph"
	NavLabelsText  = "text"
	NavLabelsHover = "hover"
)

// BottomBarFollowsNav is the bottom bar's default: it draws its entries the
// way NavLabels draws the sidebar's, so nothing moves for somebody who never
// opens the setting. The bar gets a setting of its own because it is always on
// screen and puts all its words side by side at phone width (GlimStone, "The
// bottom bar").
const BottomBarFollowsNav = "follow"

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
	switch n.NavLabels {
	case NavLabelsBoth, NavLabelsGlyph, NavLabelsText, NavLabelsHover:
	default:
		// Including the empty string, which is what every settings.json
		// written before this field existed carries. Both is the behaviour
		// those files already had, so an upgrade changes nothing on screen.
		n.NavLabels = NavLabelsBoth
	}
	switch n.BottomBarLabels {
	case BottomBarFollowsNav, NavLabelsBoth, NavLabelsGlyph, NavLabelsText, NavLabelsHover:
	default:
		// Including the empty string of an older settings.json, since
		// following the sidebar is what the bar did before the field existed.
		n.BottomBarLabels = BottomBarFollowsNav
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

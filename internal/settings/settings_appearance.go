package settings

// What the interface looks like: the one shape knob, the accent, and the
// rainbow palette that stands in for it.

import (
	"encoding/json"
	"regexp"
	"strings"
)

// The shapes the interface draws. The picker offers the first three; leaf is
// found by a gesture on it and has to survive a save like any other choice.
// Anything else falls back to DefaultShape rather than producing an interface
// with no radius rule at all.
const (
	ShapeRound  = "round"
	ShapeSoft   = "soft"
	ShapeSquare = "square"
	ShapeLeaf   = "leaf"
)

// DefaultShape is what an install with no stored shape draws. A stored choice,
// round included, keeps its shape.
const DefaultShape = ShapeSoft

// How much of a control is drawn, the value of each of the four label
// settings.
//
// LabelsHover is not the collapsing rail it sounds like: nothing resizes. The
// tile and the sidebar row keep the size they have in LabelsBoth; at rest the
// glyph sits centred in that space, and on hover it moves aside, up in a
// settings tile and left in a sidebar row, with the label appearing in the room
// it leaves. A rail that grows or overlays on hover moves the page under the
// pointer, and this one cannot.
const (
	LabelsBoth  = "both"
	LabelsGlyph = "glyph"
	LabelsText  = "text"
	LabelsHover = "hover"
)

func isLabelMode(m string) bool {
	switch m {
	case LabelsBoth, LabelsGlyph, LabelsText, LabelsHover:
		return true
	}
	return false
}

// labelMode keeps a known mode and turns anything else into LabelsBoth, the
// empty string of a settings.json older than the field included, since both is
// what every control drew before it had a setting.
func labelMode(m string) string {
	if isLabelMode(m) {
		return m
	}
	return LabelsBoth
}

// migrateLabels splits the one navLabels setting of an earlier build into the
// four label settings. Each of them that the file does not carry yet takes its
// value, since that is how those controls were drawn, and so does a bottom bar
// set to "follow", which drew the bar the way navLabels drew the sidebar.
func migrateLabels(raw []byte, n Settings) Settings {
	var old struct {
		NavLabels       string  `json:"navLabels"`
		ButtonLabels    *string `json:"buttonLabels"`
		SidebarLabels   *string `json:"sidebarLabels"`
		TabLabels       *string `json:"tabLabels"`
		BottomBarLabels *string `json:"bottomBarLabels"`
	}
	if err := json.Unmarshal(raw, &old); err != nil || !isLabelMode(old.NavLabels) {
		return n
	}
	unset := func(v *string) bool { return v == nil || *v == "" }
	if unset(old.ButtonLabels) {
		n.ButtonLabels = old.NavLabels
	}
	if unset(old.SidebarLabels) {
		n.SidebarLabels = old.NavLabels
	}
	if unset(old.TabLabels) {
		n.TabLabels = old.NavLabels
	}
	if unset(old.BottomBarLabels) || *old.BottomBarLabels == "follow" {
		n.BottomBarLabels = old.NavLabels
	}
	return n
}

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
	case ShapeRound, ShapeSoft, ShapeSquare, ShapeLeaf:
	default:
		n.Shape = DefaultShape
	}
	n.ButtonLabels = labelMode(n.ButtonLabels)
	n.SidebarLabels = labelMode(n.SidebarLabels)
	n.TabLabels = labelMode(n.TabLabels)
	n.BottomBarLabels = labelMode(n.BottomBarLabels)
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

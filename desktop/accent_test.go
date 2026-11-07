package main

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestWindowsKeepsTheAccentBackwards(t *testing.T) {
	// The default blue, #0078D7, with its alpha in the top byte.
	if got := fromABGR(0xFFD77800); got != "#0078D7" {
		t.Errorf("fromABGR = %q, want #0078D7", got)
	}
	if got := fromABGR(0x00102030); got != "#302010" {
		t.Errorf("an accent stored without alpha reads as %q", got)
	}
}

func TestMacNamesApplesAccentColours(t *testing.T) {
	cases := []struct {
		value string
		set   bool
		want  string
	}{
		{"4\n", true, "#007AFF"},
		{"-1\n", true, "#8C8C8C"},
		{"0", true, "#E0383E"},
		{"1", true, "#F7821B"},
		{"2", true, "#FFC726"},
		{"3", true, "#62BA46"},
		{"5", true, "#953D96"},
		{"6", true, "#F74F9E"},
		// Multicolour, the default, writes no key and shows blue.
		{"", false, "#007AFF"},
		{"7", true, ""},
		{"purple", true, ""},
	}
	for _, c := range cases {
		if got := appleAccent(c.value, c.set); got != c.want {
			t.Errorf("appleAccent(%q, %v) = %q, want %q", c.value, c.set, got, c.want)
		}
	}
}

func TestThePortalAnswersInDoublesInsideVariants(t *testing.T) {
	// Read wraps the value in a second variant, ReadOne does not.
	rgb := []any{0.0, 0.47, 0.84}
	for _, v := range []any{dbus.MakeVariant(dbus.MakeVariant(rgb)), dbus.MakeVariant(rgb), rgb} {
		if got := portalAccent(v); got != "#0078D6" {
			t.Errorf("portalAccent(%v) = %q, want #0078D6", v, got)
		}
	}
	if got := portalAccent([]float64{1, 1, 1}); got != "#FFFFFF" {
		t.Errorf("white reads as %q", got)
	}
}

func TestThePortalCanSayItHasNoAccent(t *testing.T) {
	for _, v := range []any{
		// Outside 0 to 1 means the desktop has no accent of its own.
		dbus.MakeVariant(dbus.MakeVariant([]any{-1.0, -1.0, -1.0})),
		[]any{0.5, 1.2, 0.5},
		[]any{0.5, 0.5},
		[]any{"red", 0.5, 0.5},
		"#0078D7",
		nil,
	} {
		if got := portalAccent(v); got != "" {
			t.Errorf("portalAccent(%v) = %q, want none", v, got)
		}
	}
}

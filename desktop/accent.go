package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"
)

// Accent hands the page the accent colour the desktop is set to, the source
// of the wand swatch beside the accent presets. The page calls Read as
// main.Accent.Read whenever the window comes back to the front, since the
// colour changes in the system's settings while the program runs.
type Accent struct{}

// Read answers "#RRGGBB", or "" where the system names no colour of its own.
func (Accent) Read() string { return readAccent() }

func rgbHex(r, g, b uint8) string { return fmt.Sprintf("#%02X%02X%02X", r, g, b) }

// fromABGR turns the DWORD Windows keeps under DWM\AccentColor, red in the
// lowest byte and alpha in the highest.
func fromABGR(v uint32) string {
	return rgbHex(uint8(v), uint8(v>>8), uint8(v>>16))
}

// appleColours are the accents of macOS's General settings by the number
// `defaults read -g AppleAccentColor` prints, as NSColor.controlAccentColor
// draws them.
var appleColours = map[int]string{
	-1: "#8C8C8C", // graphite
	0:  "#E0383E", // red
	1:  "#F7821B", // orange
	2:  "#FFC726", // yellow
	3:  "#62BA46", // green
	4:  "#007AFF", // blue
	5:  "#953D96", // purple
	6:  "#F74F9E", // pink
}

// appleAccent maps what `defaults` printed. set is false when the key is
// missing, which is how macOS stores multicolour, shown in blue.
func appleAccent(value string, set bool) string {
	if !set {
		return appleColours[4]
	}
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return appleColours[n]
}

// portalAccent reads org.freedesktop.appearance accent-color as the settings
// portal hands it out: red, green and blue as doubles from 0 to 1, inside one
// variant from ReadOne and two from Read. A channel outside that range is how
// the portal says the desktop has no accent.
func portalAccent(v any) string {
	for {
		inner, ok := v.(dbus.Variant)
		if !ok {
			break
		}
		v = inner.Value()
	}
	var rgb []float64
	switch c := v.(type) {
	case []float64:
		rgb = c
	case []any:
		for _, x := range c {
			f, ok := x.(float64)
			if !ok {
				return ""
			}
			rgb = append(rgb, f)
		}
	default:
		return ""
	}
	if len(rgb) != 3 {
		return ""
	}
	var ch [3]uint8
	for i, f := range rgb {
		if f < 0 || f > 1 {
			return ""
		}
		ch[i] = uint8(math.Round(f * 255))
	}
	return rgbHex(ch[0], ch[1], ch[2])
}

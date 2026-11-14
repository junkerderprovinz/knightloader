package signinbrowser

import "unicode/utf8"

// Modifier bits as Input.dispatch*Event takes them.
const (
	modAlt   = 1
	modCtrl  = 2
	modMeta  = 4
	modShift = 8
)

// maxText is the longest text one input may insert, which a password
// manager's paste fits in many times over.
const maxText = 4096

// Input is one thing the user did over the page in the web UI: a pointer
// event, a key, text an input method or a paste composed, back, reload, or a
// new size of the panel. Coordinates are CSS pixels of the page. The page may
// send nothing else, so no part of the DevTools protocol is open to it.
type Input struct {
	Type    string  `json:"type"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Button  int     `json:"button"`
	Buttons int     `json:"buttons"`
	Clicks  int     `json:"clicks"`
	DeltaX  float64 `json:"dx"`
	DeltaY  float64 `json:"dy"`
	Key     string  `json:"key"`
	Code    string  `json:"code"`
	KeyCode int     `json:"keyCode"`
	Mods    int     `json:"mods"`
	Text    string  `json:"text"`
	Width   int     `json:"width"`
	Height  int     `json:"height"`
	Scale   float64 `json:"scale"`
}

// viewport is the page's size in CSS pixels and the pixel ratio of the
// screen it is shown on.
type viewport struct {
	Width  int     `json:"width"`
	Height int     `json:"height"`
	Scale  float64 `json:"scale"`
}

type mouseEvent struct {
	Type       string   `json:"type"`
	X          float64  `json:"x"`
	Y          float64  `json:"y"`
	Button     string   `json:"button"`
	Buttons    int      `json:"buttons"`
	ClickCount int      `json:"clickCount"`
	Modifiers  int      `json:"modifiers"`
	DeltaX     *float64 `json:"deltaX,omitempty"`
	DeltaY     *float64 `json:"deltaY,omitempty"`
}

type keyEvent struct {
	Type           string `json:"type"`
	Key            string `json:"key"`
	Code           string `json:"code"`
	KeyCode        int    `json:"windowsVirtualKeyCode"`
	NativeKeyCode  int    `json:"nativeVirtualKeyCode"`
	Modifiers      int    `json:"modifiers"`
	Text           string `json:"text,omitempty"`
	UnmodifiedText string `json:"unmodifiedText,omitempty"`
}

// buttonNames are DOM MouseEvent.button values in Chromium's words.
var buttonNames = map[int]string{0: "left", 1: "middle", 2: "right", 3: "back", 4: "forward"}

// command is the DevTools call that replays in, or false for input the page
// has no business sending.
func (in Input) command(vp viewport) (string, any, bool) {
	mods := in.Mods & (modAlt | modCtrl | modMeta | modShift)
	switch in.Type {
	case "mousedown", "mouseup", "mousemove", "wheel":
		ev := mouseEvent{
			X:         min(max(in.X, 0), float64(vp.Width)),
			Y:         min(max(in.Y, 0), float64(vp.Height)),
			Button:    "none",
			Buttons:   in.Buttons & 31,
			Modifiers: mods,
		}
		switch in.Type {
		case "mousedown", "mouseup":
			name, ok := buttonNames[in.Button]
			if !ok {
				return "", nil, false
			}
			ev.Type, ev.Button, ev.ClickCount = "mousePressed", name, min(max(in.Clicks, 1), 3)
			if in.Type == "mouseup" {
				ev.Type = "mouseReleased"
			}
		case "mousemove":
			// A drag selecting text is a move with the button held.
			ev.Type = "mouseMoved"
			switch {
			case ev.Buttons&1 != 0:
				ev.Button = "left"
			case ev.Buttons&2 != 0:
				ev.Button = "right"
			case ev.Buttons&4 != 0:
				ev.Button = "middle"
			}
		case "wheel":
			ev.Type, ev.DeltaX, ev.DeltaY = "mouseWheel", &in.DeltaX, &in.DeltaY
		}
		return "Input.dispatchMouseEvent", ev, true

	case "keydown", "keyup":
		if in.Key == "" || len(in.Key) > 24 || len(in.Code) > 32 || in.KeyCode < 0 || in.KeyCode > 255 {
			return "", nil, false
		}
		ev := keyEvent{Type: "keyUp", Key: in.Key, Code: in.Code, KeyCode: in.KeyCode, NativeKeyCode: in.KeyCode, Modifiers: mods}
		if in.Type == "keyup" {
			return "Input.dispatchKeyEvent", ev, true
		}
		ev.Type = "rawKeyDown"
		text := ""
		switch {
		case in.Key == "Enter":
			text = "\r"
		case utf8.RuneCountInString(in.Key) == 1 && mods&modMeta == 0 && (mods&modCtrl == 0 || mods&modAlt != 0):
			// AltGr reaches a browser as Ctrl and Alt together; what it types
			// is a character, and the modifiers would make it a shortcut.
			text = in.Key
			ev.Modifiers &^= modCtrl | modAlt
		}
		if text != "" {
			ev.Type, ev.Text, ev.UnmodifiedText = "keyDown", text, text
		}
		return "Input.dispatchKeyEvent", ev, true

	case "text":
		if in.Text == "" || len(in.Text) > maxText || !utf8.ValidString(in.Text) {
			return "", nil, false
		}
		return "Input.insertText", struct {
			Text string `json:"text"`
		}{in.Text}, true
	}
	return "", nil, false
}

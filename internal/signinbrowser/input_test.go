package signinbrowser

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func params(t *testing.T, raw string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestThePagesPointerIsMappedOntoChromiumsMouseEvents(t *testing.T) {
	vp := viewport{Width: 800, Height: 600}
	for _, c := range []struct {
		in   Input
		want string
	}{
		{Input{Type: "mousedown", X: 10, Y: 20, Button: 0, Buttons: 1, Clicks: 1, Mods: modShift},
			`{"type":"mousePressed","x":10,"y":20,"button":"left","buttons":1,"clickCount":1,"modifiers":8}`},
		{Input{Type: "mouseup", X: 10, Y: 20, Button: 2, Clicks: 1},
			`{"type":"mouseReleased","x":10,"y":20,"button":"right","buttons":0,"clickCount":1,"modifiers":0}`},
		{Input{Type: "mousemove", X: 900, Y: -5, Button: -1},
			`{"type":"mouseMoved","x":800,"y":0,"button":"none","buttons":0,"clickCount":0,"modifiers":0}`},
		{Input{Type: "wheel", X: 50, Y: 60, DeltaX: 0, DeltaY: 120},
			`{"type":"mouseWheel","x":50,"y":60,"button":"none","buttons":0,"clickCount":0,"modifiers":0,"deltaX":0,"deltaY":120}`},
	} {
		method, got, ok := c.in.command(vp)
		if !ok || method != "Input.dispatchMouseEvent" {
			t.Errorf("%+v went to %q (ok %v), want Input.dispatchMouseEvent", c.in, method, ok)
			continue
		}
		raw, _ := json.Marshal(got)
		if !reflect.DeepEqual(params(t, string(raw)), params(t, c.want)) {
			t.Errorf("%+v became %s, want %s", c.in, raw, c.want)
		}
	}
}

func TestTypingSendsTheKeyAndTheCharacterItTypes(t *testing.T) {
	vp := viewport{Width: 800, Height: 600}
	for _, c := range []struct {
		name string
		in   Input
		want string
	}{
		{"a letter", Input{Type: "keydown", Key: "a", Code: "KeyA", KeyCode: 65},
			`{"type":"keyDown","key":"a","code":"KeyA","windowsVirtualKeyCode":65,"nativeVirtualKeyCode":65,"modifiers":0,"text":"a","unmodifiedText":"a"}`},
		{"an umlaut", Input{Type: "keydown", Key: "ä", Code: "Quote", KeyCode: 222},
			`{"type":"keyDown","key":"ä","code":"Quote","windowsVirtualKeyCode":222,"nativeVirtualKeyCode":222,"modifiers":0,"text":"ä","unmodifiedText":"ä"}`},
		{"a capital", Input{Type: "keydown", Key: "A", Code: "KeyA", KeyCode: 65, Mods: modShift},
			`{"type":"keyDown","key":"A","code":"KeyA","windowsVirtualKeyCode":65,"nativeVirtualKeyCode":65,"modifiers":8,"text":"A","unmodifiedText":"A"}`},
		{"Enter", Input{Type: "keydown", Key: "Enter", Code: "Enter", KeyCode: 13},
			`{"type":"keyDown","key":"Enter","code":"Enter","windowsVirtualKeyCode":13,"nativeVirtualKeyCode":13,"modifiers":0,"text":"\r","unmodifiedText":"\r"}`},
		{"Tab", Input{Type: "keydown", Key: "Tab", Code: "Tab", KeyCode: 9},
			`{"type":"rawKeyDown","key":"Tab","code":"Tab","windowsVirtualKeyCode":9,"nativeVirtualKeyCode":9,"modifiers":0}`},
		{"Backspace", Input{Type: "keydown", Key: "Backspace", Code: "Backspace", KeyCode: 8},
			`{"type":"rawKeyDown","key":"Backspace","code":"Backspace","windowsVirtualKeyCode":8,"nativeVirtualKeyCode":8,"modifiers":0}`},
		{"a shortcut", Input{Type: "keydown", Key: "a", Code: "KeyA", KeyCode: 65, Mods: modCtrl},
			`{"type":"rawKeyDown","key":"a","code":"KeyA","windowsVirtualKeyCode":65,"nativeVirtualKeyCode":65,"modifiers":2}`},
		// AltGr arrives as Ctrl and Alt together, and what it types is a
		// character, not a shortcut.
		{"AltGr", Input{Type: "keydown", Key: "@", Code: "KeyQ", KeyCode: 81, Mods: modCtrl | modAlt},
			`{"type":"keyDown","key":"@","code":"KeyQ","windowsVirtualKeyCode":81,"nativeVirtualKeyCode":81,"modifiers":0,"text":"@","unmodifiedText":"@"}`},
		{"a release", Input{Type: "keyup", Key: "a", Code: "KeyA", KeyCode: 65},
			`{"type":"keyUp","key":"a","code":"KeyA","windowsVirtualKeyCode":65,"nativeVirtualKeyCode":65,"modifiers":0}`},
	} {
		method, got, ok := c.in.command(vp)
		if !ok || method != "Input.dispatchKeyEvent" {
			t.Errorf("%s went to %q (ok %v), want Input.dispatchKeyEvent", c.name, method, ok)
			continue
		}
		raw, _ := json.Marshal(got)
		if !reflect.DeepEqual(params(t, string(raw)), params(t, c.want)) {
			t.Errorf("%s became %s, want %s", c.name, raw, c.want)
		}
	}
}

func TestComposedTextIsInsertedWhole(t *testing.T) {
	method, got, ok := Input{Type: "text", Text: "Grüße, 東京"}.command(viewport{Width: 1, Height: 1})
	raw, _ := json.Marshal(got)
	if !ok || method != "Input.insertText" || string(raw) != `{"text":"Grüße, 東京"}` {
		t.Errorf("text went to %q with %s (ok %v), want Input.insertText with the text", method, raw, ok)
	}
}

func TestInputThePageCannotSendIsDropped(t *testing.T) {
	vp := viewport{Width: 800, Height: 600}
	for name, in := range map[string]Input{
		"an unknown kind":    {Type: "Runtime.evaluate"},
		"no text":            {Type: "text"},
		"a novel":            {Type: "text", Text: strings.Repeat("x", maxText+1)},
		"a key with no name": {Type: "keydown"},
		"a long key name":    {Type: "keydown", Key: strings.Repeat("k", 40)},
		"an unknown button":  {Type: "mousedown", Button: 7},
	} {
		if method, _, ok := in.command(vp); ok {
			t.Errorf("%s was sent to %s", name, method)
		}
	}
}

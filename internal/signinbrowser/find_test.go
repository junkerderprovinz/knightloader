package signinbrowser

import "testing"

func TestTheFirstBrowserInstalledIsTaken(t *testing.T) {
	cands := []Browser{
		{Name: "Google Chrome", Path: "/missing/chrome"},
		{Name: "Microsoft Edge", Path: "/opt/edge"},
		{Name: "Chromium", Path: "/usr/bin/chromium"},
	}
	installed := func(p string) bool { return p != "/missing/chrome" }
	got, ok := firstInstalled(cands, installed)
	if !ok || got.Name != "Microsoft Edge" || got.Path != "/opt/edge" {
		t.Errorf("found %+v, %v; want Edge, the first one there", got, ok)
	}
	if _, ok := firstInstalled(cands, func(string) bool { return false }); ok {
		t.Error("a computer without any of them found one")
	}
}

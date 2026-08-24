package update

import (
	"context"
	"testing"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current   string
		wantNewer, wantOK bool
	}{
		{"1.2.0", "1.1.9", true, true},
		{"2.0.0", "1.9.9", true, true},
		{"1.0.1", "1.0.0", true, true},
		{"1.0.0", "1.0.0", false, true},
		{"1.0.0", "1.0.1", false, true},
		{"1.0.0", "2.0.0", false, true},
		{"1.0", "1.0.0", false, false},
		{"1.0.0", "not-a-version", false, false},
		{"", "1.0.0", false, false},
	}
	for _, c := range cases {
		gotNewer, gotOK := isNewer(c.latest, c.current)
		if gotOK != c.wantOK || (gotOK && gotNewer != c.wantNewer) {
			t.Errorf("isNewer(%q, %q) = (%v, %v), want (%v, %v)", c.latest, c.current, gotNewer, gotOK, c.wantNewer, c.wantOK)
		}
	}
}

func TestCheckDevBuild(t *testing.T) {
	// "dev" (every untagged local/main build) can never be meaningfully
	// compared to a release tag - Check must report it as checked-but-not-
	// available without making any network call at all.
	info := Check(context.Background(), "dev")
	if !info.Checked {
		t.Fatal("dev build: want Checked=true")
	}
	if info.Available {
		t.Fatal("dev build: want Available=false")
	}
}

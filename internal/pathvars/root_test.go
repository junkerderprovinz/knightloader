package pathvars

import (
	"path/filepath"
	"testing"
)

func TestRootIsTheLastWholeFolderBeforeAPlaceholder(t *testing.T) {
	for _, c := range []struct{ template, want string }{
		{"/downloads", "/downloads"},
		{"/downloads/<jd:packagename>", "/downloads"},
		{"/downloads/tv-<jd:simpledate:yyyy>/x", "/downloads"},
		{"/<jd:packagename>", "/"},
	} {
		if got := Root(filepath.FromSlash(c.template)); got != filepath.FromSlash(c.want) {
			t.Errorf("Root(%q) = %q, want %q", c.template, got, c.want)
		}
	}
}

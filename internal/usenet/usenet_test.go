package usenet

import (
	"slices"
	"strings"
	"testing"
)

func TestNZBSizeAddsUpTheArticles(t *testing.T) {
	nzb := strings.Replace(sampleNZB, "</segments>",
		`<segment bytes="1000" number="2">second@news</segment></segments>`, 1)
	if got := NZBSize([]byte(nzb)); got != 740067 {
		t.Errorf("size = %d, want both articles", got)
	}
	if got := NZBSize([]byte("https://host.example/a.rar")); got != 0 {
		t.Errorf("a link list has size %d, want 0", got)
	}
}

func TestAFolderEveryFileSitsInIsDropped(t *testing.T) {
	dirs := func(files []File) []string {
		var out []string
		for _, f := range files {
			out = append(out, f.Dir)
		}
		return out
	}
	for _, c := range []struct {
		name string
		in   []string
		want []string
	}{
		{"all in one folder", []string{"Film", "Film"}, []string{"", ""}},
		{"a folder and its subfolder", []string{"Film", "Film/Subs"}, []string{"", "Subs"}},
		{"two folders side by side", []string{"CD1", "CD2"}, []string{"CD1", "CD2"}},
		{"one at the top", []string{"", "Subs"}, []string{"", "Subs"}},
		{"names that only start alike", []string{"Sub", "Subs"}, []string{"Sub", "Subs"}},
	} {
		var files []File
		for _, d := range c.in {
			files = append(files, File{Name: "x", Dir: d})
		}
		if got := dirs(withoutCommonDir(files)); !slices.Equal(got, c.want) {
			t.Errorf("%s: dirs = %q, want %q", c.name, got, c.want)
		}
	}
}

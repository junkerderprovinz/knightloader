package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// TestDerivePackage pins the guess a batch gets when the user did not name one.
// A wrong guess is worse than none: it scatters one release across two groups,
// or names a group after a fragment of a filename.
func TestDerivePackage(t *testing.T) {
	cases := []struct {
		name  string
		tasks []*core.Task
		// title is what the crawled page called itself, empty for a pasted batch.
		title string
		want  string
	}{
		{
			name: "a multi-volume set collapses to its stem",
			tasks: []*core.Task{
				{URL: "https://host.example/a", Name: "Great.Film.2026.part01.rar"},
				{URL: "https://host.example/b", Name: "Great.Film.2026.part02.rar"},
				{URL: "https://host.example/c", Name: "Great.Film.2026.part03.rar"},
			},
			want: "Great.Film.2026",
		},
		{
			// "Show.S01E0" is the literal shared prefix, but it cuts the episode
			// number in half. Falling back to the last whole word is the honest
			// answer even though it loses the season.
			name: "an episode range falls back to the last whole word",
			tasks: []*core.Task{
				{URL: "https://host.example/a", Name: "Show.S01E01.mkv"},
				{URL: "https://host.example/b", Name: "Show.S01E02.mkv"},
			},
			want: "Show",
		},
		{
			name: "unrelated names on one host fall back to the host",
			tasks: []*core.Task{
				{URL: "https://files.example.com/a", Name: "invoice.pdf"},
				{URL: "https://files.example.com/b", Name: "holiday.jpg"},
			},
			want: "files.example.com",
		},
		{
			name: "unrelated names on unrelated hosts get no guess",
			tasks: []*core.Task{
				{URL: "https://one.example/a", Name: "invoice.pdf"},
				{URL: "https://two.example/b", Name: "holiday.jpg"},
			},
			want: "",
		},
		{
			name: "a single file uses its own stem",
			tasks: []*core.Task{
				{URL: "https://host.example/x", Name: "Documentary.2026.mkv"},
			},
			want: "Documentary.2026",
		},
		{
			name: "an unresolved link falls back to the URL path",
			tasks: []*core.Task{
				{URL: "https://host.example/files/Report.Final.pdf", Name: "https://host.example/files/Report.Final.pdf"},
				{URL: "https://host.example/files/Report.Draft.pdf", Name: "https://host.example/files/Report.Draft.pdf"},
			},
			want: "Report",
		},
		{
			name:  "nothing at all",
			tasks: nil,
			want:  "",
		},
		{
			// The page name beats the host, which is the whole reason the crawler
			// carries it: "files.example.com" groups everything ever fetched from
			// that host into one package.
			name: "a crawled page's own name beats the host it sits on",
			tasks: []*core.Task{
				{URL: "https://files.example.com/a", Name: "invoice.pdf"},
				{URL: "https://files.example.com/b", Name: "holiday.jpg"},
			},
			title: "Index of /pub/",
			want:  "Index of -pub-",
		},
		{
			// A shared stem is the more specific answer and stays ahead of it.
			name: "a shared stem still wins over the page name",
			tasks: []*core.Task{
				{URL: "https://host.example/a", Name: "Great.Film.2026.part01.rar"},
				{URL: "https://host.example/b", Name: "Great.Film.2026.part02.rar"},
			},
			title: "Downloads - SomeSite",
			want:  "Great.Film.2026",
		},
		{
			name: "a page with no title of its own still falls through to the host",
			tasks: []*core.Task{
				{URL: "https://files.example.com/a", Name: "invoice.pdf"},
				{URL: "https://files.example.com/b", Name: "holiday.jpg"},
			},
			title: "   ",
			want:  "files.example.com",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := derivePackage(c.tasks, c.title); got != c.want {
				t.Errorf("derivePackage() = %q, want %q", got, c.want)
			}
		})
	}
}

// TestCommonStemNeverCutsMidWord is the property that keeps the guess sane: a
// shared prefix is only used up to a separator, never through one.
func TestCommonStemNeverCutsMidWord(t *testing.T) {
	got := commonStem([]string{"Movie.S01E01", "Movie.S01E02"})
	if got != "Movie" {
		t.Errorf("commonStem = %q, want %q", got, "Movie")
	}
	// A single name is its own stem and must not be trimmed at all.
	if got := commonStem([]string{"Solo.Release.2026"}); got != "Solo.Release.2026" {
		t.Errorf("single name became %q", got)
	}
	if got := commonStem(nil); got != "" {
		t.Errorf("no names produced %q", got)
	}
}

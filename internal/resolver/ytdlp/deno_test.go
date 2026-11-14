package ytdlp

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestYtdlpIsHandedTheDenoItShouldRun(t *testing.T) {
	cases := []struct {
		name string
		deno string
		want []string
	}{
		{"no deno leaves yt-dlp to its own search", "", nil},
		{"a fetched deno", "/data/tools/deno", []string{"--js-runtimes", "deno:/data/tools/deno"}},
		{"a Windows path keeps its drive letter", `C:\KnightLoader\tools\deno.exe`, []string{"--js-runtimes", `deno:C:\KnightLoader\tools\deno.exe`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := NewBackend("yt-dlp", t.TempDir(), nil)
			b.SetDeno(c.deno)
			if got := b.denoArgs(); !slices.Equal(got, c.want) {
				t.Errorf("args %q, want %q", got, c.want)
			}
		})
	}
}

func TestEveryYtdlpRunIsHandedDeno(t *testing.T) {
	const deno = "/data/tools/deno"
	want := []string{"--js-runtimes", "deno:" + deno}

	t.Run("download", func(t *testing.T) {
		t.Setenv(runHelperEnv, "cookies:full")
		dir := t.TempDir()
		b := NewBackend(os.Args[0], dir, (&recorder{}).add)
		b.SetDeno(deno)
		b.Options = func(string) Options { return Options{} }
		b.run("task-1", "https://example.invalid/watch?v=x")
		raw, err := os.ReadFile(filepath.Join(dir, "argv.txt"))
		if err != nil {
			t.Fatalf("yt-dlp was not started: %v", err)
		}
		if argv := string(raw); !strings.Contains(argv, strings.Join(want, "\n")) {
			t.Errorf("argv lacks %q:\n%s", want, argv)
		}
	})

	t.Run("probe", func(t *testing.T) {
		b := fakeYtdlpBackend(t, "browser")
		b.SetDeno(deno)
		got, err := b.ProbeTitle(context.Background(), "https://youtube.com/watch?v=x", nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got.Title, strings.Join(want, " ")) {
			t.Errorf("yt-dlp was invoked as %q, want %q in it", got.Title, want)
		}
	})

	t.Run("playlist", func(t *testing.T) {
		b := fakeYtdlpBackend(t, "echoargs")
		b.SetDeno(deno)
		pl, err := b.ProbePlaylist(context.Background(), "https://youtube.com/playlist?list=PL1")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(pl.Title, strings.Join(want, " ")) {
			t.Errorf("yt-dlp was invoked as %q, want %q in it", pl.Title, want)
		}
	})
}

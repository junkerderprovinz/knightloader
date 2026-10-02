package ytdlp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

func TestABrowsersCookiesStayOnTheirHost(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	got := withBrowserCookies("", "https://Media.example:8443/live/index.m3u8", "sid=abc; theme=dark; =stray; bad=a\tb", now)
	want := cookieFileHeader + "\n" +
		"media.example\tFALSE\t/\tTRUE\t1800086400\tsid\tabc\n" +
		"media.example\tFALSE\t/\tTRUE\t1800086400\ttheme\tdark\n"
	if got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}

	if got := withBrowserCookies("", "http://media.example/a.mp4", "sid=abc", now); !strings.Contains(got, "\t/\tFALSE\t") {
		t.Errorf("a cookie for a plain http link is marked https only:\n%s", got)
	}
}

func TestABrowsersCookiesJoinAStoredJar(t *testing.T) {
	stored := cookieFileHeader + "\n.media.example\tTRUE\t/\tTRUE\t0\tstored\tyes"
	got := withBrowserCookies(stored, "https://media.example/a.mp4", "sid=abc", time.Unix(0, 0))
	if !strings.HasPrefix(got, stored+"\n") || !strings.HasSuffix(got, "\tsid\tabc\n") {
		t.Errorf("got %q, want the stored jar followed by the browser's cookie", got)
	}
	if got := withBrowserCookies(stored, "https://media.example/a.mp4", "", time.Unix(0, 0)); got != stored {
		t.Errorf("without a browser cookie the stored jar changed to %q", got)
	}
}

func TestRunHandsTheBrowsersHeadersToYtdlp(t *testing.T) {
	const secret = "s3cr3t-session"
	t.Setenv(runHelperEnv, "cookies:full")
	dir := t.TempDir()
	rec := &recorder{}
	b := NewBackend(os.Args[0], dir, rec.add)
	b.Options = func(string) Options { return Options{} }
	var askedFor string
	b.Headers = func(taskID, rawurl string) map[string]string {
		askedFor = taskID + " " + rawurl
		return map[string]string{
			"Cookie":     "sid=" + secret,
			"Referer":    "https://site.example/watch/7",
			"User-Agent": "Mozilla/5.0 (Test)",
		}
	}
	b.run("task-1", "https://media.example/live/index.m3u8")

	if askedFor != "task-1 https://media.example/live/index.m3u8" {
		t.Errorf("headers asked for %q", askedFor)
	}
	argv, err := os.ReadFile(filepath.Join(dir, "argv.txt"))
	if err != nil {
		t.Fatalf("the helper recorded no argv: %v", err)
	}
	args := string(argv)
	for _, want := range []string{"--add-header\nReferer:https://site.example/watch/7", "--add-header\nUser-Agent:Mozilla/5.0 (Test)", "--cookies"} {
		if !strings.Contains(args, want) {
			t.Errorf("argv lacks %q:\n%s", want, args)
		}
	}
	if strings.Contains(args, secret) {
		t.Error("the cookie value is on the command line, where any process can read it")
	}
	seen, err := os.ReadFile(filepath.Join(dir, "jar-seen.txt"))
	if err != nil {
		t.Fatalf("the child could not read the jar: %v", err)
	}
	if !strings.Contains(string(seen), "media.example\tFALSE\t/\tTRUE\t") || !strings.Contains(string(seen), "\tsid\t"+secret) {
		t.Errorf("the jar holds %q, want the browser's cookie for media.example alone", seen)
	}
	for _, u := range rec.all() {
		if strings.Contains(u.Err+u.Note, secret) {
			t.Fatalf("an update carried the cookie: %+v", u)
		}
	}
	if last := rec.last(); last.Status == core.StatusError {
		t.Errorf("the run failed: %s", last.Err)
	}
}

package app

// Routing as a fresh container starts: a JDownloader sidecar in KL_JD, yt-dlp,
// and no account anywhere. TorBox's public host list is fetched then too, for
// yt-dlp's sake, and it names GitHub, archive.org, Google's and Discord's file
// servers and Imgur among its file hosters.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
)

// torboxFileHosters is a slice of TorBox's live type:"hoster" entries.
var torboxFileHosters = []string{
	"github.com", "googleusercontent.com", "drive.usercontent.google.com",
	"cdn.discordapp.com", "imgur.com", "archive.org", "rapidgator.net",
}

// plainFiles are files on those hosts that a plain GET fetches as they are.
var plainFiles = []string{
	"https://github.com/cli/cli/releases/download/v2.60.0/gh_2.60.0_linux_amd64.tar.gz",
	"https://archive.org/download/x/x.mp4",
	"https://lh3.googleusercontent.com/abc/photo.jpg",
	"https://cdn.discordapp.com/attachments/1/2/file.zip",
	"https://i.imgur.com/abc123.png",
}

// defaultSetupApp starts an App against a JD that answers, with yt-dlp when
// withYtdlp is set, no account, and TorBox's lists as the last good copy in
// host_cache.json, since a test never reaches TorBox.
func defaultSetupApp(t *testing.T, withYtdlp bool) *App {
	t.Helper()
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":null}`))
	}))
	t.Cleanup(sidecar.Close)
	t.Setenv("KL_JD", sidecar.URL)
	for _, svc := range accounts.Catalogue {
		if svc.Env != "" {
			t.Setenv(svc.Env, "")
		}
	}
	if withYtdlp {
		t.Setenv("KL_YTDLP", ytdlpStub(t))
	}

	dir := t.TempDir()
	stream := []string{"youtube.com", "vimeo.com"}
	cache := map[string]hostCacheEntry{
		"torbox":             {Hosts: append(append([]string(nil), torboxFileHosters...), stream...), FetchedAt: time.Now()},
		"torbox-hoster-only": {Hosts: torboxFileHosters, FetchedAt: time.Now()},
	}
	b, err := json.Marshal(cache)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "host_cache.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}

	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		a.Close()
		jd.SetMediaHosts(nil)
	})
	ids := registeredIDs(a)
	if !ids["jd"] {
		t.Fatal("setup: the JD sidecar was not wired")
	}
	if ids["ytdlp"] != withYtdlp {
		t.Fatalf("setup: yt-dlp wired = %v, want %v", ids["ytdlp"], withYtdlp)
	}
	return a
}

// ytdlpStub is a yt-dlp that answers --version, so rewireBackends wires the
// backend without the real binary.
func ytdlpStub(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ytdlp-stub")
	script := "#!/bin/sh\necho 2026.01.01\n"
	if runtime.GOOS == "windows" {
		bin += ".bat"
		script = "@echo 2026.01.01\r\n"
	}
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if exec.Command(bin, "--version").Run() != nil {
		t.Skip("the yt-dlp stub is not executable here")
	}
	return bin
}

func TestPlainFilesGoToTheDirectDownloadWithoutAnAccount(t *testing.T) {
	for _, withYtdlp := range []bool{false, true} {
		name := "without yt-dlp"
		if withYtdlp {
			name = "with yt-dlp"
		}
		t.Run(name, func(t *testing.T) {
			a := defaultSetupApp(t, withYtdlp)
			for _, u := range plainFiles {
				if got := resolverIDOf(a.resolverForTaskLocked(&core.Task{URL: u})); got != "direct" {
					t.Errorf("%s goes to %q, want direct", u, got)
				}
				if host := hostOf(u); a.claims.pageOnly(host) {
					t.Errorf("the direct download and the HTTP fallback leave %s alone", host)
				}
			}
			// The file hosters on the same list stay out of reach of a plain GET.
			if got := resolverIDOf(a.resolverForTaskLocked(&core.Task{URL: "https://rapidgator.net/file/0a1b2c/movie.mkv"})); got != "jd" {
				t.Errorf("a rapidgator.net link goes to %q, want jd", got)
			}
		})
	}
}

func TestAHostAWiredAccountListsIsLeftToThatAccount(t *testing.T) {
	a := defaultSetupApp(t, false)
	release := &core.Task{URL: plainFiles[0]}

	seedAccount(t, a, "torbox", "", "torbox-key")
	a.rewireBackends()
	if got := resolverIDOf(a.resolverForTaskLocked(release)); got != "torbox" {
		t.Errorf("with a TorBox account a GitHub release goes to %q, want torbox", got)
	}
	if !a.claims.pageOnly("github.com") {
		t.Error("with a TorBox account the direct download may still take github.com")
	}
	if _, err := a.SaveResolverOrder([]string{"direct", "torbox"}); err != nil {
		t.Fatal(err)
	}
	if got := resolverIDOf(a.resolverForTaskLocked(release)); got != "torbox" {
		t.Errorf("with the direct download above TorBox a GitHub release goes to %q, want torbox", got)
	}

	seedAccount(t, a, "torbox", "", "")
	slotTestHosts(t, "github.com")
	seedAccount(t, a, "alldebrid", "", "alldebrid-key")
	a.rewireBackends()
	if got := resolverIDOf(a.resolverForTaskLocked(release)); got != "alldebrid" {
		t.Errorf("with a debrid account that lists github.com a GitHub release goes to %q, want alldebrid", got)
	}
	if !a.claims.pageOnly("github.com") {
		t.Error("with a debrid account that lists github.com the direct download may still take it")
	}
}

// JD knows far more file hosters than TorBox lists, and a host missing from
// TorBox's list is not a video site for that.
func TestAHosterOnlyJDKnowsGoesToJDWhileYtdlpRuns(t *testing.T) {
	a := defaultSetupApp(t, true)
	t.Cleanup(func() { jd.SetKnownHosts(nil) })
	jd.SetKnownHosts([]string{"uploadgig.com", "rapidgator.net", "youtube.com"})

	named := &core.Task{URL: "https://uploadgig.com/file/download/0a1b2c3d/movie.part1.rar"}
	bare := &core.Task{URL: "https://uploadgig.com/file/download/0a1b2c3d"}
	video := &core.Task{URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ"}
	check := func(order string) {
		t.Helper()
		for _, task := range []*core.Task{named, bare} {
			if got := resolverIDOf(a.resolverForTaskLocked(task)); got != "jd" {
				t.Errorf("%s: %s goes to %q, want jd", order, task.URL, got)
			}
			after := &core.Task{URL: task.URL, Resolver: "jd"}
			if next := a.nextResolverLocked(after); next == "direct" || next == "http" {
				t.Errorf("%s: after jd, %s falls back to %q", order, task.URL, next)
			}
		}
		if got := resolverIDOf(a.resolverForTaskLocked(video)); got != "ytdlp" {
			t.Errorf("%s: a YouTube link goes to %q, want ytdlp", order, got)
		}
	}

	check("automatic order")
	if _, err := a.SaveResolverOrder([]string{"direct", "ytdlp", "jd"}); err != nil {
		t.Fatal(err)
	}
	check("direct and yt-dlp above JD")
}

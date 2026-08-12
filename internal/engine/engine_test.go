package engine

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GopeedLab/gopeed/pkg/base"
	gopeed "github.com/GopeedLab/gopeed/pkg/util"
	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/proxycfg"
)

// TestAnUnroutedDownloadFollowsTheGlobalProxy is the trap this mapping exists to
// avoid, and it is worth a test rather than a comment because both answers look
// correct.
//
// nil means "follow the global config", which is the loopback proxy the speed
// limit lives in. The mode that reads like the right one for an unproxied
// download - RequestProxyModeNone - means no proxy handler at all, so it would
// take the download off the meter as well. The bug that follows is a speed limit
// that silently does nothing, on the downloads somebody was most deliberate
// about.
func TestAnUnroutedDownloadFollowsTheGlobalProxy(t *testing.T) {
	for _, name := range []string{"no route at all", "the direct gateway"} {
		r := proxycfg.Route{}
		if name == "the direct gateway" {
			var err error
			r, err = proxycfg.Direct().Route()
			if err != nil {
				t.Fatalf("the direct gateway has no route: %v", err)
			}
		}
		if got := requestProxy(r); got != nil {
			t.Fatalf("%s produced %+v, want nil so the loopback meter still applies", name, got)
		}
	}
}

// TestARoutedDownloadNamesItsOwnProxy. Custom is the only mode gopeed resolves
// in favour of the request; follow and none both hand the download back to the
// global config, which would mean the connection the user picked was ignored
// with nothing to say so.
func TestARoutedDownloadNamesItsOwnProxy(t *testing.T) {
	e := proxycfg.Entry{ID: "3", Kind: proxycfg.KindSOCKS5, Host: "proxy.lan", Port: 1080, Username: "alice", Password: "s3cret", Enabled: true}
	r, err := e.Route()
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	got := requestProxy(r)
	if got == nil {
		t.Fatal("a routed download produced no request proxy")
	}
	want := base.RequestProxy{
		Mode:   base.RequestProxyModeCustom,
		Scheme: "socks5",
		Host:   "proxy.lan:1080",
		Usr:    "alice",
		Pwd:    "s3cret",
	}
	if *got != want {
		t.Fatalf("requestProxy = %+v, want %+v", *got, want)
	}
	// The handler is what gopeed actually calls; a mode or a field it does not
	// like produces a nil one and the download quietly goes out unproxied.
	if got.ToHandler() == nil {
		t.Fatal("gopeed built no proxy handler from this route, so the download would go out unproxied")
	}
}

// oneFile is what the HTTP fetcher resolves to: a resource with no name of its
// own and a single file in it. A resource that DOES carry a name is a folder,
// which is the case placeFolder exists for.
func oneFile(name string) *base.Resource {
	return &base.Resource{Size: 7, Files: []*base.FileInfo{{Name: name, Size: 7}}}
}

func optsIn(dir string) *base.Options {
	return &base.Options{Path: dir}
}

// wouldRenameTo is what the download library's own duplicate check would do to
// the name we hand it. It splits on "/" and nothing else, so the path is built
// the way the library builds it rather than with filepath.
func wouldRenameTo(t *testing.T, dir, name string) string {
	t.Helper()
	got, err := gopeed.CheckDuplicateAndRename(path.Join(filepath.ToSlash(dir), name))
	if err != nil {
		t.Fatalf("the library's own duplicate check failed: %v", err)
	}
	return got
}

func writeFile(t *testing.T, p string) {
	t.Helper()
	if err := os.WriteFile(p, []byte("already here"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// THE TRAP THIS WIRING EXISTS FOR. The fetcher manager reports AutoRename true
// unconditionally and there is no configuration that turns it off, so whatever
// name it is handed goes through its own duplicate check. If the reservation is
// still sitting on that name the check finds a file and counts again: the user
// asked for one rename and gets "movie (2) (2).mkv", one more bracket per retry.
func TestTheLibraryDoesNotRenameOnTopOfOurRename(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "movie.mkv"))

	opts := optsIn(dir)
	name, err := place(Job{Collision: collide.Rename}, oneFile("movie.mkv"), opts)
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	if name != "movie (2).mkv" || opts.Name != "movie (2).mkv" {
		t.Fatalf("name = %q, Options.Name = %q; want movie (2).mkv for both", name, opts.Name)
	}
	if _, err := os.Stat(filepath.Join(dir, opts.Name)); err == nil {
		t.Fatal("the reservation is still on disk, so the library will rename around it")
	}
	if got := wouldRenameTo(t, dir, opts.Name); got != opts.Name {
		t.Fatalf("the library would turn %q into %q", opts.Name, got)
	}
	// The file that was already there is what the whole policy is protecting.
	if b, err := os.ReadFile(filepath.Join(dir, "movie.mkv")); err != nil || string(b) != "already here" {
		t.Fatalf("the existing file was touched: %q, %v", b, err)
	}
}

// The name has to be sanitized before it is reserved, not after. If this fails,
// the reservation is made under one name and the library writes another, so it
// reserved nothing and the download lands on top of whatever is at the name it
// really uses.
func TestTheReservedNameIsTheNameTheLibraryWillWrite(t *testing.T) {
	cases := []struct {
		name string
		file string
	}{
		{"a character the library replaces", "sea:son.mkv"},
		{"longer than the library allows", strings.Repeat("n", 150) + ".mkv"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			opts := optsIn(dir)
			got, err := place(Job{Collision: collide.Rename}, oneFile(c.file), opts)
			if err != nil {
				t.Fatalf("place: %v", err)
			}
			if want := gopeed.SafeFilename(got); want != got {
				t.Fatalf("handed over %q, which the library rewrites to %q", got, want)
			}
			// And the reservation really was made at that name: with the sanitized
			// name occupied, the next attempt must count up instead of handing the
			// same one out again.
			writeFile(t, filepath.Join(dir, got))
			next, err := place(Job{Collision: collide.Rename}, oneFile(c.file), optsIn(dir))
			if err != nil {
				t.Fatalf("second place: %v", err)
			}
			if next == got {
				t.Fatalf("the taken name %q was handed out a second time", got)
			}
			if want := gopeed.SafeFilename(next); want != next {
				t.Fatalf("counted name %q would be rewritten to %q", next, want)
			}
		})
	}
}

// A resource that carries a name of its own is a FOLDER, and Options.Name then
// names the folder rather than a file inside it. If this fails, the policy is
// applied to the wrong thing and it does not even fail visibly: the download
// succeeds, into a directory called "movie (2).mkv".
func TestAMultiFileResourceIsTreatedAsTheFolderItIs(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "Show.S01"), 0o755); err != nil {
		t.Fatal(err)
	}
	res := &base.Resource{Name: "Show.S01", Files: []*base.FileInfo{{Name: "ep01.mkv"}}}

	opts := optsIn(dir)
	name, err := place(Job{Collision: collide.Rename}, res, opts)
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	if opts.Name != "Show.S01 (2)" {
		t.Fatalf("Options.Name = %q, want the counted FOLDER name Show.S01 (2)", opts.Name)
	}
	// The task keeps the file's name: renaming the folder did not move the file.
	if name != "ep01.mkv" {
		t.Fatalf("reported name = %q, want the file's own", name)
	}
	if _, err := os.Stat(filepath.Join(dir, opts.Name)); err == nil {
		t.Fatal("the reserved folder is still on disk")
	}
}

// Overwrite on a folder would delete a tree nobody named. Refusing is the honest
// answer; if this fails, a setting chosen for files empties a directory.
func TestOverwriteIsRefusedForAFolderRatherThanApplied(t *testing.T) {
	dir := t.TempDir()
	inside := filepath.Join(dir, "Show.S01")
	if err := os.Mkdir(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(inside, "ep01.mkv"))
	res := &base.Resource{Name: "Show.S01", Files: []*base.FileInfo{{Name: "ep01.mkv"}}}

	opts := optsIn(dir)
	if _, err := place(Job{Collision: collide.Overwrite}, res, opts); !errors.Is(err, collide.ErrFolderOverwrite) {
		t.Fatalf("error = %v, want ErrFolderOverwrite", err)
	}
	if opts.Name != "" {
		t.Fatalf("Options.Name = %q; a refused policy must not name anything", opts.Name)
	}
	if _, err := os.Stat(filepath.Join(inside, "ep01.mkv")); err != nil {
		t.Fatal("the folder was emptied by a policy that is supposed to refuse")
	}
}

// Skip settles the task instead of downloading, and it must name the file that
// is in the way: "not downloaded" with nothing after it tells nobody anything.
func TestSkipRefusesToStartAndSaysWhat(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "movie.mkv"))

	opts := optsIn(dir)
	name, err := place(Job{Collision: collide.Skip}, oneFile("movie.mkv"), opts)
	if err == nil {
		t.Fatal("skip started a download over a file that was already there")
	}
	if !strings.Contains(err.Error(), "movie.mkv") {
		t.Fatalf("error %q does not name the file in the way", err)
	}
	if name != "movie.mkv" {
		t.Fatalf("name = %q; the resolved name has to travel with the failure", name)
	}
	if opts.Name != "" {
		t.Fatalf("Options.Name = %q; nothing was reserved", opts.Name)
	}
}

// An empty policy means NO policy here, and the collide package reads the same
// empty string as its own default. If this fails, every caller of the plain
// Download entry point silently gains a rename it never asked for.
func TestNoPolicyLeavesTheLibraryToNameTheFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "movie.mkv"))

	opts := optsIn(dir)
	name, err := place(Job{}, oneFile("movie.mkv"), opts)
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	if opts.Name != "" {
		t.Fatalf("Options.Name = %q; with no policy the library names the file", opts.Name)
	}
	if name != "movie.mkv" {
		t.Fatalf("name = %q, want the resolved one", name)
	}
	if _, err := os.Stat(filepath.Join(dir, "movie.mkv")); err != nil {
		t.Fatal("a job with no policy still touched the folder")
	}
}

// TestStartAfterCloseAnswersInsteadOfPanicking is the guard at the top of
// Start proven rather than merely present: every path through Start ends in
// e.wg.Add(1), and by the time a caller can reach Start after Close has
// begun, Close is already past close(e.done) and quite possibly already
// inside e.wg.Wait(). Add racing a Wait already under way is not a slow
// task, it is documented Go runtime behaviour ("sync: WaitGroup misuse: Add
// called concurrently with Wait") that panics the whole process. Close has
// already fully returned here, which is the one interleaving guaranteed to
// still be true by the time this Start call runs, so this is the
// deterministic slice of the race rather than an attempt to reproduce the
// timing-dependent one.
func TestStartAfterCloseAnswersInsteadOfPanicking(t *testing.T) {
	var mu sync.Mutex
	var got *core.Update
	e, err := New(t.TempDir(), func(_ string, u core.Update) {
		mu.Lock()
		defer mu.Unlock()
		got = &u
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// A URL nothing could ever answer - if the guard did not stop this before
	// e.wg.Add(1), this would hang or panic rather than merely fail the
	// assertions below.
	e.Start(Job{TaskID: "late-1", URL: "http://127.0.0.1:1/unreachable"})

	mu.Lock()
	defer mu.Unlock()
	if got == nil {
		t.Fatal("Start after Close produced no update at all")
	}
	if got.Status != core.StatusError {
		t.Errorf("status = %q, want error", got.Status)
	}
	if got.Err != "shutting down" {
		t.Errorf("err = %q, want \"shutting down\"", got.Err)
	}
}

// TestConcurrentStartAndCloseNeverPanics is the interleaving the test above
// cannot reach: Close already fully returned there, which is the one
// ordering the old select/default guard actually handled correctly. What it
// missed - Start's own e.done check and its e.wg.Add(1) as two separate
// steps, with Close free to land its own e.wg.Wait in between - only shows
// up when a Start call is genuinely racing a Close, not following one.
// That gap needed a real, live BitTorrent swarm and the race detector to
// surface at all (see this package's own git history) - synthetic
// concurrency here cannot force the exact interleaving on demand, so this
// runs many overlapping attempts and leans on -race in CI to catch what a
// single run might miss. A caller-side panic recovered into t.Errorf is a
// clean, attributable failure; an unrecovered WaitGroup-misuse panic on some
// other goroutine crashes the whole test binary instead, which is still a
// failure but a cruder one - this exists to make the common case the clean
// one.
func TestConcurrentStartAndCloseNeverPanics(t *testing.T) {
	for i := 0; i < 20; i++ {
		e, err := New(t.TempDir(), func(string, core.Update) {})
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		var wg sync.WaitGroup
		for n := 0; n < 8; n++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("Start panicked: %v", r)
					}
				}()
				e.Start(Job{
					TaskID: fmt.Sprintf("stress-%d-%d", i, n),
					URL:    "http://127.0.0.1:1/unreachable",
				})
			}(n)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Close panicked: %v", r)
				}
			}()
			if err := e.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
		}()

		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("round %d: Start/Close never finished - deadlock, not a data race", i)
		}
	}
}

// TestSetTorrentConfigReachesGopeedsOwnProtocolConfig is the read side of the
// write SetTorrentConfig does: gopeed's own Fetcher.Setup (internal/protocol/
// bt/fetcher.go) reads ProtocolConfig["bt"] back through exactly the same
// GetConfig + util.MapToStruct path this test uses, so decoding it back the
// same way is what "did this actually reach gopeed" means from this side of
// the boundary. Distinct, mutually unmistakable values for the three fields -
// not e.g. matching port and seed-duration - so a positional-argument mix-up
// in SetTorrentConfig's own body would fail this test rather than pass it by
// coincidence.
func TestSetTorrentConfigReachesGopeedsOwnProtocolConfig(t *testing.T) {
	e, err := New(t.TempDir(), func(string, core.Update) {})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer e.Close()

	if err := e.SetTorrentConfig(6969, 2.5, 10800); err != nil {
		t.Fatalf("SetTorrentConfig: %v", err)
	}

	cfg, err := e.d.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	var bt btProtocolConfig
	if err := gopeed.MapToStruct(cfg.ProtocolConfig["bt"], &bt); err != nil {
		t.Fatalf("MapToStruct: %v", err)
	}
	if bt.ListenPort != 6969 {
		t.Errorf("ListenPort = %d, want 6969", bt.ListenPort)
	}
	if bt.SeedRatio != 2.5 {
		t.Errorf("SeedRatio = %v, want 2.5", bt.SeedRatio)
	}
	if bt.SeedTime != 10800 {
		t.Errorf("SeedTime = %d, want 10800", bt.SeedTime)
	}
}

// TestSetTorrentConfigLeavesUnrelatedConfigAlone is the read-modify-write
// this function has to be, not the construct-fresh-and-overwrite it would be
// one refactor away from becoming: base.DownloaderStoreConfig carries proxy,
// download directory, concurrency cap and every other protocol's own config
// alongside ProtocolConfig["bt"], all in the one struct GetConfig/PutConfig
// round-trip whole. A version of SetTorrentConfig that built a fresh
// DownloaderStoreConfig instead of mutating the one GetConfig returned would
// still pass the test above and silently wipe the proxy this engine's own
// speed limiter depends on (see UseProxy's own doc comment) - this is what
// catches that.
func TestSetTorrentConfigLeavesUnrelatedConfigAlone(t *testing.T) {
	e, err := New(t.TempDir(), func(string, core.Update) {})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer e.Close()

	if err := e.UseProxy("127.0.0.1:9"); err != nil {
		t.Fatalf("UseProxy: %v", err)
	}
	before, err := e.d.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	wantDownloadDir := before.DownloadDir
	wantMaxRunning := before.MaxRunning
	wantProxyHost := before.Proxy.Host

	if err := e.SetTorrentConfig(51413, 1.0, 7200); err != nil {
		t.Fatalf("SetTorrentConfig: %v", err)
	}

	after, err := e.d.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if after.DownloadDir != wantDownloadDir {
		t.Errorf("DownloadDir = %q, want %q (SetTorrentConfig must not touch it)", after.DownloadDir, wantDownloadDir)
	}
	if after.MaxRunning != wantMaxRunning {
		t.Errorf("MaxRunning = %d, want %d (SetTorrentConfig must not touch it)", after.MaxRunning, wantMaxRunning)
	}
	if after.Proxy == nil || after.Proxy.Host != wantProxyHost {
		t.Errorf("Proxy.Host = %v, want %q (SetTorrentConfig must not touch it)", after.Proxy, wantProxyHost)
	}
}

// TestSetTorrentConfigOverwritesRatherThanAccumulates guards the other
// direction from the two tests above: a second call with different numbers
// must leave the second call's numbers in place, not the first's and not
// some mix of both - the read-modify-write reads gopeed's CURRENT bt config
// each time, which on a naive implementation could mean an old field
// surviving a call that meant to replace it.
func TestSetTorrentConfigOverwritesRatherThanAccumulates(t *testing.T) {
	e, err := New(t.TempDir(), func(string, core.Update) {})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer e.Close()

	if err := e.SetTorrentConfig(1111, 1.0, 3600); err != nil {
		t.Fatalf("SetTorrentConfig (first): %v", err)
	}
	if err := e.SetTorrentConfig(2222, 3.0, 7200); err != nil {
		t.Fatalf("SetTorrentConfig (second): %v", err)
	}

	cfg, err := e.d.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	var bt btProtocolConfig
	if err := gopeed.MapToStruct(cfg.ProtocolConfig["bt"], &bt); err != nil {
		t.Fatalf("MapToStruct: %v", err)
	}
	if bt.ListenPort != 2222 || bt.SeedRatio != 3.0 || bt.SeedTime != 7200 {
		t.Errorf("after two calls: ListenPort=%d SeedRatio=%v SeedTime=%d, want 2222/3/7200 (the second call's own values)",
			bt.ListenPort, bt.SeedRatio, bt.SeedTime)
	}
}

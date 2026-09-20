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

// TestAnUnroutedDownloadFollowsTheGlobalProxy checks for nil, which keeps the
// loopback meter. RequestProxyModeNone looks equivalent but would drop the
// speed limit.
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

// TestARoutedDownloadNamesItsOwnProxy checks for the custom mode, the only
// one gopeed resolves in favour of the request.
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
	if got.ToHandler() == nil {
		t.Fatal("gopeed built no proxy handler from this route, so the download would go out unproxied")
	}
}

// oneFile is what the HTTP fetcher resolves to: no name of its own and a
// single file. A named resource is a folder.
func oneFile(name string) *base.Resource {
	return &base.Resource{Size: 7, Files: []*base.FileInfo{{Name: name, Size: 7}}}
}

func optsIn(dir string) *base.Options {
	return &base.Options{Path: dir}
}

// wouldRenameTo runs the library's own duplicate check on name. It splits on
// "/" only, so the path is built with path rather than filepath.
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

// TestTheLibraryDoesNotRenameOnTopOfOurRename: the fetcher always runs its
// own duplicate check, so a reservation left on the name would turn
// "movie (2).mkv" into "movie (2) (2).mkv".
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
	if b, err := os.ReadFile(filepath.Join(dir, "movie.mkv")); err != nil || string(b) != "already here" {
		t.Fatalf("the existing file was touched: %q, %v", b, err)
	}
}

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
			// With the sanitised name occupied, the next attempt must count up.
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
	if name != "ep01.mkv" {
		t.Fatalf("reported name = %q, want the file's own", name)
	}
	if _, err := os.Stat(filepath.Join(dir, opts.Name)); err == nil {
		t.Fatal("the reserved folder is still on disk")
	}
}

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

// TestNoPolicyLeavesTheLibraryToNameTheFile: an empty policy is no policy
// here, although collide reads it as Rename.
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

func TestAWorkingFolderIsWhereTheBytesGo(t *testing.T) {
	dest, work := t.TempDir(), t.TempDir()
	j := Job{Dir: dest, WorkDir: work}
	if got := j.writeDir(); got != work {
		t.Fatalf("writeDir = %q, want the working folder %q", got, work)
	}
	if got := (Job{Dir: dest}).writeDir(); got != dest {
		t.Fatalf("writeDir = %q, want the destination %q", got, dest)
	}
}

// TestAJobWithAWorkingFolderDecidesNoNameHere: a name counted in the working
// folder would be counted again at the destination by the mover.
func TestAJobWithAWorkingFolderDecidesNoNameHere(t *testing.T) {
	work := t.TempDir()
	// A half-written file from the previous attempt.
	writeFile(t, filepath.Join(work, "movie.mkv"))

	opts := optsIn(work)
	name, err := place(Job{Dir: t.TempDir(), WorkDir: work, Collision: collide.Rename}, oneFile("movie.mkv"), opts)
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	if opts.Name != "" {
		t.Fatalf("Options.Name = %q; the policy belongs at the destination, not in the working folder", opts.Name)
	}
	if name != "movie.mkv" {
		t.Fatalf("name = %q, want the resolved one", name)
	}
	// Without the working folder the same job does decide a name.
	plain := optsIn(work)
	if _, err := place(Job{Dir: work, Collision: collide.Rename}, oneFile("movie.mkv"), plain); err != nil {
		t.Fatalf("place: %v", err)
	}
	if plain.Name != "movie (2).mkv" {
		t.Fatalf("Options.Name = %q, want movie (2).mkv", plain.Name)
	}
}

// TestStartAfterCloseAnswersInsteadOfPanicking covers the deterministic part
// of the Start/Close race: a wg.Add after Close has begun its Wait would
// panic the process.
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

// TestConcurrentStartAndCloseNeverPanics races Start against Close many
// times. The exact interleaving cannot be forced, so this relies on -race in
// CI; a recovered panic reports cleanly, an unrecovered one crashes the test
// binary.
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
			t.Fatalf("round %d: Start/Close never finished; deadlock, not a data race", i)
		}
	}
}

// TestSetTorrentConfigReachesGopeedsOwnProtocolConfig reads the config back
// the way gopeed's fetcher does. The three values differ so a swapped
// argument fails.
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

// TestSetTorrentConfigLeavesUnrelatedConfigAlone guards the read-modify-write:
// writing a fresh config would wipe the proxy the speed limit depends on.
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

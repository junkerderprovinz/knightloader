package engine

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GopeedLab/gopeed/pkg/base"
	gopeed "github.com/GopeedLab/gopeed/pkg/util"
	"github.com/junkerderprovinz/knightloader/internal/collide"
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

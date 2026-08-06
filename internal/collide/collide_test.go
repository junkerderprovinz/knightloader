package collide

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("stat %s: %v", path, err)
	}
	return err == nil
}

// info is a real fs.FileInfo for the fake Stat functions below, so a test can
// say "this name is taken" without arranging the file on disk.
func info(t *testing.T) fs.FileInfo {
	t.Helper()
	fi, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return fi
}

// If this fails, "archive.tar.gz" is renamed to "archive (2).gz" and no
// unpacker recognises the result, or a dotfile loses its name entirely.
func TestSplitNameKeepsTheExtensionAnUnpackerNeeds(t *testing.T) {
	cases := []struct {
		name     string
		wantStem string
		wantExt  string
	}{
		{"name.txt", "name", ".txt"},
		{"archive.tar.gz", "archive", ".tar.gz"},
		{"archive.tar.bz2", "archive", ".tar.bz2"},
		{"archive.TAR.ZST", "archive", ".TAR.ZST"},
		{"archive.tar", "archive", ".tar"},
		// Release names are full of dots; only ".tar" may be folded into the
		// extension or the episode number ends up behind the counter.
		{"Show.S01E02.1080p.mkv", "Show.S01E02.1080p", ".mkv"},
		{"no-extension", "no-extension", ""},
		{".gitignore", ".gitignore", ""},
		{".env.local", ".env", ".local"},
		{".tar.gz", ".tar", ".gz"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stem, ext := splitName(c.name)
			if stem != c.wantStem || ext != c.wantExt {
				t.Fatalf("splitName(%q) = %q, %q; want %q, %q", c.name, stem, ext, c.wantStem, c.wantExt)
			}
		})
	}
}

// If this fails, a policy the user chose in settings does something other than
// what its name promises - which for overwrite means losing a finished file.
func TestReserveAppliesThePolicy(t *testing.T) {
	const had = "already here"
	cases := []struct {
		name       string
		policy     Policy
		existing   bool
		wantAction Action
		wantBase   string
		wantErr    error
		wantKept   bool // the file that was already there still holds its bytes
	}{
		{name: "overwrite truncates", policy: Overwrite, existing: true, wantAction: Overwritten, wantBase: "file.txt"},
		{name: "overwrite on a free name", policy: Overwrite, wantAction: Created, wantBase: "file.txt"},
		{name: "skip leaves it alone", policy: Skip, existing: true, wantAction: Skipped, wantBase: "file.txt", wantKept: true},
		{name: "skip on a free name", policy: Skip, wantAction: Created, wantBase: "file.txt"},
		{name: "rename counts up", policy: Rename, existing: true, wantAction: Renamed, wantBase: "file (2).txt", wantKept: true},
		{name: "rename on a free name", policy: Rename, wantAction: Created, wantBase: "file.txt"},
		{name: "ask stalls", policy: Ask, existing: true, wantAction: NeedsDecision, wantBase: "file.txt", wantErr: ErrNeedsDecision, wantKept: true},
		{name: "ask on a free name", policy: Ask, wantAction: Created, wantBase: "file.txt"},
		// An empty policy is a settings file written before the setting existed;
		// it must download, not fail.
		{name: "empty policy falls back", policy: "", existing: true, wantAction: Renamed, wantBase: "file (2).txt", wantKept: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "file.txt")
			if c.existing {
				write(t, target, had)
			}
			res, err := Reserve(target, c.policy)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("Reserve error = %v, want %v", err, c.wantErr)
			}
			if res.Action != c.wantAction {
				t.Fatalf("Action = %q, want %q", res.Action, c.wantAction)
			}
			if got := filepath.Base(res.Path); got != c.wantBase {
				t.Fatalf("Path = %q, want base %q", res.Path, c.wantBase)
			}
			if c.wantKept && read(t, target) != had {
				t.Fatalf("existing file was modified: %q", read(t, target))
			}
			switch c.wantAction {
			case Skipped, NeedsDecision:
				if res.File != nil {
					t.Fatal("File is set although nothing was reserved")
				}
			default:
				if res.File == nil {
					t.Fatal("File is nil, so the reserved name is not held")
				}
				defer res.File.Close()
				if !exists(t, res.Path) {
					t.Fatalf("%s was not created, so the name is not reserved", res.Path)
				}
				if fi, err := res.File.Stat(); err != nil {
					t.Fatal(err)
				} else if fi.Size() != 0 {
					t.Fatalf("reserved file has %d bytes, want an empty file", fi.Size())
				}
			}
		})
	}
}

// If this fails, the handle is not usable for the download it was reserved for.
func TestReserveHandsBackAWritableHandle(t *testing.T) {
	target := filepath.Join(t.TempDir(), "file.bin")
	res, err := Reserve(target, Rename)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if _, err := res.File.WriteString("payload"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := res.File.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := read(t, res.Path); got != "payload" {
		t.Fatalf("file = %q, want %q", got, "payload")
	}
}

// This is the failure the whole package exists for: two downloads finishing at
// the same instant must not both be told to write "name (2).txt". Run with
// -race. If this fails, one of the two files is silently lost.
func TestReserveGivesConcurrentCallersDistinctNames(t *testing.T) {
	const callers = 32
	target := filepath.Join(t.TempDir(), "clash.tar.gz")

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		paths []string
	)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := Reserve(target, Rename)
			if err != nil {
				mu.Lock()
				defer mu.Unlock()
				t.Errorf("reserve: %v", err)
				return
			}
			// Write through the handle before releasing it: a name that is only
			// reserved but never written would hide a reservation that does not
			// actually hold the file.
			if _, err := fmt.Fprintf(res.File, "caller %d", i); err != nil {
				t.Errorf("write: %v", err)
			}
			res.File.Close()
			mu.Lock()
			defer mu.Unlock()
			paths = append(paths, res.Path)
		}(i)
	}
	wg.Wait()

	if len(paths) != callers {
		t.Fatalf("got %d results, want %d", len(paths), callers)
	}
	seen := make(map[string]bool, callers)
	for _, p := range paths {
		if seen[p] {
			t.Fatalf("two callers were given the same path %q", p)
		}
		seen[p] = true
		if !exists(t, p) {
			t.Fatalf("%s does not exist, so it was never really reserved", p)
		}
		if base := filepath.Base(p); !strings.HasSuffix(base, ".tar.gz") {
			t.Fatalf("base %q lost its .tar.gz extension", base)
		}
	}
	if !seen[target] {
		t.Fatalf("nobody got the plain target %q", target)
	}
	// Every distinct path must be a distinct file on disk, not one file under
	// two names.
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != callers {
		t.Fatalf("directory holds %d files, want %d", len(entries), callers)
	}
}

// If this fails, a folder that somehow collects thousands of copies makes the
// rename loop spin instead of reporting a problem anybody can act on.
func TestReserveStopsCountingAtTheCap(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "file.txt")
	write(t, target, "x")
	write(t, filepath.Join(dir, "file (2).txt"), "x")
	write(t, filepath.Join(dir, "file (3).txt"), "x")

	res, err := Options{MaxAttempts: 3}.Reserve(target, Rename)
	if !errors.Is(err, ErrNoFreeName) {
		t.Fatalf("error = %v, want ErrNoFreeName", err)
	}
	if res.File != nil {
		t.Fatal("a file was reserved although no name was free")
	}
	if !strings.Contains(err.Error(), target) {
		t.Fatalf("error %q does not name the target, so nobody can act on it", err)
	}
	// The cap must not have been worked around by writing a fourth name.
	if exists(t, filepath.Join(dir, "file (4).txt")) {
		t.Fatal("the cap was exceeded")
	}
}

// If this fails, a download to a folder that does not exist yet fails outright
// instead of creating it, which is what internal/app does for download folders.
func TestReserveCreatesTheMissingParent(t *testing.T) {
	target := filepath.Join(t.TempDir(), "series", "season 1", "ep.mkv")
	res, err := Reserve(target, Rename)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	defer res.File.Close()
	if res.Action != Created || res.Path != target {
		t.Fatalf("Action = %q, Path = %q; want %q and the target", res.Action, res.Path, Created)
	}
	if !exists(t, target) {
		t.Fatal("the parent directory was not created")
	}
}

// A directory sitting on the target name is not a free name, however the
// platform words the failure. If this fails, extraction folders next to the
// downloads turn ordinary renames into hard errors on Windows.
func TestReserveTreatsADirectoryAsTaken(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "file.txt")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := Reserve(target, Rename)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	defer res.File.Close()
	if res.Action != Renamed || filepath.Base(res.Path) != "file (2).txt" {
		t.Fatalf("Action = %q, Path = %q; want a rename to file (2).txt", res.Action, res.Path)
	}
}

// The same trap through the seam, so it is covered on every platform: an
// exclusive create that fails with something other than EEXIST while the name
// clearly exists is still a collision, not a reason to give up.
func TestReserveKeepsGoingWhenAClaimFailsOnAnExistingName(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "file.txt")
	blocked := target
	o := Options{
		Open: func(name string, flag int, perm fs.FileMode) (*os.File, error) {
			if name == blocked {
				return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
			}
			return os.OpenFile(name, flag, perm)
		},
		Stat: func(name string) (fs.FileInfo, error) {
			if name == blocked {
				return info(t), nil
			}
			return os.Stat(name)
		},
	}
	res, err := o.Reserve(target, Rename)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	defer res.File.Close()
	if res.Action != Renamed || filepath.Base(res.Path) != "file (2).txt" {
		t.Fatalf("Action = %q, Path = %q; want a rename to file (2).txt", res.Action, res.Path)
	}
}

// A genuine filesystem failure must surface as itself. If this fails, a
// read-only download folder looks like a naming problem and the rename loop
// hammers it a thousand times first.
func TestReserveReportsARealFailureImmediately(t *testing.T) {
	target := filepath.Join(t.TempDir(), "file.txt")
	var attempts int
	o := Options{
		Open: func(name string, flag int, perm fs.FileMode) (*os.File, error) {
			attempts++
			return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
		},
		Stat: func(name string) (fs.FileInfo, error) {
			return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
		},
	}
	if _, err := o.Reserve(target, Rename); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("error = %v, want a permission error", err)
	}
	if attempts != 1 {
		t.Fatalf("tried %d names, want 1: a permission error does not get better with a counter", attempts)
	}
}

// If this fails, a download whose name is already at the filesystem's limit
// cannot be renamed at all and reports "file name too long" instead - or it is
// renamed to something that has nothing left of the name the user asked for.
func TestReserveClipsAnOverlongCandidate(t *testing.T) {
	dir := t.TempDir()
	stem := strings.Repeat("a", maxBaseName-len(".txt"))
	target := filepath.Join(dir, stem+".txt")
	write(t, target, "x")

	res, err := Reserve(target, Rename)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	defer res.File.Close()
	base := filepath.Base(res.Path)
	if len(base) > maxBaseName {
		t.Fatalf("candidate is %d bytes, want at most %d", len(base), maxBaseName)
	}
	if !strings.HasSuffix(base, " (2).txt") {
		t.Fatalf("base = %q, want it to end in the counter and the extension", base)
	}
}

// counted is tested directly because the interesting names cannot be written on
// every platform: Windows refuses a path holding a byte that is not valid UTF-8
// and counts its 255 in UTF-16 units, so a round trip through the filesystem
// would only ever cover the ASCII case. If this fails, a download keeps its
// name on one host and loses it on another.
func TestCountedFitsTheNameLimitWithoutLosingTheName(t *testing.T) {
	cases := []struct {
		name string
		stem string
		ext  string
	}{
		{"short name is untouched", "file", ".txt"},
		{"ascii at the limit", strings.Repeat("a", maxBaseName), ".txt"},
		{"multi-byte runes", strings.Repeat("ä", maxBaseName), ".mkv"},
		// A name carrying bytes from a legacy encoding is not valid UTF-8 at
		// any length. The clip must still keep what fits; shortening until the
		// whole string parses throws away everything from the bad byte on.
		{"not utf-8 at all", "caf\xe9-" + strings.Repeat("b", maxBaseName), ".txt"},
		{"double extension", strings.Repeat("a", maxBaseName), ".tar.gz"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, n := range []int{2, 9, 10, 100, DefaultMaxAttempts} {
				got := counted(c.stem, c.ext, n)
				if len(got) > maxBaseName {
					t.Fatalf("counted(n=%d) is %d bytes, want at most %d: %q", n, len(got), maxBaseName, got)
				}
				if !strings.HasSuffix(got, fmt.Sprintf(" (%d)%s", n, c.ext)) {
					t.Fatalf("counted(n=%d) = %q, want it to end in the counter and %q", n, got, c.ext)
				}
				// Whatever the budget allowed must still be the front of the
				// name the user asked for, not some shorter remnant of it.
				kept := strings.TrimSuffix(got, fmt.Sprintf(" (%d)%s", n, c.ext))
				if !strings.HasPrefix(c.stem, kept) {
					t.Fatalf("counted(n=%d) kept %q, which is not a prefix of %q", n, kept, c.stem)
				}
				if want := min(len(c.stem), maxBaseName-len(fmt.Sprintf(" (%d)", n))-len(c.ext)); len(kept) < want-(utf8.UTFMax-1) {
					t.Fatalf("counted(n=%d) kept only %d of %d usable bytes, so the name was thrown away: %q", n, len(kept), want, kept)
				}
			}
		})
	}
}

// clipBytes is where an overlong name is actually cut, and the case it exists
// for is the one no round trip through the filesystem exercises: a name that is
// not valid UTF-8. If this fails, such a download is renamed to " (2).txt" and
// nothing identifies which file it was.
func TestClipBytesDropsOnlyTheRuneTheCutLandedIn(t *testing.T) {
	cases := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"shorter than the budget", "abc", 8, "abc"},
		{"exactly the budget", "abc", 3, "abc"},
		{"ascii cut", "abcdef", 3, "abc"},
		{"cut on a rune boundary", "aä", 3, "aä"},
		{"cut inside a rune drops that rune whole", "aäb", 2, "a"},
		{"cut inside a four byte rune", "a😀b", 3, "a"},
		// The regression: a bad byte before the cut must not take the rest of
		// the name with it.
		{"bad byte before the cut", "caf\xe9aaaaaaaa", 8, "caf\xe9aaaa"},
		{"bad byte first", "\xe9nzo", 3, "\xe9nz"},
		{"nothing but continuation bytes", "\x80\x80\x80\x80", 3, "\x80\x80\x80"},
		{"no budget", "abc", 0, ""},
		{"negative budget", "abc", -1, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := clipBytes(c.in, c.n)
			if got != c.want {
				t.Fatalf("clipBytes(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
			}
			if len(got) > c.n && c.n > 0 {
				t.Fatalf("clipBytes(%q, %d) returned %d bytes", c.in, c.n, len(got))
			}
		})
	}
}

// The double extension through Reserve rather than through splitName alone. If
// this fails, "archive.tar.gz" lands on disk as "archive.tar (2).gz", which no
// unpacker opens.
func TestReserveKeepsTheExtensionWhenItCountsUp(t *testing.T) {
	cases := []struct {
		base string
		want string
	}{
		{"archive.tar.gz", "archive (2).tar.gz"},
		{"archive.tar.zst", "archive (2).tar.zst"},
		{"Show.S01E02.1080p.mkv", "Show.S01E02.1080p (2).mkv"},
		{"no-extension", "no-extension (2)"},
		{".gitignore", ".gitignore (2)"},
	}
	for _, c := range cases {
		t.Run(c.base, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, c.base)
			write(t, target, "already here")

			res, err := Reserve(target, Rename)
			if err != nil {
				t.Fatalf("reserve: %v", err)
			}
			defer res.File.Close()
			if res.Action != Renamed {
				t.Fatalf("Action = %q, want %q", res.Action, Renamed)
			}
			if got := filepath.Base(res.Path); got != c.want {
				t.Fatalf("Path = %q, want base %q", res.Path, c.want)
			}
		})
	}
}

// If this fails, a failed dispatch leaves an empty file behind and the retry
// renames itself out of the way of its own leftovers - or, worse, Release eats
// a partial download.
func TestReleaseRemovesOnlyTheEmptyFileItCreated(t *testing.T) {
	cases := []struct {
		name       string
		policy     Policy
		existing   string // content placed at the target first, "" for none
		write      string // bytes written through the handle before Release
		wantExists bool
		wantBody   string
	}{
		{name: "created and untouched", policy: Rename, wantExists: false},
		{name: "created and written", policy: Rename, write: "partial", wantExists: true, wantBody: "partial"},
		{name: "renamed and untouched", policy: Rename, existing: "old", wantExists: false},
		// The file existed before we got there, so it is not ours to delete even
		// though truncating already emptied it.
		{name: "overwritten", policy: Overwrite, existing: "old", wantExists: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "file.txt")
			if c.existing != "" {
				write(t, target, c.existing)
			}
			res, err := Reserve(target, c.policy)
			if err != nil {
				t.Fatalf("reserve: %v", err)
			}
			if c.write != "" {
				if _, err := res.File.WriteString(c.write); err != nil {
					t.Fatal(err)
				}
			}
			if err := res.Release(); err != nil {
				t.Fatalf("release: %v", err)
			}
			if got := exists(t, res.Path); got != c.wantExists {
				t.Fatalf("exists(%s) = %v, want %v", res.Path, got, c.wantExists)
			}
			if c.wantExists && c.wantBody != "" && read(t, res.Path) != c.wantBody {
				t.Fatalf("body = %q, want %q", read(t, res.Path), c.wantBody)
			}
			if c.existing != "" && c.policy == Rename && read(t, target) != c.existing {
				t.Fatalf("the file that was already there changed to %q", read(t, target))
			}
		})
	}
}

// Release must be safe on a result that never reserved anything.
func TestReleaseOnASkippedResultDoesNothing(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "file.txt")
	write(t, target, "keep me")
	res, err := Reserve(target, Skip)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := res.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if read(t, target) != "keep me" {
		t.Fatal("Release deleted a file it never reserved")
	}
}

// If this fails, asking about a collision changes the folder it was asked about.
func TestCheckReportsWithoutActing(t *testing.T) {
	dir := t.TempDir()
	taken := filepath.Join(dir, "taken.txt")
	free := filepath.Join(dir, "free.txt")
	write(t, taken, "x")

	if got, err := Check(taken); err != nil || !got {
		t.Fatalf("Check(taken) = %v, %v; want true, nil", got, err)
	}
	if got, err := Check(free); err != nil || got {
		t.Fatalf("Check(free) = %v, %v; want false, nil", got, err)
	}
	if exists(t, free) {
		t.Fatal("Check created the file it was asked about")
	}
	if _, err := Check("  "); err == nil {
		t.Fatal("Check accepted an empty path")
	}
}

// A stat failure that is not "not found" is not an answer, and reporting it as
// "no collision" would hand the caller a name it never verified.
func TestCheckReportsAStatFailure(t *testing.T) {
	o := Options{
		Stat: func(name string) (fs.FileInfo, error) {
			return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrPermission}
		},
	}
	if _, err := o.Check(filepath.Join(t.TempDir(), "file.txt")); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("error = %v, want a permission error", err)
	}
}

// A policy string that never went through ParsePolicy is a bug in the caller.
// If this fails, that bug silently picks a behaviour that can delete files.
func TestReserveRejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "file.txt")

	if _, err := Reserve(target, Policy("overwirte")); !errors.Is(err, ErrUnknownPolicy) {
		t.Fatalf("error = %v, want ErrUnknownPolicy", err)
	}
	if exists(t, target) {
		t.Fatal("an unknown policy still created the file")
	}
	if _, err := Reserve("   ", Rename); err == nil {
		t.Fatal("an empty target was accepted")
	}
}

// If this fails, a hand-edited settings.json can leave the server unable to
// download, or can turn a typo into overwriting finished files.
func TestParsePolicyFoldsUnknownInputOntoTheSafeDefault(t *testing.T) {
	cases := []struct {
		in   string
		want Policy
	}{
		{"overwrite", Overwrite},
		{"  SKIP ", Skip},
		{"Rename", Rename},
		{"ask", Ask},
		{"", DefaultPolicy},
		{"overwirte", DefaultPolicy},
		{"delete everything", DefaultPolicy},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := ParsePolicy(c.in); got != c.want {
				t.Fatalf("ParsePolicy(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
	if DefaultPolicy == Overwrite {
		t.Fatal("the fallback for unreadable input must never be the policy that deletes files")
	}
	// Everything offered in the UI must be something Reserve accepts.
	for _, p := range Policies() {
		if ParsePolicy(string(p)) != p {
			t.Fatalf("policy %q is offered but does not round-trip through ParsePolicy", p)
		}
	}
}

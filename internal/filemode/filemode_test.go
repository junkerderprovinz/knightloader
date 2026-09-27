package filemode_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/filemode"
)

func TestParseReadsTheSpellingsLinuxserverImagesAccept(t *testing.T) {
	for in, want := range map[string]fs.FileMode{
		"000":   0,
		"0":     0,
		"002":   0o002,
		"022":   0o022,
		"0022":  0o022,
		"0027":  0o027,
		"777":   0o777,
		" 002 ": 0o002,
	} {
		got, err := filemode.Parse(in)
		if err != nil {
			t.Errorf("Parse(%q) refused a valid mask: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("Parse(%q) = %04o, want %04o", in, got, want)
		}
	}
}

func TestParseRefusesWhatIsNotAnOctalMask(t *testing.T) {
	for _, in := range []string{"", " ", "abc", "8", "0o22", "-1", "+2", "1000", "0x12", "u=rwx"} {
		if got, err := filemode.Parse(in); err == nil {
			t.Errorf("Parse(%q) = %04o, want an error", in, got)
		}
	}
}

func TestAnEmptyUmaskChangesNothing(t *testing.T) {
	before, applied := filemode.Mask(), filemode.Applied()
	if err := filemode.Apply(""); err != nil {
		t.Fatalf("an unset UMASK was refused: %v", err)
	}
	if filemode.Applied() != applied {
		t.Error("an unset UMASK changed whether a UMASK counts as applied")
	}
	if got := filemode.Mask(); got != before {
		t.Errorf("an unset UMASK changed the mask from %04o to %04o", before, got)
	}
}

func TestAnUnreadableUmaskIsRefusedAndChangesNothing(t *testing.T) {
	before := filemode.Mask()
	if err := filemode.Apply("rw-rw-rw-"); err == nil {
		t.Error("a UMASK that is no octal mask was accepted")
	}
	if got := filemode.Mask(); got != before {
		t.Errorf("a refused UMASK changed the mask from %04o to %04o", before, got)
	}
}

// TestDownloadsUnderUmask000ComeOutWritableForEveryone is the Unraid case: an
// SMB account other than the container's has to move and delete what lands on
// the share.
func TestDownloadsUnderUmask000ComeOutWritableForEveryone(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the modes are a Linux container's; other systems have no umask or measure it differently")
	}
	was := filemode.Mask()
	if err := filemode.Apply("000"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := filemode.Apply(fmt.Sprintf("%03o", was)); err != nil {
			t.Errorf("the umask could not be put back to %04o: %v", was, err)
		}
	})
	if !filemode.Applied() || filemode.Mask() != 0 {
		t.Fatalf("UMASK=000 left the mask at %04o (applied %v)", filemode.Mask(), filemode.Applied())
	}

	base := t.TempDir()
	r, err := collide.Reserve(filepath.Join(base, "Show.S01", "ep01.mkv"), collide.Rename)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.File.Close(); err != nil {
		t.Fatal(err)
	}
	wantMode(t, r.Path, 0o666)
	wantMode(t, filepath.Dir(r.Path), 0o777)

	// What the torrent library leaves behind: fixed modes and a finished file
	// made read-only.
	lib := filepath.Join(base, "Movie")
	if err := os.Mkdir(lib, 0o755); err != nil {
		t.Fatal(err)
	}
	done := filepath.Join(lib, "movie.mkv")
	if err := os.WriteFile(done, []byte("x"), 0o444); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{done, lib} {
		if err := filemode.Settle(p); err != nil {
			t.Fatal(err)
		}
	}
	wantMode(t, done, 0o666)
	wantMode(t, lib, 0o777)
}

func wantMode(t *testing.T, path string, want fs.FileMode) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != want {
		t.Errorf("%s came out %04o, want %04o", filepath.Base(path), got, want)
	}
}

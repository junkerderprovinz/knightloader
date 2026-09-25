package watch

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAFileThatIsNotALinkListIsHandedOverWhole(t *testing.T) {
	for _, name := range []string{"Season 3.torrent", "links.dlc", "links.CCF", "links.rsdf", "Show.S01E01.nzb"} {
		jobs, err := Parse(name, strings.NewReader("the bytes as they are"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		j := only(t, jobs)
		if j.File == nil || j.File.Name != name || string(j.File.Data) != "the bytes as they are" {
			t.Errorf("%s: job = %+v, want the file handed over whole", name, j)
		}
		if want := strings.TrimSuffix(name, filepath.Ext(name)); j.Package != want {
			t.Errorf("%s: package = %q, want %q", name, j.Package, want)
		}
		if len(j.URLs) != 0 {
			t.Errorf("%s: a whole file also carries links %v", name, j.URLs)
		}
	}
}

func TestAnEmptyWholeFileIsRefused(t *testing.T) {
	if _, err := Parse("empty.torrent", strings.NewReader("")); err == nil {
		t.Error("an empty .torrent parsed")
	}
}

func TestAMagnetFileBecomesALinkJob(t *testing.T) {
	jobs, err := Parse("Linux ISOs.magnet",
		strings.NewReader("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=one\n"+
			"magnet:?xt=urn:btih:fedcba9876543210fedcba9876543210fedcba98&dn=two\n"))
	if err != nil {
		t.Fatal(err)
	}
	j := only(t, jobs)
	if len(j.URLs) != 2 || j.Package != "Linux ISOs" || j.File != nil {
		t.Fatalf("job = %+v, want both magnets in one link job", j)
	}
}

func TestEveryNewTypeIsPickedUpAndRetired(t *testing.T) {
	p, dir, rec := newPolled(t, false)
	names := []string{"a.torrent", "b.magnet", "c.dlc", "d.ccf", "e.rsdf", "f.nzb"}
	for _, name := range names {
		content := "payload"
		if strings.HasSuffix(name, ".magnet") {
			content = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"
		}
		write(t, filepath.Join(dir, name), content)
	}

	p.poll()
	p.poll()
	if n := rec.count(); n != len(names) {
		t.Fatalf("handed over %d jobs, want one per file", n)
	}
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(dir, name+".done")); err != nil {
			t.Errorf("%s was not retired: %v", name, err)
		}
	}
}

func TestARefusedFileIsTakenOnceSomethingCanOpenIt(t *testing.T) {
	dir := t.TempDir()
	rec := &sink{}
	var open atomic.Bool
	w, err := New(Options{
		Folders:  []Folder{{Dir: dir}},
		Interval: time.Hour,
		OnJob:    rec.add,
		Check: func(Job) error {
			if !open.Load() {
				return errors.New("no account can fetch an .nzb")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	p := (*w.byDir.Load())[dir]
	write(t, filepath.Join(dir, "Show.nzb"), "nzb bytes")

	p.poll()
	p.poll()
	open.Store(true)
	p.poll()
	if n := rec.count(); n != 0 {
		t.Fatalf("handed over %d jobs from a file already found unusable, before anything said to look again", n)
	}

	w.Retry()
	p.poll()
	if n := rec.count(); n != 1 {
		t.Fatalf("handed over %d jobs after Retry, want the dropped file", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "Show.nzb.done")); err != nil {
		t.Errorf("the file was not retired: %v", err)
	}
}

func TestAnNZBBeyondTheListCapIsStillRead(t *testing.T) {
	big := strings.Repeat("x", maxIntakeSize+1)
	jobs, err := Parse("Film.2160p.nzb", strings.NewReader(big))
	if err != nil {
		t.Fatalf("an .nzb just over the list cap was refused: %v", err)
	}
	if j := only(t, jobs); j.File == nil || len(j.File.Data) != len(big) {
		t.Error("the .nzb was not handed over whole")
	}
	if _, err := Parse("list.torrent", strings.NewReader(big)); err == nil {
		t.Error("a .torrent over the cap was read")
	}
}

func TestAFileTheCheckRefusesStaysWhereItWasDropped(t *testing.T) {
	p, dir, rec := newPolled(t, false)
	refused := errors.New("no JDownloader to open it")
	p.check = func(j Job) error {
		if j.File != nil && strings.HasSuffix(j.File.Name, ".dlc") {
			return refused
		}
		return nil
	}
	write(t, filepath.Join(dir, "locked.dlc"), "encrypted")
	write(t, filepath.Join(dir, "open.torrent"), "bencode")

	p.poll()
	p.poll()
	p.poll()
	jobs := rec.all()
	if len(jobs) != 1 || jobs[0].File == nil || jobs[0].File.Name != "open.torrent" {
		t.Fatalf("handed over %+v, want only the file the check let through", jobs)
	}
	if _, err := os.Stat(filepath.Join(dir, "locked.dlc")); err != nil {
		t.Errorf("the refused file is gone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "locked.dlc.done")); !os.IsNotExist(err) {
		t.Error("the refused file was retired")
	}
}

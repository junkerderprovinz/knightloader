package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/usenet"
	"github.com/junkerderprovinz/knightloader/internal/watch"
)

const droppedNZB = `<?xml version="1.0" encoding="iso-8859-1" ?>
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
 <file poster="someone" date="1700000000" subject="Show.S01E01 [1/2] yEnc">
  <segments><segment bytes="739067" number="1">abcdef0123456789@news</segment></segments>
 </file>
</nzb>`

// readyService takes every NZB and has it ready with one file at once, or
// refuses every one with refuse.
type readyService struct {
	refuse error

	mu   sync.Mutex
	sent []string
}

func (*readyService) Slot() string        { return "torbox" }
func (*readyService) Label() string       { return "TorBox" }
func (*readyService) SubmitsPerHour() int { return 0 }
func (s *readyService) Submit(_ context.Context, name string, _ []byte) (string, error) {
	if s.refuse != nil {
		return "", s.refuse
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, name)
	return "9", nil
}
func (*readyService) Status(_ context.Context, ids []string) (map[string]usenet.Status, error) {
	out := map[string]usenet.Status{}
	for _, id := range ids {
		out[id] = usenet.Status{Phase: usenet.PhaseReady, Files: []usenet.File{{ID: "1", Name: "show.s01e01.mkv", Size: 700}}}
	}
	return out, nil
}
func (*readyService) Link(context.Context, string, string) (string, error) {
	return "https://cdn.example/show.s01e01.mkv", nil
}
func (*readyService) Delete(context.Context, string) error { return nil }

// containerJD opens every container into one link and records what it was
// given.
type containerJD struct {
	mu   sync.Mutex
	ext  string
	data []byte
}

func (*containerJD) Download(string, string, map[string]string, int) {}
func (*containerJD) Pause(string)                                    {}
func (*containerJD) Resume(string)                                   {}
func (*containerJD) Remove(string, bool)                             {}
func (*containerJD) AddContainer(string, string, time.Duration) ([]resolver.Result, error) {
	return nil, errors.New("a dropped container is not fetched from an address")
}
func (j *containerJD) AddContainerFile(ext string, data []byte, _ string, _ time.Duration) ([]resolver.Result, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.ext, j.data = ext, data
	return []resolver.Result{{DirectURL: "https://host.example/opened.bin", Name: "opened.bin", Size: 5}}, nil
}

func TestADroppedTorrentIsStagedWithEveryFile(t *testing.T) {
	a := newCrawlApp(t, false)
	uri := testTorrentURI(t, "Pack", []metainfo.FileInfo{
		{Length: 900, Path: []string{"one.mkv"}},
		{Length: 12, Path: []string{"two.srt"}},
	})
	data, err := torrent.DecodeBytes(uri)
	if err != nil {
		t.Fatal(err)
	}
	job := watch.Job{File: &watch.File{Name: "Pack.torrent", Data: data}, Package: "Pack"}
	if err := a.checkWatchJob(job); err != nil {
		t.Fatalf("a valid .torrent was refused: %v", err)
	}
	a.stageWatchJob(job)

	tasks := a.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("staged %d tasks, want the torrent", len(tasks))
	}
	got := tasks[0]
	if got.Resolver != "torrent" || got.Origin != OriginWatch || got.Package != "Pack" {
		t.Errorf("task = %s via %s from %s in %q", got.Name, got.Resolver, got.Origin, got.Package)
	}
	for _, f := range got.TorrentFiles {
		if !f.Selected {
			t.Errorf("%s is not selected; a dropped torrent has no one to pick files", f.Path)
		}
	}
}

func TestADamagedTorrentStaysInTheFolder(t *testing.T) {
	a := newCrawlApp(t, false)
	err := a.checkWatchJob(watch.Job{File: &watch.File{Name: "broken.torrent", Data: []byte("not bencode")}})
	if err == nil {
		t.Fatal("a file that is no torrent would be retired")
	}
}

func TestADroppedNZBGoesToTheUsenetAccount(t *testing.T) {
	a := newCrawlApp(t, false)
	svc := &readyService{}
	a.SetUsenetServices(svc)
	job := watch.Job{File: &watch.File{Name: "Show.S01E01.nzb", Data: []byte(droppedNZB)}, Package: "Show.S01E01"}
	if err := a.checkWatchJob(job); err != nil {
		t.Fatalf("an .nzb was refused with an account set up: %v", err)
	}
	a.stageWatchJob(job)

	waitFor(t, "the .nzb's file to be staged", func() bool { return len(a.Tasks()) == 1 })
	got := a.Tasks()[0]
	if got.Resolver != usenet.ResolverID || got.Name != "show.s01e01.mkv" || got.Origin != OriginWatch {
		t.Errorf("task = %s via %s from %s", got.Name, got.Resolver, got.Origin)
	}
	if got.Package != "Show.S01E01" {
		t.Errorf("package = %q, want the file's base name", got.Package)
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.sent) != 1 || svc.sent[0] != "Show.S01E01" {
		t.Errorf("the account got %v, want the one .nzb under its name", svc.sent)
	}
}

func TestADroppedNZBWithoutAnAccountIsReadForLinks(t *testing.T) {
	a := newCrawlApp(t, false)
	links := watch.Job{File: &watch.File{Name: "list.nzb", Data: []byte("<links><a>https://host.example/one.bin</a></links>")}, Package: "list"}
	if err := a.checkWatchJob(links); err != nil {
		t.Fatalf("an .nzb holding a link was refused: %v", err)
	}
	a.stageWatchJob(links)
	if tasks := a.Tasks(); len(tasks) != 1 || tasks[0].URL != "https://host.example/one.bin" {
		t.Fatalf("tasks = %+v, want the link inside", tasks)
	}

	real := watch.Job{File: &watch.File{Name: "Show.nzb", Data: []byte(droppedNZB)}}
	if err := a.checkWatchJob(real); !errors.Is(err, ErrNZBNeedsUsenet) {
		t.Errorf("a real .nzb with no account answered %v, want it refused with the reason", err)
	}
}

func TestADroppedEncryptedContainerIsRefusedWithoutJDownloader(t *testing.T) {
	t.Setenv("KL_JD", "")
	a := newCrawlApp(t, false)
	err := a.checkWatchJob(watch.Job{File: &watch.File{Name: "links.ccf", Data: []byte("encrypted bytes")}})
	if !errors.Is(err, ErrNoContainerBackend) {
		t.Errorf("err = %v, want the missing backend named", err)
	}
}

func TestADroppedEncryptedContainerIsOpenedByJDownloader(t *testing.T) {
	a := newCrawlApp(t, false)
	jd := &containerJD{}
	a.bmu.Lock()
	a.jd = jd
	a.bmu.Unlock()

	job := watch.Job{File: &watch.File{Name: "Links.CCF", Data: []byte("encrypted bytes")}, Package: "Links"}
	if err := a.checkWatchJob(job); err != nil {
		t.Fatalf("a container JD can open was refused: %v", err)
	}
	a.stageWatchJob(job)

	jd.mu.Lock()
	ext, data := jd.ext, string(jd.data)
	jd.mu.Unlock()
	if ext != "ccf" || data != "encrypted bytes" {
		t.Errorf("JD got %q as %q, want the dropped bytes as ccf", data, ext)
	}
	tasks := a.Tasks()
	if len(tasks) != 1 || tasks[0].Origin != OriginWatch || !strings.HasSuffix(tasks[0].URL, "opened.bin") {
		t.Fatalf("tasks = %+v, want the link JD found, from the watched folder", tasks)
	}
}

func TestADroppedPlainContainerIsReadHere(t *testing.T) {
	a := newCrawlApp(t, false)
	job := watch.Job{File: &watch.File{Name: "list.dlc", Data: []byte("https://host.example/one.bin\n")}, Package: "list"}
	if err := a.checkWatchJob(job); err != nil {
		t.Fatalf("a link list named .dlc was refused: %v", err)
	}
	a.stageWatchJob(job)
	if tasks := a.Tasks(); len(tasks) != 1 {
		t.Fatalf("staged %d tasks, want the link in the list", len(tasks))
	}
}

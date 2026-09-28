package app

// A torrent a debrid service fetched, handed to the built-in client to seed:
// it seeds from the files the service delivered, and one whose files are not
// all there is left alone with the reason on its row.

import (
	"bytes"
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/testenv"
)

// packTorrent builds a torrent of three files and returns it as the link the
// app carries, with each file's content by its path inside the torrent.
func packTorrent(t *testing.T) (string, map[string][]byte) {
	t.Helper()
	files := map[string][]byte{
		"a.bin": bytes.Repeat([]byte{'a'}, 40<<10),
		"b.bin": bytes.Repeat([]byte{'b'}, 24<<10),
		"c.bin": bytes.Repeat([]byte{'c'}, 16<<10),
	}
	src := filepath.Join(t.TempDir(), "Pack")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(src, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	info := metainfo.Info{PieceLength: 16 << 10}
	if err := info.BuildFromFilePath(src); err != nil {
		t.Fatal(err)
	}
	ib, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	b, err := bencode.Marshal(metainfo.MetaInfo{InfoBytes: ib})
	if err != nil {
		t.Fatal(err)
	}
	return torrent.EncodeBytes(b), files
}

// packService is a debrid service that has the torrent cached and hands out
// the files it holds, which may be fewer than the torrent has.
type packService struct {
	origin string
	held   map[string][]byte
}

func (s *packService) ID() string    { return "fakedebrid" }
func (s *packService) Label() string { return "Fake debrid" }
func (s *packService) AddTorrent(context.Context, debrid.TorrentSource) (string, bool, error) {
	return "J1", false, nil
}
func (s *packService) TorrentStatus(context.Context, string) (debrid.TorrentJob, error) {
	job := debrid.TorrentJob{Name: "Pack", State: debrid.TorrentReady}
	for _, name := range []string{"a.bin", "b.bin", "c.bin"} {
		if data, ok := s.held[name]; ok {
			job.Files = append(job.Files, debrid.TorrentFile{ID: name, Path: "Pack/" + name, Size: int64(len(data)), Held: true})
			job.Size += int64(len(data))
		}
	}
	return job, nil
}
func (s *packService) FileURL(_ context.Context, _ string, f debrid.TorrentFile) (debrid.Direct, error) {
	return debrid.Direct{URL: s.origin + "/" + f.ID}, nil
}
func (s *packService) DeleteTorrent(context.Context, string) error { return nil }

// seedAfterDebridApp has a debrid service that takes torrents first, with the
// switch that hands what it fetched to the built-in client set to seed.
func seedAfterDebridApp(t *testing.T, seed bool, held map[string][]byte) *App {
	t.Helper()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(held[filepath.Base(r.URL.Path)]))
	}))
	t.Cleanup(origin.Close)
	a := newQueueApp(t)
	s := settings.Defaults()
	s.DownloadDir = t.TempDir()
	s.Crawl = false
	s.Torrent.SeedAfterDebrid = seed
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	a.bmu.Lock()
	a.debrid["fakedebrid"] = a.torrentsVia("fakedebrid", &packService{origin: origin.URL, held: held},
		&routeSpy{name: "links", events: make(chan string, 4)}, engineHandoff{a.Engine, a})
	a.bmu.Unlock()
	a.Registry.Register(debrid.Resolver{ServiceID: "fakedebrid", Prio: 90, Torrents: true})
	return a
}

func TestATorrentADebridServiceFetchedSeedsFromItsFiles(t *testing.T) {
	testenv.RequireWideListener(t)
	if raceEnabled {
		// See TestAStalledDownloadIsReconnectedAndKeepsItsBytes.
		t.Skip("gopeed v1.9.3 has internal data races in every real HTTP transfer")
	}
	link, files := packTorrent(t)
	a := seedAfterDebridApp(t, true, files)
	dir := t.TempDir()

	queueTorrent(a, &core.Task{ID: "p1", URL: link, Dir: dir, Status: core.StatusQueued, Enabled: true})
	waitFor(t, "the torrent to seed from the files the service delivered", func() bool {
		return liveTask(a, "p1").Seeding
	})

	got := liveTask(a, "p1")
	if got.Resolver != "torrent" || !samePath(got.File, filepath.Join(dir, "Pack")) {
		t.Errorf("seeding on %q from %s, want the built-in client on the torrent's folder", got.Resolver, got.File)
	}
	if got.Status != core.StatusDone || got.SeedingOver {
		t.Errorf("the task is %s, seeding over %v; a seeding torrent is done and owes more", got.Status, got.SeedingOver)
	}
	for name, want := range files {
		if b, err := os.ReadFile(filepath.Join(dir, "Pack", name)); err != nil || !bytes.Equal(b, want) {
			t.Errorf("%s holds %d bytes after the hand-off, %v", name, len(b), err)
		}
	}
}

// The service delivered a file short of the torrent's, or one that differs
// from it. Seeding either would fetch from the swarm, so the torrent is not
// seeded, the row says why, and the files stay as the service delivered them.
func TestATorrentWhoseFilesAreNotAsTheTorrentHasThemIsNotSeeded(t *testing.T) {
	if raceEnabled {
		t.Skip("gopeed v1.9.3 has internal data races in every real HTTP transfer")
	}
	for _, c := range []struct {
		name  string
		spoil func(held map[string][]byte)
		want  string
	}{
		{"one missing", func(held map[string][]byte) { delete(held, "c.bin") }, "c.bin is missing"},
		{"one damaged", func(held map[string][]byte) {
			b := bytes.Clone(held["b.bin"])
			b[100] = 'x'
			held["b.bin"] = b
		}, "b.bin does not match the torrent"},
	} {
		t.Run(c.name, func(t *testing.T) {
			link, files := packTorrent(t)
			held := maps.Clone(files)
			c.spoil(held)
			a := seedAfterDebridApp(t, true, held)
			dir := t.TempDir()

			queueTorrent(a, &core.Task{ID: "p1", URL: link, Dir: dir, Status: core.StatusQueued, Enabled: true})
			waitFor(t, "the reason the torrent is not seeded", func() bool {
				return strings.HasPrefix(liveTask(a, "p1").Note, "Not seeded: ")
			})

			got := liveTask(a, "p1")
			if !strings.Contains(got.Note, c.want) {
				t.Errorf("the row says %q, want it to say %q", got.Note, c.want)
			}
			if got.Seeding || !got.SeedingOver || got.Status != core.StatusDone {
				t.Errorf("the task is %s, seeding %v, over %v; want it done and not owing any seeding", got.Status, got.Seeding, got.SeedingOver)
			}
			for name := range files {
				b, err := os.ReadFile(filepath.Join(dir, "Pack", name))
				want, delivered := held[name]
				switch {
				case !delivered && err == nil:
					t.Errorf("%s, which the service did not deliver, was fetched from the swarm", name)
				case delivered && !bytes.Equal(b, want):
					t.Errorf("%s changed after the service delivered it (%v)", name, err)
				}
			}
		})
	}
}

// doneOnService is a torrent the debrid service has fetched and delivered,
// still on the service's backend.
func doneOnService(t *testing.T, a *App, files []core.TorrentFile) {
	t.Helper()
	link, _ := packTorrent(t)
	putTask(t, a, core.Task{ID: "p1", URL: link, Name: "Pack", Resolver: "fakedebrid", Dir: t.TempDir(),
		Status: core.StatusDone, Enabled: true, TorrentFiles: files})
}

func TestWithoutTheSwitchADebridTorrentStaysWhereItWasFetched(t *testing.T) {
	a := seedAfterDebridApp(t, false, nil)
	doneOnService(t, a, nil)

	a.seedFromService("p1")

	if got := liveTask(a, "p1"); got.Resolver != "fakedebrid" || got.Note != "" {
		t.Errorf("the task went to %q with the note %q, want it left with the service", got.Resolver, got.Note)
	}
}

// Only some of the torrent's files were chosen, so the service fetched only
// those, and the built-in client would fetch the pieces they share with the
// others.
func TestADebridTorrentFetchedInPartIsNotSeeded(t *testing.T) {
	a := seedAfterDebridApp(t, true, nil)
	doneOnService(t, a, []core.TorrentFile{{Path: "a.bin", Size: 40 << 10, Selected: true}, {Path: "b.bin", Size: 24 << 10}})

	a.seedFromService("p1")

	got := liveTask(a, "p1")
	if got.Resolver != "fakedebrid" {
		t.Errorf("the task went to %q, want it left with the service", got.Resolver)
	}
	if !strings.HasPrefix(got.Note, "Not seeded: only some of its files") {
		t.Errorf("the row says %q, want the reason it is not seeded", got.Note)
	}
}

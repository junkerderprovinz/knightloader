package app

// A torrent a debrid service fetched, handed to an external qBittorrent to
// seed: KnightLoader checks the files, logs in and adds the torrent where its
// files are, and a torrent it cannot hand over is left with the reason.

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/testenv"
)

// fakeQBittorrent is a qBittorrent Web UI that takes admin/secret and records
// every torrent it is asked to add.
type fakeQBittorrent struct {
	*httptest.Server
	mu     sync.Mutex
	logins int
	adds   []qbitAdd
}

// qbitAdd is one torrents/add request: its fields, the .torrent it carried and
// the session cookie it came with.
type qbitAdd struct {
	fields  map[string]string
	torrent []byte
	session string
}

func newFakeQBittorrent(t *testing.T) *fakeQBittorrent {
	t.Helper()
	f := &fakeQBittorrent{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v2/auth/login", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.logins++
		f.mu.Unlock()
		if r.PostFormValue("username") != "admin" || r.PostFormValue("password") != "secret" {
			io.WriteString(w, "Fails.")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: "s1", Path: "/"})
		io.WriteString(w, "Ok.")
	})
	mux.HandleFunc("POST /api/v2/torrents/add", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		add := qbitAdd{fields: map[string]string{}}
		if c, err := r.Cookie("SID"); err == nil {
			add.session = c.Value
		}
		for k, v := range r.MultipartForm.Value {
			add.fields[k] = v[0]
		}
		if fh := r.MultipartForm.File["torrents"]; len(fh) > 0 {
			file, err := fh[0].Open()
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			add.torrent, _ = io.ReadAll(file)
			file.Close()
		}
		f.mu.Lock()
		f.adds = append(f.adds, add)
		f.mu.Unlock()
		if add.session != "s1" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		io.WriteString(w, "Ok.")
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func (f *fakeQBittorrent) seen() (int, []qbitAdd) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.logins, f.adds
}

// qbitApp seeds what the debrid service fetched in qb, which mounts the
// download folder as /data/downloads, with password as the one stored. The
// built-in client is switched off, which leaves qBittorrent's way open.
func qbitApp(t *testing.T, qb *fakeQBittorrent, password string) *App {
	t.Helper()
	a := seedAfterDebridApp(t, true, nil)
	s := a.Settings.Get()
	s.ModulesOff = []string{"torrents"}
	s.Torrent.SeedIn = settings.SeedInQBittorrent
	s.Torrent.QBittorrent = settings.QBittorrent{
		URL:           qb.URL,
		Username:      "admin",
		Password:      password,
		Category:      "cross-seed",
		DownloadsPath: "/data/downloads",
	}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	return a
}

// fetchedPack is the torrent of packTorrent, done on the service with its
// files written to the tv folder under the download folder, less those named
// in missing.
func fetchedPack(t *testing.T, a *App, link string, files map[string][]byte, missing ...string) string {
	t.Helper()
	dir := filepath.Join(a.Settings.Get().DownloadDir, "tv")
	if err := os.MkdirAll(filepath.Join(dir, "Pack"), 0o755); err != nil {
		t.Fatal(err)
	}
	var list []core.TorrentFile
	for name, data := range files {
		list = append(list, core.TorrentFile{Path: name, Size: int64(len(data)), Selected: true})
		if slices.Contains(missing, name) {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, "Pack", name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	putTask(t, a, core.Task{ID: "p1", URL: link, Name: "Pack", Resolver: "fakedebrid", Dir: dir,
		Status: core.StatusDone, Enabled: true, TorrentFiles: list})
	return dir
}

func TestAFetchedTorrentGoesToQBittorrentWithItsFolder(t *testing.T) {
	qb := newFakeQBittorrent(t)
	a := qbitApp(t, qb, "secret")
	link, files := packTorrent(t)
	fetchedPack(t, a, link, files)

	a.seedFromService("p1")

	logins, adds := qb.seen()
	if logins != 1 || len(adds) != 1 {
		t.Fatalf("qBittorrent saw %d logins and %d adds, want one of each", logins, len(adds))
	}
	add := adds[0]
	want := map[string]string{
		"savepath":      "/data/downloads/tv",
		"skip_checking": "true",
		"contentLayout": "Original",
		"autoTMM":       "false",
		"category":      "cross-seed",
		"root_folder":   "true",
	}
	for k, v := range want {
		if add.fields[k] != v {
			t.Errorf("%s = %q, want %q", k, add.fields[k], v)
		}
	}
	raw, _ := torrent.DecodeBytes(link)
	if !bytes.Equal(add.torrent, raw) || add.fields["urls"] != "" {
		t.Errorf("the add carried %d bytes of .torrent and urls %q, want the task's .torrent", len(add.torrent), add.fields["urls"])
	}
	if add.session != "s1" {
		t.Errorf("the add came with session %q, want the one the login opened", add.session)
	}
	got := liveTask(a, "p1")
	if got.Note != "Seeding in qBittorrent" || got.Resolver != "fakedebrid" || got.Seeding {
		t.Errorf("the row says %q on %q, seeding here %v; want it seeding in qBittorrent only", got.Note, got.Resolver, got.Seeding)
	}
}

func TestAFetchedMagnetGoesToQBittorrentAsItsLink(t *testing.T) {
	qb := newFakeQBittorrent(t)
	a := qbitApp(t, qb, "secret")
	_, files := packTorrent(t)
	magnet := "magnet:?xt=urn:btih:c12fe1c06bba254a9dc9f519b335aa7c1367a88a&dn=Pack"
	fetchedPack(t, a, magnet, files)

	a.seedFromService("p1")

	_, adds := qb.seen()
	if len(adds) != 1 {
		t.Fatalf("qBittorrent saw %d adds, want one; the row says %q", len(adds), liveTask(a, "p1").Note)
	}
	if adds[0].fields["urls"] != magnet || adds[0].torrent != nil {
		t.Errorf("the add carried urls %q and %d bytes of .torrent, want the magnet link", adds[0].fields["urls"], len(adds[0].torrent))
	}
	if adds[0].fields["savepath"] != "/data/downloads/tv" {
		t.Errorf("savepath = %q, want the task's folder as qBittorrent sees it", adds[0].fields["savepath"])
	}
}

func TestARefusedQBittorrentLoginLeavesTheTorrentUnseeded(t *testing.T) {
	qb := newFakeQBittorrent(t)
	a := qbitApp(t, qb, "wrong")
	link, files := packTorrent(t)
	fetchedPack(t, a, link, files)

	a.seedFromService("p1")

	if _, adds := qb.seen(); len(adds) != 0 {
		t.Errorf("qBittorrent was asked to add %d torrents after refusing the login", len(adds))
	}
	got := liveTask(a, "p1")
	if got.Note != "Not seeded: qBittorrent refused the username or password" {
		t.Errorf("the row says %q, want the refused login", got.Note)
	}
	a.mu.Lock()
	started := a.started["p1"]
	a.mu.Unlock()
	if got.Resolver != "fakedebrid" || got.Seeding || started {
		t.Errorf("the task went to %q, seeding %v; want no seed job in its place", got.Resolver, got.Seeding)
	}
}

func TestATorrentThatFailsTheCheckIsNotHandedToQBittorrent(t *testing.T) {
	qb := newFakeQBittorrent(t)
	a := qbitApp(t, qb, "secret")
	link, files := packTorrent(t)
	fetchedPack(t, a, link, files, "c.bin")

	a.seedFromService("p1")

	if logins, adds := qb.seen(); logins != 0 || len(adds) != 0 {
		t.Errorf("qBittorrent saw %d logins and %d adds for a torrent short of a file", logins, len(adds))
	}
	if got := liveTask(a, "p1").Note; !strings.HasPrefix(got, "Not seeded: ") || !strings.Contains(got, "c.bin is missing") {
		t.Errorf("the row says %q, want the file that is missing", got)
	}
}

// qBittorrent is set up but the built-in client is chosen, which seeds as it
// always has and never calls qBittorrent.
func TestWithTheBuiltInClientChosenQBittorrentIsLeftAlone(t *testing.T) {
	testenv.RequireWideListener(t)
	if raceEnabled {
		t.Skip("gopeed v1.9.3 has internal data races in every real HTTP transfer")
	}
	qb := newFakeQBittorrent(t)
	a := qbitApp(t, qb, "secret")
	s := a.Settings.Get()
	s.ModulesOff = nil
	s.Torrent.SeedIn = settings.SeedInBuiltIn
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	link, files := packTorrent(t)
	dir := fetchedPack(t, a, link, files)

	a.seedFromService("p1")
	waitFor(t, "the torrent to seed in the built-in client", func() bool {
		return liveTask(a, "p1").Seeding
	})

	got := liveTask(a, "p1")
	if got.Resolver != "torrent" || !samePath(got.File, filepath.Join(dir, "Pack")) {
		t.Errorf("seeding on %q from %s, want the built-in client on the torrent's folder", got.Resolver, got.File)
	}
	if logins, adds := qb.seen(); logins != 0 || len(adds) != 0 {
		t.Errorf("qBittorrent saw %d logins and %d adds with the built-in client chosen", logins, len(adds))
	}
}

package crossseed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/bencode"
)

// injections records what a runner hands over, and refuses with refuse when
// it is set.
type injections struct {
	mu     sync.Mutex
	got    []Found
	refuse error
}

func (in *injections) inject(_ context.Context, _ Source, f Found) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.refuse != nil {
		return in.refuse
	}
	in.got = append(in.got, f)
	return nil
}

func (in *injections) refuseWith(err error) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.refuse = err
}

func (in *injections) all() []Found {
	in.mu.Lock()
	defer in.mu.Unlock()
	return append([]Found(nil), in.got...)
}

type rig struct {
	runner *Runner
	in     *injections
	log    *Log
	hist   *History
	// sources are the finished torrents a find of the daemon is matched to.
	sources []Source
}

func startRig(t *testing.T, conf Config) *rig {
	t.Helper()
	dir := t.TempDir()
	r := &rig{in: &injections{}, log: OpenLog(filepath.Join(dir, "cross-seed.jsonl")), hist: OpenHistory(filepath.Join(dir, "asked.json"))}
	r.runner = Start(Options{
		Config:  func() Config { return conf },
		History: r.hist,
		Log:     r.log,
		Inject:  r.in.inject,
		Sources: func() []Source { return r.sources },
	})
	t.Cleanup(r.runner.Close)
	return r
}

func (r *rig) settle(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.runner.Wait(ctx); err != nil {
		t.Fatal("the runner did not settle: ", err)
	}
}

func (r *rig) entries(t *testing.T) []Entry {
	t.Helper()
	got, err := r.log.Last(100)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func redSource(t *testing.T) Source {
	t.Helper()
	return Source{Hash: hashWithSource(t, "RED"), Name: "album", Torrent: redTorrent("RED"), Dir: t.TempDir()}
}

func opsAt(t *testing.T, g *fakeGazelle) Account {
	t.Helper()
	srv := httptest.NewServer(g)
	t.Cleanup(srv.Close)
	site := OPS
	site.API = srv.URL
	return Account{Client: NewGazelle(site, "key-1"), Announce: "https://home.opsfet.ch/pk/announce"}
}

func TestATorrentFromREDIsInjectedWithItsTwinFromOPS(t *testing.T) {
	g := &fakeGazelle{known: map[string]int{strings.ToUpper(hashWithSource(t, "OPS")): 11}}
	r := startRig(t, Config{Gazelle: []Account{opsAt(t, g)}})
	r.runner.Offer(redSource(t))
	r.settle(t)

	got := r.in.all()
	if len(got) != 1 {
		t.Fatalf("injected %d torrents, want the OPS twin", len(got))
	}
	f := got[0]
	if f.Tracker != "OPS" || f.Hash != hashWithSource(t, "OPS") {
		t.Errorf("injected %s %s, want OPS's twin", f.Tracker, f.Hash)
	}
	raw, err := bencode.Raw(f.Torrent, "info")
	if err != nil || bencode.InfoHash(raw) != f.Hash {
		t.Error("the injected .torrent does not have the twin's hash")
	}
	if !strings.Contains(string(f.Torrent), "home.opsfet.ch/pk/announce") {
		t.Error("the injected .torrent does not announce to the user's OPS address")
	}
	if len(f.Files) != 1 || f.Files[0] != (FileEntry{"album", 5}) {
		t.Errorf("files %+v", f.Files)
	}
	e := r.entries(t)
	if len(e) != 1 || e[0].Result != ResultInjected || e[0].Tracker != "OPS" || e[0].InfoHash != f.Hash || e[0].Name != "album" {
		t.Errorf("log %+v", e)
	}
}

func TestATorrentIsNeverAskedForTwiceOnOneSite(t *testing.T) {
	g := &fakeGazelle{}
	r := startRig(t, Config{Gazelle: []Account{opsAt(t, g)}})
	src := redSource(t)
	r.runner.Offer(src)
	r.settle(t)
	r.runner.Offer(src, src)
	r.settle(t)
	if len(g.keys) != 2 {
		t.Errorf("OPS was asked %d times, want once for each of its two source tags", len(g.keys))
	}
	if len(r.entries(t)) != 0 {
		t.Error("no match was logged as an injection or refusal")
	}
}

func TestASiteThatRefusesIsLoggedAndRemembered(t *testing.T) {
	g := &fakeGazelle{}
	acc := opsAt(t, g)
	acc.Client = acc.Client.WithKey("wrong")
	r := startRig(t, Config{Gazelle: []Account{acc}})
	r.runner.Offer(redSource(t))
	r.settle(t)
	e := r.entries(t)
	if len(e) != 1 || e[0].Result != ResultError || !strings.Contains(e[0].Reason, "bad credentials") {
		t.Fatalf("log %+v", e)
	}
	if !r.hist.Asked("OPS", redSource(t).Hash) {
		t.Error("the refusal was not remembered")
	}
}

func TestARefusedInjectionIsLoggedWithItsReason(t *testing.T) {
	g := &fakeGazelle{known: map[string]int{strings.ToUpper(hashWithSource(t, "OPS")): 11}}
	r := startRig(t, Config{Gazelle: []Account{opsAt(t, g)}})
	r.in.refuse = ErrOtherFilesystem
	r.runner.Offer(redSource(t))
	r.settle(t)
	e := r.entries(t)
	if len(e) != 1 || e[0].Result != ResultRefused || e[0].Reason != ErrOtherFilesystem.Error() {
		t.Fatalf("log %+v", e)
	}
}

func TestAnIndexerTwinIsFoundAndTheTorrentItselfPassedOver(t *testing.T) {
	var fetched atomic.Int32
	own := multiFile("Show.S01", 100, 200)
	twin := []byte(strings.Replace(string(own), "http://tracker.x/ann", "http://tracker.y/ann", 1))
	twin = []byte(strings.Replace(string(twin), "7:privatei1e", "7:privatei1e6:source1:Y", 1))
	srv := fakeIndexer(t, []string{item("Show.S01", 300, "own"), item("Show.S01", 300, "twin")},
		map[string][]byte{"own": own, "twin": twin}, &fetched)
	raw, _ := bencode.Raw(own, "info")
	r := startRig(t, Config{Torznab: []*Torznab{NewTorznab("Y", srv.URL+"/api", "idx-key")}})
	r.runner.Offer(Source{Hash: bencode.InfoHash(raw), Name: "Show.S01", Torrent: own, Dir: t.TempDir()})
	r.settle(t)
	got := r.in.all()
	if len(got) != 1 || got[0].Tracker != "Y" || string(got[0].Torrent) != string(twin) {
		t.Fatalf("injected %+v, want the twin from Y", got)
	}
}

func TestAMagnetIsSearchedForByItsFiles(t *testing.T) {
	var fetched atomic.Int32
	twin := multiFile("Show.S01", 100, 200)
	srv := fakeIndexer(t, []string{item("Show.S01", 300, "twin")}, map[string][]byte{"twin": twin}, &fetched)
	r := startRig(t, Config{Torznab: []*Torznab{NewTorznab("Y", srv.URL+"/api", "idx-key")}})
	r.runner.Offer(Source{Hash: "ff", Name: "Show.S01", Dir: t.TempDir(),
		Files: []FileEntry{{"Show.S01/f0.mkv", 100}, {"Show.S01/f1.mkv", 200}}})
	r.settle(t)
	if got := r.in.all(); len(got) != 1 {
		t.Fatalf("injected %d torrents, want the twin", len(got))
	}
}

func TestADaemonTakesTheTorrentInsteadOfASearch(t *testing.T) {
	g := &fakeGazelle{}
	d := &fakeDaemon{}
	srv := httptest.NewServer(d)
	defer srv.Close()
	r := startRig(t, Config{Gazelle: []Account{opsAt(t, g)}, Daemon: NewDaemon(srv.URL, "daemon-key", "")})
	src := redSource(t)
	r.runner.Offer(src)
	r.settle(t)
	r.runner.Offer(src)
	r.settle(t)
	if got, want := d.handedPaths(), filepath.Join(src.Dir, src.Name); len(got) != 1 || got[0] != want {
		t.Fatalf("the daemon was handed %q, want %q once", got, want)
	}
	if len(g.keys) != 0 {
		t.Error("OPS was asked although the daemon searches")
	}
	if e := r.entries(t); len(e) != 1 || e[0].Result != ResultHanded {
		t.Errorf("log %+v", e)
	}
}

// A search cut short by Close is not remembered, so the next start asks
// again.
func TestCloseCutsASearchShortWithoutRememberingIt(t *testing.T) {
	asked := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(asked)
		<-r.Context().Done()
	}))
	defer srv.Close()
	site := OPS
	site.API = srv.URL
	r := startRig(t, Config{Gazelle: []Account{{Client: NewGazelle(site, "key-1"), Announce: "https://home.opsfet.ch/pk/announce"}}})
	src := redSource(t)
	r.runner.Offer(src)
	<-asked
	closed := make(chan struct{})
	go func() {
		r.runner.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Close waited for the search")
	}
	if r.hist.Asked("OPS", src.Hash) {
		t.Error("the search cut short was remembered")
	}
	if e := r.entries(t); len(e) != 0 {
		t.Errorf("the search cut short was logged: %+v", e)
	}
	r.runner.Offer(src)
}

func TestAnInjectionTheClientCannotTakeNowIsAskedForAgain(t *testing.T) {
	g := &fakeGazelle{known: map[string]int{strings.ToUpper(hashWithSource(t, "OPS")): 11}}
	r := startRig(t, Config{Gazelle: []Account{opsAt(t, g)}})
	r.in.refuse = ErrNotNow
	src := redSource(t)
	r.runner.Offer(src)
	r.settle(t)
	if r.hist.Asked("OPS", src.Hash) || len(r.entries(t)) != 0 {
		t.Fatal("an injection put off was remembered or logged")
	}
	r.in.refuse = nil
	r.runner.Offer(src)
	r.settle(t)
	if len(r.in.all()) != 1 {
		t.Fatal("the twin was not injected on the next look")
	}
}

// daemonFolderRig is a runner that hands its torrents to a daemon sharing a
// folder with it, after the RED album finished, and the folder the daemon
// saves its finds in.
func daemonFolderRig(t *testing.T) (*rig, Source, string) {
	t.Helper()
	folder := t.TempDir()
	_, daemon := daemonAt(t, folder)
	r := startRig(t, Config{Daemon: daemon})
	src := redSource(t)
	r.sources = []Source{src}
	r.runner.Offer(src)
	r.settle(t)
	found := filepath.Join(folder, "found")
	if err := os.MkdirAll(found, 0o755); err != nil {
		t.Fatal(err)
	}
	return r, src, found
}

func TestATwinTheDaemonSavesIsInjectedBesideItsTorrent(t *testing.T) {
	r, src, found := daemonFolderRig(t)
	twin := hashWithSource(t, "OPS")
	saved := filepath.Join(found, "[audio][OPS]album["+twin+"].torrent")
	writeFile(t, saved, string(redTorrent("OPS")))
	r.runner.TakeFinds(context.Background())

	got := r.in.all()
	if len(got) != 1 || got[0].Tracker != "OPS" || got[0].Hash != twin {
		t.Fatalf("injected %+v, want the OPS twin", got)
	}
	if _, err := os.Stat(saved); !os.IsNotExist(err) {
		t.Errorf("the find stayed in the folder once it was injected: %v", err)
	}
	if twins := r.hist.Injected(src.Hash); len(twins) != 1 || twins[0] != twin {
		t.Errorf("the twins of the torrent are %v", twins)
	}
	if e := r.entries(t); len(e) != 2 || e[0].Result != ResultInjected || e[0].Tracker != "OPS" {
		t.Errorf("log %+v", e)
	}
}

func TestAFindWithTheFilesOfNoFinishedTorrentIsRefused(t *testing.T) {
	r, _, found := daemonFolderRig(t)
	other := "d8:announce20:http://tracker.x/ann4:infod6:lengthi7e4:name5:other12:piece lengthi16384e6:pieces20:" + pieces20 + "ee"
	saved := filepath.Join(found, "[unknown][Elsewhere]other.torrent")
	writeFile(t, saved, other)
	r.runner.TakeFinds(context.Background())

	if got := r.in.all(); len(got) != 0 {
		t.Fatalf("injected %+v", got)
	}
	if _, err := os.Stat(saved); !os.IsNotExist(err) {
		t.Errorf("the refused find stayed in the folder: %v", err)
	}
	if e := r.entries(t); len(e) != 2 || e[0].Result != ResultRefused || e[0].Tracker != "Elsewhere" {
		t.Errorf("log %+v", e)
	}
}

func TestAFindTheClientCannotTakeNowWaitsForTheNextLook(t *testing.T) {
	r, _, found := daemonFolderRig(t)
	saved := filepath.Join(found, "[audio][OPS]album.torrent")
	writeFile(t, saved, string(redTorrent("OPS")))
	r.in.refuseWith(ErrNotNow)
	r.runner.TakeFinds(context.Background())
	if _, err := os.Stat(saved); err != nil {
		t.Fatalf("the find went although nothing took it: %v", err)
	}
	r.in.refuseWith(nil)
	r.runner.TakeFinds(context.Background())
	if got := r.in.all(); len(got) != 1 {
		t.Fatalf("injected %+v at the next look", got)
	}
}

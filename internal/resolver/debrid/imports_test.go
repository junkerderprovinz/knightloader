package debrid

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

func TestAJobLinkNamesTheAccountAndTheDownload(t *testing.T) {
	for _, c := range []struct{ slot, id, link string }{
		{"realdebrid", "ABC123", "debrid://realdebrid/ABC123"},
		{"realdebrid#work", "ABC123", "debrid://realdebrid/ABC123?account=work"},
		{"torbox", "web/42", "debrid://torbox/web/42"},
	} {
		link := JobLink(c.slot, c.id)
		if link != c.link {
			t.Errorf("JobLink(%q, %q) = %q, want %q", c.slot, c.id, link, c.link)
		}
		slot, id, ok := ParseJobLink(link)
		if !ok || slot != c.slot || id != c.id {
			t.Errorf("ParseJobLink(%q) = %q, %q, %v", link, slot, id, ok)
		}
	}
	for _, raw := range []string{"debrid://realdebrid/", "https://real-debrid.com/d/ABC", testMagnet} {
		if _, _, ok := ParseJobLink(raw); ok {
			t.Errorf("%q read as an imported download", raw)
		}
	}
}

func TestOnlyTheAccountADownloadWasImportedFromClaimsIt(t *testing.T) {
	link := JobLink("realdebrid#work", "ABC123")
	work := Resolver{ServiceID: "realdebrid", Account: "work", Torrents: true}
	if !work.Match(link) {
		t.Error("the account the download is on does not claim it")
	}
	for _, other := range []Resolver{
		{ServiceID: "realdebrid", Torrents: true},
		{ServiceID: "alldebrid", Account: "work", Torrents: true},
	} {
		if other.Match(link) {
			t.Errorf("%s claims a download on another account", other.Info().ID)
		}
	}
	res, err := work.Resolve(context.Background(), resolver.Request{URL: link})
	if err != nil || res.DirectURL != link || res.Name != "" {
		t.Errorf("Resolve = %+v, %v; want the link as it is and no name over the staged one", res, err)
	}
}

func TestAnImportedDownloadIsNeverSentToALinkCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the link check asked about %s", r.URL)
	}))
	defer srv.Close()
	rd := NewRealDebrid("tok")
	rd.base = srv.URL
	got, err := Resolver{ServiceID: "realdebrid", Svc: rd}.Check(context.Background(), []string{JobLink("realdebrid", "ABC")})
	if err != nil || len(got) != 1 || got[0] != core.AvailUncheckable {
		t.Errorf("Check = %v, %v; want it uncheckable, as a torrent is", got, err)
	}
}

func TestRealDebridListsItsNewestTorrents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/torrents" || r.URL.Query().Get("limit") != "100" {
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Error("the list went out without the token")
		}
		fmt.Fprint(w, `[{"id":"RD2","filename":"Show","hash":"ABCDEF","bytes":730,"added":"2026-09-25T10:00:00.000Z","status":"downloaded"}]`)
	}))
	defer srv.Close()
	rd := NewRealDebrid("tok")
	rd.base = srv.URL

	got, complete, err := rd.List(context.Background())
	want := []Listed{{ID: "RD2", Name: "Show", Size: 730, Hash: "abcdef", Added: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)}}
	if err != nil || !complete || !listedEqual(got, want) {
		t.Errorf("List = %+v, %v, %v; want %+v, complete", got, complete, err, want)
	}
}

func TestAllDebridListsEveryMagnet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v4.1/magnet/status" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.FormValue("id") != "" {
			t.Error("the list asked for one magnet")
		}
		fmt.Fprint(w, `{"status":"success","data":{"magnets":[{"id":7,"filename":"Show","size":730,"hash":"ABCDEF","uploadDate":1790000000,"statusCode":1}]}}`)
	}))
	defer srv.Close()
	ad := NewAllDebrid("key")
	ad.base = srv.URL + "/v4"

	got, complete, err := ad.List(context.Background())
	want := []Listed{{ID: "7", Name: "Show", Size: 730, Hash: "abcdef", Added: time.Unix(1790000000, 0)}}
	if err != nil || !complete || !listedEqual(got, want) {
		t.Errorf("List = %+v, %v, %v; want %+v, complete", got, complete, err, want)
	}
}

// Premiumize names a transfer's source only through a proxy link on its own
// API, whatever was added.
func TestPremiumizeListsEveryTransfer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/transfer/list" {
			t.Errorf("unexpected %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"status":"success","transfers":[
			{"id":"PM1","name":"Show","status":"running","src":"https://www.premiumize.me/api/job/src?id=PM1"},
			{"id":"PM2","name":"file.zip","status":"finished","src":"https://www.premiumize.me/api/job/src?id=PM2"}]}`)
	}))
	defer srv.Close()
	pm := NewPremiumize("key")
	pm.base = srv.URL

	got, complete, err := pm.List(context.Background())
	want := []Listed{{ID: "PM1", Name: "Show"}, {ID: "PM2", Name: "file.zip"}}
	if err != nil || !complete || !listedEqual(got, want) {
		t.Errorf("List = %+v, %v, %v; want %+v, complete", got, complete, err, want)
	}
}

func TestDebridLinkListsItsNewestTorrents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/seedbox/list" || r.URL.Query().Get("perPage") != "50" {
			t.Errorf("unexpected %s", r.URL)
		}
		fmt.Fprint(w, `{"success":true,"value":[{"id":"DL1","name":"Show","hashString":"ABCDEF","totalSize":730,"created":1790000000}],
			"pagination":{"page":0,"pages":1,"next":-1,"previous":-1}}`)
	}))
	defer srv.Close()
	dl := NewDebridLink("key")
	dl.base = srv.URL

	got, complete, err := dl.List(context.Background())
	want := []Listed{{ID: "DL1", Name: "Show", Size: 730, Hash: "abcdef", Added: time.Unix(1790000000, 0)}}
	if err != nil || !complete || !listedEqual(got, want) {
		t.Errorf("List = %+v, %v, %v; want %+v, complete", got, complete, err, want)
	}
}

// rdWebsite is a Real-Debrid account whose torrent list the test fills as the
// user would on the website, and which the API adds to as well. It lists the
// newest first, as Real-Debrid does.
type rdWebsite struct {
	mu      sync.Mutex
	list    []rdListed
	n       int
	deleted []string
}

type rdListed struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Hash     string `json:"hash"`
	Added    string `json:"added"`
}

// add puts a torrent on the account the way the website does and returns its
// id.
func (s *rdWebsite) add(name, hash string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	id := fmt.Sprintf("RD%d", s.n)
	s.list = append([]rdListed{{ID: id, Filename: name, Hash: hash, Added: time.Now().UTC().Format(time.RFC3339Nano)}}, s.list...)
	return id
}

func (s *rdWebsite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/torrents":
		s.mu.Lock()
		b, _ := json.Marshal(s.list)
		s.mu.Unlock()
		w.Write(b)
	case r.URL.Path == "/torrents/addMagnet":
		md := magnetHash(r.FormValue("magnet"))
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"id":%q}`, s.add("magnet", md))
	case strings.HasPrefix(r.URL.Path, "/torrents/info/"):
		fmt.Fprint(w, `{"filename":"Show","status":"downloading","progress":10,"files":[],"links":[]}`)
	case strings.HasPrefix(r.URL.Path, "/torrents/delete/"):
		id := strings.TrimPrefix(r.URL.Path, "/torrents/delete/")
		s.mu.Lock()
		s.deleted = append(s.deleted, id)
		s.list = slices.DeleteFunc(s.list, func(e rdListed) bool { return e.ID == id })
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func magnetHash(magnet string) string {
	_, rest, _ := strings.Cut(magnet, "urn:btih:")
	hash, _, _ := strings.Cut(rest, "&")
	return hash
}

// memoryRecord stands in for the app's file: what one account's import last
// saved.
type memoryRecord struct {
	mu  sync.Mutex
	rec ImportRecord
}

func (m *memoryRecord) load() ImportRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	return ImportRecord{Since: m.rec.Since, Seen: slices.Clone(m.rec.Seen)}
}

func (m *memoryRecord) save(r ImportRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rec = r
}

// handedOver collects what an import staged.
type handedOver struct {
	mu  sync.Mutex
	got []string
}

func (h *handedOver) add(j Listed) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.got = append(h.got, j.ID)
}

func (h *handedOver) ids() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.got)
}

// newImport is an account's import as it starts after a restart: from what
// mem holds and nothing else.
func newImport(mem *memoryRecord) (*AccountImport, *handedOver) {
	h := &handedOver{}
	return &AccountImport{Load: mem.load, Save: mem.save, Hand: h.add}, h
}

func rdAccount(t *testing.T) (*RealDebrid, *rdWebsite) {
	t.Helper()
	site := &rdWebsite{}
	srv := httptest.NewServer(site)
	t.Cleanup(srv.Close)
	rd := NewRealDebrid("tok")
	rd.base = srv.URL
	return rd, site
}

func poll(t *testing.T, im *AccountImport, l Lister) {
	t.Helper()
	if err := im.Poll(context.Background(), l); err != nil {
		t.Fatal(err)
	}
}

func TestWhatIsOnTheAccountWhenTheImportBeginsStaysThere(t *testing.T) {
	rd, site := rdAccount(t)
	site.add("Old show", "aaaa")
	im, handed := newImport(&memoryRecord{})

	poll(t, im, rd)
	poll(t, im, rd)
	if got := handed.ids(); len(got) != 0 {
		t.Errorf("imported %v, which was on the account before the import began", got)
	}
}

func TestATorrentAddedOnTheWebsiteIsImportedOnce(t *testing.T) {
	rd, site := rdAccount(t)
	im, handed := newImport(&memoryRecord{})
	poll(t, im, rd)

	id := site.add("New show", "bbbb")
	poll(t, im, rd)
	poll(t, im, rd)
	if got := handed.ids(); !slices.Equal(got, []string{id}) {
		t.Errorf("imported %v, want %s once", got, id)
	}
}

func TestAnImportedTorrentIsNotImportedAgainAfterARestart(t *testing.T) {
	rd, site := rdAccount(t)
	mem := &memoryRecord{}
	im, handed := newImport(mem)
	poll(t, im, rd)
	id := site.add("New show", "bbbb")
	poll(t, im, rd)
	if got := handed.ids(); !slices.Equal(got, []string{id}) {
		t.Fatalf("imported %v before the restart, want %s", got, id)
	}

	restarted, again := newImport(mem)
	poll(t, restarted, rd)
	poll(t, restarted, rd)
	if got := again.ids(); len(got) != 0 {
		t.Errorf("imported %v again after the restart", got)
	}
}

func TestATorrentThisInstanceAddedIsNeverImported(t *testing.T) {
	rd, site := rdAccount(t)
	mem := &memoryRecord{}
	im, handed := newImport(mem)
	poll(t, im, rd)

	claimed := make(chan string, 1)
	b, _ := newTestBackend(t, rd, &partRecorder{})
	b.Added = func(job string) {
		im.Claim(job)
		claimed <- job
	}
	b.Download("t1", testMagnet, nil, 1)
	defer b.Remove("t1", false)
	own := <-claimed

	poll(t, im, rd)
	restarted, again := newImport(mem)
	poll(t, restarted, rd)
	if got := slices.Concat(handed.ids(), again.ids()); len(got) != 0 {
		t.Errorf("imported %v, which this instance added itself (%s)", got, own)
	}
	site.mu.Lock()
	defer site.mu.Unlock()
	if len(site.list) != 1 || site.list[0].ID != own {
		t.Errorf("the account lists %+v, want the torrent this instance added", site.list)
	}
}

// fixedList is a service whose list the test sets directly.
type fixedList struct {
	mu       sync.Mutex
	list     []Listed
	complete bool
}

func (f *fixedList) set(complete bool, list ...Listed) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.list, f.complete = list, complete
}

func (f *fixedList) List(context.Context) ([]Listed, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.list), f.complete, nil
}

func TestATorrentAlreadyInTheListIsNotImported(t *testing.T) {
	l := &fixedList{}
	l.set(true)
	im, handed := newImport(&memoryRecord{})
	im.Known = func(j Listed) bool { return j.Hash == "cccc" }
	poll(t, im, l)

	l.set(true, Listed{ID: "J1", Hash: "cccc"})
	poll(t, im, l)
	im.Known = nil
	poll(t, im, l)
	if got := handed.ids(); len(got) != 0 {
		t.Errorf("imported %v, a torrent the list already had", got)
	}
}

// A web download has no info hash to recognise it by, and one this instance
// is starting shows up on the account before its id comes back.
func TestADownloadWithoutAHashWaitsForASecondLook(t *testing.T) {
	l := &fixedList{}
	l.set(true)
	im, handed := newImport(&memoryRecord{})
	poll(t, im, l)

	l.set(true, Listed{ID: "web/1"}, Listed{ID: "web/2"})
	poll(t, im, l)
	if got := handed.ids(); len(got) != 0 {
		t.Fatalf("imported %v at the first look", got)
	}
	im.Claim("web/1")
	poll(t, im, l)
	if got := handed.ids(); !slices.Equal(got, []string{"web/2"}) {
		t.Errorf("imported %v, want only the download nobody claimed", got)
	}
}

func TestADownloadDatedBeforeTheImportBeganIsNotImported(t *testing.T) {
	l := &fixedList{}
	l.set(false)
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	im, handed := newImport(&memoryRecord{})
	im.Now = func() time.Time { return now }
	poll(t, im, l)

	// A page of the newest only: an older torrent comes back onto it once
	// newer ones are deleted on the website.
	l.set(false, Listed{ID: "old", Hash: "dddd", Added: now.Add(-time.Hour)}, Listed{ID: "new", Hash: "eeee", Added: now.Add(time.Minute)})
	poll(t, im, l)
	if got := handed.ids(); !slices.Equal(got, []string{"new"}) {
		t.Errorf("imported %v, want only the torrent added after the import began", got)
	}
}

func TestACompleteListLetsTheImportForgetWhatIsGone(t *testing.T) {
	l := &fixedList{}
	l.set(true, Listed{ID: "A", Hash: "aaaa"}, Listed{ID: "B", Hash: "bbbb"})
	mem := &memoryRecord{}
	im, _ := newImport(mem)
	poll(t, im, l)

	l.set(true, Listed{ID: "B", Hash: "bbbb"})
	poll(t, im, l)
	if got := mem.load().Seen; !slices.Equal(got, []string{"B"}) {
		t.Errorf("remembers %v, want only what the account still holds", got)
	}

	l.set(false, Listed{ID: "C", Hash: "cccc"})
	poll(t, im, l)
	if got := mem.load().Seen; !slices.Equal(got, []string{"B", "C"}) {
		t.Errorf("remembers %v after a page of the newest; B may only be further down", got)
	}
}

func TestSwitchingTheImportOnAgainStartsAfresh(t *testing.T) {
	l := &fixedList{}
	l.set(true)
	mem := &memoryRecord{}
	im, handed := newImport(mem)
	poll(t, im, l)

	im.Forget()
	l.set(true, Listed{ID: "while-off", Hash: "ffff"})
	poll(t, im, l)
	if got := handed.ids(); len(got) != 0 {
		t.Errorf("imported %v, added while the import was off", got)
	}
}

// heldList answers once release is closed, with the list it had when asked.
type heldList struct {
	fixedList
	asked   chan struct{}
	release chan struct{}
}

func (h *heldList) List(ctx context.Context) ([]Listed, bool, error) {
	list, complete, err := h.fixedList.List(ctx)
	close(h.asked)
	<-h.release
	return list, complete, err
}

func TestAPollAcrossASwitchOffLeavesWhatItReadAlone(t *testing.T) {
	l := &heldList{asked: make(chan struct{}), release: make(chan struct{})}
	l.set(true, Listed{ID: "A", Hash: "aaaa"})
	mem := &memoryRecord{}
	im, _ := newImport(mem)

	done := make(chan error, 1)
	go func() { done <- im.Poll(context.Background(), l) }()
	<-l.asked
	im.Forget()
	close(l.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if rec := mem.load(); !rec.Since.IsZero() || len(rec.Seen) != 0 {
		t.Errorf("remembers %+v from a list read before the import was switched off", rec)
	}
}

func TestAnImportedDownloadIsFetchedWithoutBeingAddedAgain(t *testing.T) {
	svc := &scriptedService{jobs: []TorrentJob{{Name: "Show", Size: 730, State: TorrentReady, Files: twoFiles}}}
	parts := &partRecorder{}
	b, up := newTestBackend(t, svc, parts)

	b.Download("t1", JobLink("fake", "J7"), nil, 1)
	up.until(t, core.StatusDone)

	if len(svc.added) != 0 {
		t.Errorf("the download was added to the service again: %v", svc.added)
	}
	if got := parts.parts(); len(got) != 2 || got[0].Path != "Show/e01.mkv" {
		t.Errorf("the engine got %v", partURLs(got))
	}
	waitFor(t, func() bool { return slices.Equal(svc.deletedJobs(), []string{"J7"}) }, "the finished download deleted on the service")
}

func TestRemovingAnImportedDownloadLeavesItToTheApp(t *testing.T) {
	svc := &scriptedService{jobs: []TorrentJob{{Name: "Show", Progress: 0.1}}}
	b, up := newTestBackend(t, svc, &partRecorder{})

	b.Download("t1", JobLink("fake", "J7"), nil, 1)
	for u := range up.ch {
		if u.Remote != nil && u.Remote.Progress > 0 {
			break
		}
	}
	b.Remove("t1", true)
	time.Sleep(50 * time.Millisecond)
	if d := svc.deletedJobs(); len(d) != 0 {
		t.Errorf("deleted %v; an undone removal would find the download gone", d)
	}
}

func TestAnImportedDownloadTheServiceGivesUpOnFailsInPlace(t *testing.T) {
	svc := &scriptedService{jobs: []TorrentJob{{Name: "Show", State: TorrentFailed, Reason: "dead torrent"}}}
	b, up := newTestBackend(t, svc, &partRecorder{})

	b.Download("t1", JobLink("fake", "J7"), nil, 1)
	failed := up.until(t, core.StatusError)
	if failed.Unsupported || !strings.Contains(failed.Err, "dead torrent") {
		t.Errorf("failed with %+v; nothing else holds the download, so it stays here with the reason", failed)
	}
	time.Sleep(50 * time.Millisecond)
	if d := svc.deletedJobs(); len(d) != 0 {
		t.Errorf("deleted %v, which the user may want to look at on the website", d)
	}
}

func listedEqual(a, b []Listed) bool {
	return slices.EqualFunc(a, b, func(x, y Listed) bool {
		return x.ID == y.ID && x.Name == y.Name && x.Size == y.Size && x.Hash == y.Hash && x.Added.Equal(y.Added)
	})
}

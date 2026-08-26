package jd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// TestAggregateSumsThePackage pins the fix for a link JDownloader crawled into
// several files. Reporting only the first showed a fraction of the real size
// and called the task finished while the rest was still downloading.
func TestAggregateSumsThePackage(t *testing.T) {
	links := []DownloadLink{
		{Name: "part1.rar", BytesTotal: 100, BytesLoaded: 100, Speed: 0, Finished: true},
		{Name: "part2.rar", BytesTotal: 200, BytesLoaded: 50, Speed: 700},
		{Name: "part3.rar", BytesTotal: 300, BytesLoaded: 0, Speed: 300},
	}
	u := aggregate(links)
	if u.Size != 600 {
		t.Errorf("Size = %d, want the package total 600", u.Size)
	}
	if u.Loaded != 150 {
		t.Errorf("Loaded = %d, want 150", u.Loaded)
	}
	if u.Speed != 1000 {
		t.Errorf("Speed = %d, want the combined 1000", u.Speed)
	}
	if u.Status != core.StatusRunning {
		t.Errorf("Status = %q; one finished file does not finish the package", u.Status)
	}
	if !strings.Contains(u.Name, "+2") {
		t.Errorf("Name = %q, want it to say how many more files there are", u.Name)
	}
}

// TestAggregateFinishesOnlyWhenEveryFileIs is the other half: the task settles
// exactly once, when nothing is left.
func TestAggregateFinishesOnlyWhenEveryFileIs(t *testing.T) {
	all := []DownloadLink{
		{Name: "a.bin", BytesTotal: 10, BytesLoaded: 10, Finished: true},
		{Name: "b.bin", BytesTotal: 20, BytesLoaded: 20, Status: "Finished"},
	}
	u := aggregate(all)
	if u.Status != core.StatusDone {
		t.Errorf("Status = %q, want done", u.Status)
	}
	if u.Speed != 0 {
		t.Errorf("Speed = %d on a finished package, want 0", u.Speed)
	}

	one := []DownloadLink{{Name: "solo.bin", BytesTotal: 5, BytesLoaded: 5, Finished: true}}
	if u := aggregate(one); u.Name != "solo.bin" {
		t.Errorf("Name = %q; a single file gets no counter", u.Name)
	}
}

// fakeJDContainer answers just enough of the Deprecated API for
// awaitContainerLinks to run a full submit -> settle -> harvest -> remove
// cycle against something that is not a live JD. It settles immediately
// (isCollecting always false, the link count never changes) because the
// settle-detection loop itself is awaitContainerLinks's own pre-existing,
// unchanged logic — this fake exists to pin AddCryptedV1's wiring into that
// shared path, not to re-prove the loop.
type fakeJDContainer struct {
	t        *testing.T
	mu       sync.Mutex
	dataURLs []string
	pkgName  string
	removed  []int64
}

func newFakeJDContainer(t *testing.T) *fakeJDContainer { return &fakeJDContainer{t: t} }

func (f *fakeJDContainer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/linkgrabberv2/addLinks":
			var params []struct {
				DataURLs    []string `json:"dataURLs"`
				PackageName string   `json:"packageName"`
			}
			decodeCallParams(f.t, r.URL.RawQuery, &params)
			f.mu.Lock()
			if len(params) > 0 {
				f.dataURLs = append(f.dataURLs, params[0].DataURLs...)
				f.pkgName = params[0].PackageName
			}
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"data":{"id":1}}`))
		case "/linkgrabberv2/queryPackages":
			f.mu.Lock()
			name := f.pkgName
			f.mu.Unlock()
			if name == "" {
				_, _ = w.Write([]byte(`{"data":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"uuid":7,"name":"` + name + `"}]}`))
		case "/linkgrabberv2/isCollecting":
			_, _ = w.Write([]byte(`{"data":false}`))
		case "/linkgrabberv2/queryLinks":
			_, _ = w.Write([]byte(`{"data":[{"uuid":100,"url":"https://host.example/a","name":"a.bin","host":"host.example","bytesTotal":4096}]}`))
		case "/linkgrabberv2/removeLinks":
			f.mu.Lock()
			f.removed = append(f.removed, 7)
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"data":true}`))
		default:
			http.NotFound(w, r)
		}
	})
}

// TestAddCryptedV1SubmitsHarvestsAndCleansUp drives AddCryptedV1 end to end
// against fakeJDContainer: the raw bytes go in as an inline dataURLs entry
// (not a URL — this payload was never fetchable), the harvested link comes
// back as a resolver.Result (URL, name AND size - the crawl that decrypted
// the container already knows all three) through the same path AddContainer
// uses, and the package is removed from JD's grabber afterwards so JD does
// not start it on its own.
func TestAddCryptedV1SubmitsHarvestsAndCleansUp(t *testing.T) {
	orig := pollInterval
	pollInterval = 5 * time.Millisecond
	defer func() { pollInterval = orig }()

	fake := newFakeJDContainer(t)
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b := NewBackend(srv.URL, func(string, core.Update) {})
	links, err := b.AddCryptedV1([]byte("rsa-payload-stand-in"), "MyPackage", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].DirectURL != "https://host.example/a" {
		t.Fatalf("links = %+v, want the one harvested link", links)
	}
	if links[0].Name != "a.bin" {
		t.Errorf("Name = %q, want the name the crawl already found, not a bare URL", links[0].Name)
	}
	if links[0].Size != 4096 {
		t.Errorf("Size = %d, want the size the crawl already found", links[0].Size)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.dataURLs) != 1 {
		t.Fatalf("JD received %d dataURLs entries, want 1", len(fake.dataURLs))
	}
	if want := "data:application/dlc;base64,"; len(fake.dataURLs[0]) < len(want) || fake.dataURLs[0][:len(want)] != want {
		t.Errorf("dataURL = %q, want it to start with %q", fake.dataURLs[0], want)
	}
	if len(fake.removed) == 0 {
		t.Error("JD's grabber package was never removed; JD is left free to start it on its own")
	}
}

// fakeJDCheck answers just enough of the Deprecated API for CheckLinks to run
// a full stage -> settle -> read -> remove cycle, with per-URL availability it
// was told to answer rather than a single canned reply - the shape the mapping
// defence (keyed by URL, not position) actually needs something to defend
// against.
type fakeJDCheck struct {
	t       *testing.T
	mu      sync.Mutex
	avail   map[string]string // url -> "ONLINE"/"OFFLINE"/anything else
	links   string            // the raw links string JD received
	pkgName string
	removed []int64
}

func newFakeJDCheck(t *testing.T, avail map[string]string) *fakeJDCheck {
	return &fakeJDCheck{t: t, avail: avail}
}

func (f *fakeJDCheck) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/linkgrabberv2/addLinks":
			var params []struct {
				Links       string `json:"links"`
				PackageName string `json:"packageName"`
			}
			decodeCallParams(f.t, r.URL.RawQuery, &params)
			f.mu.Lock()
			if len(params) > 0 {
				f.links = params[0].Links
				f.pkgName = params[0].PackageName
			}
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"data":{"id":1}}`))
		case "/linkgrabberv2/queryPackages":
			f.mu.Lock()
			name := f.pkgName
			f.mu.Unlock()
			if name == "" {
				_, _ = w.Write([]byte(`{"data":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"uuid":7,"name":"` + name + `"}]}`))
		case "/linkgrabberv2/isCollecting":
			_, _ = w.Write([]byte(`{"data":false}`))
		case "/linkgrabberv2/queryLinks":
			f.mu.Lock()
			defer f.mu.Unlock()
			out := make([]CrawledLink, 0, len(f.avail))
			i := int64(100)
			for u, a := range f.avail {
				out = append(out, CrawledLink{UUID: i, URL: u, Availability: a})
				i++
			}
			b, err := json.Marshal(out)
			if err != nil {
				f.t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"data":` + string(b) + `}`))
		case "/linkgrabberv2/removeLinks":
			f.mu.Lock()
			f.removed = append(f.removed, 7)
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"data":true}`))
		default:
			http.NotFound(w, r)
		}
	})
}

// TestCheckLinksMapsAvailabilityByURL pins the two things that matter about
// CheckLinks: it turns JD's own ONLINE/OFFLINE strings into this app's
// Availability, and it does so by matching the URL JD echoed back rather than
// by position - the fake deliberately hands the two links back in reverse
// order (map iteration order in Go is randomized) so a positional bug would
// show up as a flipped verdict, not a compile error.
func TestCheckLinksMapsAvailabilityByURL(t *testing.T) {
	orig := pollInterval
	pollInterval = 5 * time.Millisecond
	defer func() { pollInterval = orig }()

	const alive = "https://host.example/alive"
	const dead = "https://host.example/dead"
	fake := newFakeJDCheck(t, map[string]string{alive: "ONLINE", dead: "OFFLINE"})
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b := NewBackend(srv.URL, func(string, core.Update) {})
	got, err := b.CheckLinks(context.Background(), []string{alive, dead})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d verdicts, want 2", len(got))
	}
	if got[0] != core.AvailOnline {
		t.Errorf("verdict[0] (%s) = %q, want online", alive, got[0])
	}
	if got[1] != core.AvailOffline {
		t.Errorf("verdict[1] (%s) = %q, want offline", dead, got[1])
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.links != alive+"\n"+dead {
		t.Errorf("JD received links %q, want the two URLs newline-joined", fake.links)
	}
	if len(fake.removed) == 0 {
		t.Error("JD's grabber package was never removed; JD is left free to show it in its own window")
	}
}

// TestCheckLinksUncheckableForAnythingElse pins the safe default: a link JD's
// crawl produced an entry for but stated no ONLINE/OFFLINE opinion on (a bare
// "UNKNOWN", or empty) must not read as either verdict, and must not borrow
// its neighbor's answer either.
func TestCheckLinksUncheckableForAnythingElse(t *testing.T) {
	orig := pollInterval
	pollInterval = 5 * time.Millisecond
	defer func() { pollInterval = orig }()

	const known = "https://host.example/known"
	const undecided = "https://host.example/undecided"
	fake := newFakeJDCheck(t, map[string]string{known: "ONLINE", undecided: "UNKNOWN"})
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b := NewBackend(srv.URL, func(string, core.Update) {})
	got, err := b.CheckLinks(context.Background(), []string{known, undecided})
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != core.AvailOnline {
		t.Errorf("verdict for the known link = %q, want online", got[0])
	}
	if got[1] != core.AvailUncheckable {
		t.Errorf("verdict for the undecided link = %q, want uncheckable, not borrowed from its neighbor", got[1])
	}
}

// TestCheckLinksErrorsTheWholeBatchOnTimeout pins the other side of the same
// contract: a link JD's crawl never produces an entry for at all (dropped,
// unparseable, or simply still running) must not resolve to a per-link
// verdict for anyone in the batch - resolver.Checker promises an error means
// nothing was answered, not "the missing one is offline", and app.runCheck
// relies on exactly that to file the whole group uncheckable rather than
// deleting the ones that were actually fine.
func TestCheckLinksErrorsTheWholeBatchOnTimeout(t *testing.T) {
	orig := pollInterval
	pollInterval = 5 * time.Millisecond
	defer func() { pollInterval = orig }()

	const known = "https://host.example/known"
	const neverCrawled = "https://host.example/never-crawled"
	// The fake only ever reports one of the two URLs the batch asked about, so
	// CheckLinks's own settle condition (len(found) == len(urls)) can never be
	// met - the scenario a short deadline is here to end quickly instead of
	// hanging.
	fake := newFakeJDCheck(t, map[string]string{known: "ONLINE"})
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	b := NewBackend(srv.URL, func(string, core.Update) {})
	if _, err := b.CheckLinks(ctx, []string{known, neverCrawled}); err == nil {
		t.Error("CheckLinks = nil error, want the deadline's error - a batch that never fully settled must not report a partial answer")
	}
}

// TestPatienceLimitsAreSeparate guards the reasoning rather than the numbers: a
// download that takes hours must not be killed for taking hours, so the limit
// on "did it ever start" has to be much shorter than the one on "has it
// stopped moving".
func TestPatienceLimitsAreSeparate(t *testing.T) {
	if appearLimit >= stallLimit {
		t.Errorf("appearLimit %v is not shorter than stallLimit %v; the two answer different questions",
			appearLimit, stallLimit)
	}
	if stallLimit < appearLimit*2 {
		t.Errorf("stallLimit %v leaves too little room for a hoster cool-down", stallLimit)
	}
}

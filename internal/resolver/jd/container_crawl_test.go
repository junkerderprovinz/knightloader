package jd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// grabLink is one link as JD's link grabber reports it, separate from
// CrawledLink so the fake's wire shape does not follow changes to the parser.
type grabLink struct {
	UUID         int64  `json:"uuid"`
	URL          string `json:"url"`
	Name         string `json:"name"`
	Host         string `json:"host"`
	BytesTotal   int64  `json:"bytesTotal"`
	Availability string `json:"availability"`
	PackageUUID  int64  `json:"packageUUID"`
}

// jobFilterBehaviour is what a JD build does with queryLinks's jobUUIDs filter.
type jobFilterBehaviour int

const (
	// jobFilterHonoured: the filter is applied, as the API documents it.
	jobFilterHonoured jobFilterBehaviour = iota
	// jobFilterBlind: the filter is applied and never matches anything, as
	// seen on a live JD.
	jobFilterBlind
	// jobFilterIgnored: the key is unknown, so the query answers with the
	// whole link grabber.
	jobFilterIgnored
)

// grabPkg is one link-grabber package. ours marks the ones this crawl job
// produced, which is what the jobUUIDs filter answers with.
type grabPkg struct {
	uuid  int64
	name  string
	ours  bool
	links []grabLink
}

// fakeJDGrabber is a JD whose link grabber behaves like the ones in the
// field: a container can open into several packages that do not carry the
// marker name, isCollecting can stay true for ever on a busy instance, the
// jobUUIDs filter may work or not, and another user's package sits in the
// same grabber.
type fakeJDGrabber struct {
	t  *testing.T
	mu sync.Mutex

	jobID      int64
	collecting bool
	jobFilter  jobFilterBehaviour
	packages   []grabPkg

	marker       string // the packageName addLinks was given
	removedLinks []int64
	removedPkgs  []int64
	unscoped     int // queryLinks calls with neither filter
}

func (f *fakeJDGrabber) linksFor(match func(grabPkg) bool) []grabLink {
	out := []grabLink{}
	for _, p := range f.packages {
		if match(p) {
			out = append(out, p.links...)
		}
	}
	return out
}

func (f *fakeJDGrabber) writeLinks(w http.ResponseWriter, links []grabLink) {
	b, err := json.Marshal(links)
	if err != nil {
		f.t.Errorf("marshalling the fake's links: %v", err)
		return
	}
	_, _ = w.Write([]byte(`{"data":` + string(b) + `}`))
}

func (f *fakeJDGrabber) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/linkgrabberv2/addLinks":
			var params []struct {
				PackageName string `json:"packageName"`
			}
			decodeCallParams(f.t, r.URL.RawQuery, &params)
			f.mu.Lock()
			if len(params) > 0 {
				f.marker = params[0].PackageName
			}
			id := f.jobID
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"data":{"id":` + itoa(id) + `}}`))

		case "/linkgrabberv2/queryPackages":
			f.mu.Lock()
			defer f.mu.Unlock()
			out := make([]map[string]any, 0, len(f.packages))
			for _, p := range f.packages {
				out = append(out, map[string]any{"uuid": p.uuid, "name": p.name})
			}
			b, err := json.Marshal(out)
			if err != nil {
				f.t.Errorf("marshalling the fake's packages: %v", err)
				return
			}
			_, _ = w.Write([]byte(`{"data":` + string(b) + `}`))

		case "/linkgrabberv2/isCollecting":
			f.mu.Lock()
			busy := f.collecting
			f.mu.Unlock()
			if busy {
				_, _ = w.Write([]byte(`{"data":true}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":false}`))

		case "/linkgrabberv2/queryLinks":
			var params []struct {
				JobUUIDs     []int64 `json:"jobUUIDs"`
				PackageUUIDs []int64 `json:"packageUUIDs"`
			}
			decodeCallParams(f.t, r.URL.RawQuery, &params)
			f.mu.Lock()
			defer f.mu.Unlock()
			switch {
			case len(params) > 0 && len(params[0].JobUUIDs) > 0:
				switch f.jobFilter {
				case jobFilterBlind:
					f.writeLinks(w, nil)
				case jobFilterIgnored:
					f.writeLinks(w, f.linksFor(func(grabPkg) bool { return true }))
				default:
					want := params[0].JobUUIDs[0]
					f.writeLinks(w, f.linksFor(func(p grabPkg) bool { return p.ours && want == f.jobID }))
				}
			case len(params) > 0 && len(params[0].PackageUUIDs) > 0:
				ids := map[int64]bool{}
				for _, id := range params[0].PackageUUIDs {
					ids[id] = true
				}
				f.writeLinks(w, f.linksFor(func(p grabPkg) bool { return ids[p.uuid] }))
			default:
				// Counted so an unscoped harvest shows up in the assertions.
				f.unscoped++
				f.writeLinks(w, f.linksFor(func(grabPkg) bool { return true }))
			}

		case "/linkgrabberv2/removeLinks":
			var params [][]int64
			decodeCallParams(f.t, r.URL.RawQuery, &params)
			f.mu.Lock()
			if len(params) > 0 {
				f.removedLinks = append(f.removedLinks, params[0]...)
			}
			if len(params) > 1 {
				f.removedPkgs = append(f.removedPkgs, params[1]...)
			}
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"data":true}`))

		default:
			http.NotFound(w, r)
		}
	})
}

func itoa(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// fastPoll shortens the grabber polling for a test and restores it afterwards.
func fastPoll(t *testing.T) {
	t.Helper()
	orig := pollInterval
	pollInterval = 5 * time.Millisecond
	t.Cleanup(func() { pollInterval = orig })
}

func urlsOf(t *testing.T, res []resolver.Result) []string {
	t.Helper()
	out := make([]string, 0, len(res))
	for _, r := range res {
		out = append(out, r.DirectURL)
	}
	sort.Strings(out)
	return out
}

// troyPackages is one release mirrored into several packages, each named after
// its contents, plus a package that belongs to the user.
func troyPackages() []grabPkg {
	return []grabPkg{
		{uuid: 11, name: "Troja.2004.DC.German.AC3.DL.2160p.UHD.US.BluRay.DV.HDR.x265-VECTOR", ours: true, links: []grabLink{
			{UUID: 101, URL: "https://mirror-a.example/troja.part1.rar", Name: "troja.part1.rar", BytesTotal: 4096, Availability: "ONLINE", PackageUUID: 11},
			{UUID: 102, URL: "https://mirror-a.example/troja.part2.rar", Name: "troja.part2.rar", BytesTotal: 4096, Availability: "ONLINE", PackageUUID: 11},
		}},
		{uuid: 12, name: "Troja.2004.DC...-VECTOR (Mirror 2)", ours: true, links: []grabLink{
			{UUID: 103, URL: "https://mirror-b.example/troja.part1.rar", Name: "troja.part1.rar", BytesTotal: 4096, Availability: "ONLINE", PackageUUID: 12},
		}},
		{uuid: 99, name: "something the user added through JD's own window", links: []grabLink{
			{UUID: 900, URL: "https://elsewhere.example/not-ours.rar", Name: "not-ours.rar", PackageUUID: 99},
		}},
	}
}

// The container opens into two packages without the marker while the grabber
// keeps collecting. Every link must come back promptly, the user's package must
// stay untouched, and the crawl's output must be removed from the grabber.
func TestAddContainerHarvestsPackagesJDNamedItself(t *testing.T) {
	fastPoll(t)

	f := &fakeJDGrabber{t: t, jobID: 815, collecting: true, jobFilter: jobFilterHonoured, packages: troyPackages()}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	b := NewBackend(srv.URL, func(string, core.Update) {})
	start := time.Now()
	got, err := b.AddContainer("http://kl.example/api/containers/relay/tok", "MyPackage", 3*time.Second)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("AddContainer: %v (the container opened in JD; only finding it again failed)", err)
	}
	want := []string{
		"https://mirror-a.example/troja.part1.rar",
		"https://mirror-a.example/troja.part2.rar",
		"https://mirror-b.example/troja.part1.rar",
	}
	gotURLs := urlsOf(t, got)
	if len(gotURLs) != len(want) {
		t.Fatalf("harvested %v, want all %d links of the container's packages", gotURLs, len(want))
	}
	for i := range want {
		if gotURLs[i] != want[i] {
			t.Fatalf("harvested %v, want %v", gotURLs, want)
		}
	}
	if elapsed > time.Second {
		t.Errorf("AddContainer took %v of its 3s budget; a grabber that is permanently collecting must not stop it settling", elapsed)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.unscoped != 0 {
		t.Errorf("%d queryLinks call(s) read the whole grabber; a harvest has to stay inside its own crawl", f.unscoped)
	}
	removed := map[int64]bool{}
	for _, id := range f.removedLinks {
		removed[id] = true
	}
	for _, id := range f.removedPkgs {
		for _, p := range f.packages {
			if p.uuid == id {
				for _, l := range p.links {
					removed[l.UUID] = true
				}
			}
		}
	}
	for _, id := range []int64{101, 102, 103} {
		if !removed[id] {
			t.Errorf("link %d was left in JD's grabber; JD is free to start it itself", id)
		}
	}
	if removed[900] {
		t.Error("the stranger's link was removed from JD's grabber")
	}
}

// With a job filter that answers nothing, the marker package alone must find
// the crawl.
func TestAddContainerFallsBackToTheMarkerPackage(t *testing.T) {
	fastPoll(t)

	f := &fakeJDGrabber{t: t, jobID: 4711, collecting: true, jobFilter: jobFilterBlind}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	// The marker package appears once addLinks has named it.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			f.mu.Lock()
			marker := f.marker
			if marker != "" && len(f.packages) == 0 {
				f.packages = []grabPkg{{uuid: 21, name: marker, ours: true, links: []grabLink{
					{UUID: 201, URL: "https://mirror-a.example/jungle.rar", Name: "jungle.rar", BytesTotal: 8192, Availability: "ONLINE", PackageUUID: 21},
				}}}
				f.mu.Unlock()
				return
			}
			f.mu.Unlock()
			time.Sleep(time.Millisecond)
		}
	}()
	defer func() { <-done }()

	b := NewBackend(srv.URL, func(string, core.Update) {})
	start := time.Now()
	got, err := b.AddContainer("http://kl.example/api/containers/relay/tok", "MyPackage", 3*time.Second)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("AddContainer: %v", err)
	}
	if len(got) != 1 || got[0].DirectURL != "https://mirror-a.example/jungle.rar" {
		t.Fatalf("harvested %+v, want the one link of the marker package", got)
	}
	if got[0].Name != "jungle.rar" || got[0].Size != 8192 || got[0].Available != core.AvailOnline {
		t.Errorf("harvested %+v, want the name, size and verdict the crawl already found", got[0])
	}
	if elapsed > time.Second {
		t.Errorf("AddContainer took %v of its 3s budget; isCollecting is a hint, never a gate", elapsed)
	}
}

// A JD that ignores the jobUUIDs key answers with the whole grabber; adopting
// that would return the user's links as the container's and delete them.
func TestAddContainerRefusesAJobFilterThatIsNotOne(t *testing.T) {
	fastPoll(t)

	f := &fakeJDGrabber{t: t, jobID: 77, jobFilter: jobFilterIgnored, packages: []grabPkg{
		{uuid: 31, name: "22 Click'n'Load submissions ago", links: []grabLink{
			{UUID: 301, URL: "https://elsewhere.example/his.rar", Name: "his.rar", PackageUUID: 31},
		}},
	}}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	// Our own package arrives under the marker, a little after the handover.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			f.mu.Lock()
			if f.marker != "" && len(f.packages) == 1 {
				f.packages = append(f.packages, grabPkg{uuid: 32, name: f.marker, ours: true, links: []grabLink{
					{UUID: 302, URL: "https://mirror-a.example/ours.rar", Name: "ours.rar", BytesTotal: 512, Availability: "ONLINE", PackageUUID: 32},
				}})
				f.mu.Unlock()
				return
			}
			f.mu.Unlock()
			time.Sleep(time.Millisecond)
		}
	}()
	defer func() { <-done }()

	b := NewBackend(srv.URL, func(string, core.Update) {})
	got, err := b.AddContainer("http://kl.example/api/containers/relay/tok", "MyPackage", 3*time.Second)
	if err != nil {
		t.Fatalf("AddContainer: %v", err)
	}
	if len(got) != 1 || got[0].DirectURL != "https://mirror-a.example/ours.rar" {
		t.Fatalf("harvested %+v, want only the container's own link", got)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range f.removedLinks {
		if id == 301 {
			t.Error("the user's own staged link was deleted out of his link grabber")
		}
	}
	for _, id := range f.removedPkgs {
		if id == 31 {
			t.Error("the user's own package was deleted out of his link grabber")
		}
	}
}

func TestAddContainerStillReportsAContainerThatNeverOpened(t *testing.T) {
	fastPoll(t)

	f := &fakeJDGrabber{t: t, jobID: 1} // no packages at all
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	b := NewBackend(srv.URL, func(string, core.Update) {})
	if _, err := b.AddContainer("http://kl.example/api/containers/relay/tok", "MyPackage", 60*time.Millisecond); err == nil {
		t.Error("AddContainer reported success for a container JD never opened")
	}
}

func TestCheckLinksSettlesWhileJDCollectsSomethingElse(t *testing.T) {
	fastPoll(t)

	const alive = "https://host.example/alive"
	fake := newFakeJDCheck(t, map[string]string{alive: "ONLINE"})
	fake.collecting = true
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	b := NewBackend(srv.URL, func(string, core.Update) {})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := b.CheckLinks(ctx, []string{alive})
	if err != nil {
		t.Fatalf("CheckLinks: %v", err)
	}
	if len(got) != 1 || got[0] != core.AvailOnline {
		t.Errorf("verdicts = %v, want the one online verdict", got)
	}
}

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

// grabLink is one link as JD's link grabber reports it. Spelled out here rather
// than reusing the production CrawledLink so the fake keeps answering the same
// wire shape even when the parsed struct changes.
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
	// jobFilterBlind: the filter is applied and never matches anything - the
	// behaviour measured on a live JD, which is why the marker name has to stay
	// a working anchor of its own.
	jobFilterBlind
	// jobFilterIgnored: the key means nothing to this build, so the query is
	// answered with the entire link grabber. The dangerous one: its answer
	// looks like a crawl that produced everybody's links.
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

// fakeJDGrabber is a JD whose link grabber can be told to behave the way the
// ones in the field do, rather than the way the happy path assumed:
//
//   - the container opens into SEVERAL packages, none of them carrying the name
//     that was passed to addLinks (a scene DLC names its own packages, one per
//     mirror), so looking a package up by that name finds nothing;
//   - isCollecting answers true for ever, because the instance has other work
//     in its grabber (jdp's had 22 Click'n'Load submissions and 5 pastes
//     waiting), so "the grabber has gone quiet" never happens;
//   - the jobUUIDs filter works, or answers nothing, or is not a filter at all;
//   - a package belonging to somebody else sits in the same grabber, so a
//     harvest that reads the grabber unscoped is visibly wrong instead of
//     accidentally right.
type fakeJDGrabber struct {
	t  *testing.T
	mu sync.Mutex

	jobID      int64
	collecting bool
	// jobFilter is what this JD does with queryLinks's jobUUIDs filter:
	// honour it, answer nothing at all, or ignore the key and hand back the
	// whole grabber. All three have to be survivable; the last one is the
	// dangerous one, because its answer looks like a very large crawl.
	jobFilter jobFilterBehaviour
	packages  []grabPkg

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
				// A real JD answers an unfiltered query with the whole grabber,
				// stranger's links included. Counted so a harvest that forgot to
				// scope itself fails loudly here instead of quietly stealing them.
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

// troyPackages is the shape of the container that never landed: one release
// mirrored into several packages, each named after its own contents, plus a
// package that is none of our business.
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

// TestAddContainerHarvestsPackagesJDNamedItself is jdp's failing DLC, in a
// test: the container opens into two packages that do not carry the marker, and
// the grabber never once says it has stopped collecting. Every link inside the
// container has to come back, promptly, without the stranger's package, and
// everything the crawl produced has to be taken back out of JD's grabber so JD
// does not start it on its own.
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

// TestAddContainerFallsBackToTheMarkerPackage is the other half of the same
// contract. The jobUUIDs filter is not trustworthy on every JD build - it has
// been measured answering nothing while the grabber plainly held the links - so
// the marker package has to stay a working second anchor, and a grabber that is
// permanently collecting must not hold that path up either.
func TestAddContainerFallsBackToTheMarkerPackage(t *testing.T) {
	fastPoll(t)

	f := &fakeJDGrabber{t: t, jobID: 4711, collecting: true, jobFilter: jobFilterBlind}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	// The package JD really did name after our marker, filled in once the
	// handover has told the fake what that marker is.
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

// TestAddContainerRefusesAJobFilterThatIsNotOne guards the price of the job
// anchor. A JD build that does not know the jobUUIDs key does not refuse the
// query, it answers it with the whole link grabber - so an answer like that
// must be recognised for what it is rather than adopted as "the links my
// container produced". Adopting it would hand the user's own staged links back
// as the container's contents and then delete them out of his grabber.
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

// TestAddContainerStillReportsAContainerThatNeverOpened keeps the error that
// matters: nothing to find anywhere is not the same as found-and-empty, and it
// must still be reported rather than answered with an empty, successful list.
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

// TestCheckLinksSettlesWhileJDCollectsSomethingElse pins the same reasoning on
// the check path: the batch is settled by its own link count standing still, so
// a grabber busy with 22 other jobs cannot turn every check into a timeout.
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

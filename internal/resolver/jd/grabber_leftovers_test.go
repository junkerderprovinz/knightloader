package jd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// fakeJDDupes is a JD with its duplicate manager switched on, which is how the
// shipped one is configured and how the live instance was measured
// (cfg/jd.controlling.linkcollector.LinkCollectorConfig.json:
// "dupemanagerenabled":true, and LinkgrabberSettings
// "defaultonaddeddupeslinksaction":"ASK" - a question a headless JD has nobody
// to ask).
//
// The behaviour that matters is the silent half: a crawl whose links are
// already sitting in the link grabber produces NOTHING. No package, no empty
// package, no error, no log line. Measured on the live instance on 2026-09-13:
// a Troja DLC whose nineteen links were all present as KnightLoader leftovers
// created no package at all, and the same file created one holding exactly one
// link the moment one leftover was deleted.
type fakeJDDupes struct {
	t  *testing.T
	mu sync.Mutex

	// contents is what the container carries, i.e. what the crawl would produce
	// if the grabber were empty.
	contents []grabLink
	packages []grabPkg
	nextUUID int64

	marker      string
	removedPkgs []int64
}

func (f *fakeJDDupes) has(url string) bool {
	for _, p := range f.packages {
		for _, l := range p.links {
			if l.URL == url {
				return true
			}
		}
	}
	return false
}

// crawlLocked is JD opening the container: every link the grabber already holds
// is dropped without a word, and only what is left becomes a package.
func (f *fakeJDDupes) crawlLocked(marker string) {
	var fresh []grabLink
	for _, l := range f.contents {
		if f.has(l.URL) {
			continue
		}
		fresh = append(fresh, l)
	}
	if len(fresh) == 0 {
		return
	}
	f.nextUUID++
	pkg := grabPkg{uuid: 500 + f.nextUUID, name: marker, ours: true}
	for i := range fresh {
		fresh[i].PackageUUID = pkg.uuid
		pkg.links = append(pkg.links, fresh[i])
	}
	f.packages = append(f.packages, pkg)
}

func (f *fakeJDDupes) handler() http.Handler {
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
				f.crawlLocked(f.marker)
			}
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"data":{"id":9001}}`))

		case "/linkgrabberv2/queryPackages":
			f.mu.Lock()
			out := make([]map[string]any, 0, len(f.packages))
			for _, p := range f.packages {
				out = append(out, map[string]any{"uuid": p.uuid, "name": p.name})
			}
			f.mu.Unlock()
			b, err := json.Marshal(out)
			if err != nil {
				f.t.Errorf("marshalling the fake's packages: %v", err)
				return
			}
			_, _ = w.Write([]byte(`{"data":` + string(b) + `}`))

		case "/linkgrabberv2/isCollecting":
			_, _ = w.Write([]byte(`{"data":false}`))

		case "/linkgrabberv2/queryLinks":
			var params []struct {
				JobUUIDs     []int64 `json:"jobUUIDs"`
				PackageUUIDs []int64 `json:"packageUUIDs"`
			}
			decodeCallParams(f.t, r.URL.RawQuery, &params)
			f.mu.Lock()
			defer f.mu.Unlock()
			// This JD is the measured one: it does not know the jobUUIDs key, so
			// it answers that query with the whole grabber. Backend's probe is
			// what has to notice.
			var links []grabLink
			if len(params) > 0 && len(params[0].PackageUUIDs) > 0 {
				ids := map[int64]bool{}
				for _, id := range params[0].PackageUUIDs {
					ids[id] = true
				}
				for _, p := range f.packages {
					if ids[p.uuid] {
						links = append(links, p.links...)
					}
				}
			} else {
				for _, p := range f.packages {
					links = append(links, p.links...)
				}
			}
			if links == nil {
				links = []grabLink{}
			}
			b, err := json.Marshal(links)
			if err != nil {
				f.t.Errorf("marshalling the fake's links: %v", err)
				return
			}
			_, _ = w.Write([]byte(`{"data":` + string(b) + `}`))

		case "/linkgrabberv2/removeLinks":
			var params [][]int64
			decodeCallParams(f.t, r.URL.RawQuery, &params)
			f.mu.Lock()
			if len(params) > 1 {
				f.removedPkgs = append(f.removedPkgs, params[1]...)
				for _, id := range params[1] {
					for i := range f.packages {
						if f.packages[i].uuid == id {
							f.packages = append(f.packages[:i], f.packages[i+1:]...)
							break
						}
					}
				}
			}
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"data":true}`))

		default:
			http.NotFound(w, r)
		}
	})
}

// trojaContents is the container that would not open: three links that are also
// already sitting in the grabber as KnightLoader's own leftovers.
func trojaContents() []grabLink {
	return []grabLink{
		{UUID: 701, URL: "https://rapidgator.example/file/aaa", Name: "troja.part01.rar", BytesTotal: 4096, Availability: "ONLINE"},
		{UUID: 702, URL: "https://rapidgator.example/file/bbb", Name: "troja.part02.rar", BytesTotal: 4096, Availability: "ONLINE"},
		{UUID: 703, URL: "https://rapidgator.example/file/ccc", Name: "troja.part03.rar", BytesTotal: 4096, Availability: "ONLINE"},
	}
}

// leftoverPackages is what a JD looks like after KnightLoader has staged those
// same links through Backend.Download and never taken them out again: one
// abandoned "KL-<task id>" package per link, plus a package of the user's own
// that is none of our business.
func leftoverPackages() []grabPkg {
	return []grabPkg{
		{uuid: 601, name: "KL-107a116b0657e424", links: []grabLink{
			{UUID: 801, URL: "https://rapidgator.example/file/aaa", Name: "troja.part01.rar", PackageUUID: 601},
		}},
		{uuid: 602, name: "KL-5b0ee833d4383b6e", links: []grabLink{
			{UUID: 802, URL: "https://rapidgator.example/file/bbb", Name: "troja.part02.rar", PackageUUID: 602},
		}},
		{uuid: 603, name: "KL-882c2ed6926a04e6", links: []grabLink{
			{UUID: 803, URL: "https://rapidgator.example/file/ccc", Name: "troja.part03.rar", PackageUUID: 603},
		}},
		{uuid: 699, name: "Avanti ragazzi di Buda", links: []grabLink{
			{UUID: 899, URL: "https://elsewhere.example/his.mp3", Name: "his.mp3", PackageUUID: 699},
		}},
	}
}

// TestAddContainerClearsItsOwnLeftoversBeforeOpening is the measured failure,
// in a test.
//
// JDownloader drops a crawled link that is already in its grabber, silently.
// KnightLoader stages every JD-routed download into that same grabber as
// "KL-<task id>" and never takes it back out, so after one container has been
// downloaded once, its links sit there for ever and the SAME container can
// never be opened again: the crawl yields nothing, JD says nothing, and the
// upload runs into its full timeout.
//
// The leftovers are KnightLoader's own rubbish, so KnightLoader clears them.
// The user's own package is not ours and must survive.
func TestAddContainerClearsItsOwnLeftoversBeforeOpening(t *testing.T) {
	fastPoll(t)

	f := &fakeJDDupes{t: t, contents: trojaContents(), packages: leftoverPackages()}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	b := NewBackend(srv.URL, func(string, core.Update) {})
	got, err := b.AddContainer("http://kl.example/api/containers/relay/tok", "Troja", 2*time.Second)
	if err != nil {
		t.Fatalf("AddContainer: %v (JD decrypted the container; its own leftovers ate every link)", err)
	}
	if len(got) != 3 {
		t.Fatalf("harvested %v, want all 3 links of the container", urlsOf(t, got))
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.packages {
		if p.name == "KL-107a116b0657e424" || p.name == "KL-5b0ee833d4383b6e" || p.name == "KL-882c2ed6926a04e6" {
			t.Errorf("abandoned package %q is still in JD's grabber", p.name)
		}
	}
	strangerGone := true
	for _, p := range f.packages {
		if p.uuid == 699 {
			strangerGone = false
		}
	}
	if strangerGone {
		t.Error("the user's own package was deleted out of his link grabber")
	}
}

// TestSweepKeepsWhatIsStillInFlight is the price of the sweep, and the reason
// it is not a blanket "delete everything called KL-".
//
// A download that was handed to JD moments ago has a "KL-<task id>" package in
// the grabber that is mid-crawl, and a second container being opened at the
// same time has its own marker package there. Neither is abandoned, and
// sweeping either would kill a job that is working.
func TestSweepKeepsWhatIsStillInFlight(t *testing.T) {
	f := &fakeJDDupes{t: t, packages: []grabPkg{
		{uuid: 611, name: "KL-aaaabbbbccccdddd"},            // a live download
		{uuid: 612, name: "KL-1789330428997"},               // another container being opened
		{uuid: 613, name: "KL-check-1789330428998"},         // a check in progress
		{uuid: 614, name: "KL-0011223344556677"},            // abandoned
		{uuid: 615, name: "KL-Cloud.Atlas.2012.REMASTERED"}, // the user's own, not our shape
	}}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	b := NewBackend(srv.URL, func(string, core.Update) {})
	b.holdGrabber("KL-aaaabbbbccccdddd")
	b.holdGrabber("KL-1789330428997")
	b.holdGrabber("KL-check-1789330428998")

	b.sweepGrabber()

	f.mu.Lock()
	defer f.mu.Unlock()
	left := map[string]bool{}
	for _, p := range f.packages {
		left[p.name] = true
	}
	for _, name := range []string{"KL-aaaabbbbccccdddd", "KL-1789330428997", "KL-check-1789330428998", "KL-Cloud.Atlas.2012.REMASTERED"} {
		if !left[name] {
			t.Errorf("%q was swept out of JD's grabber; it is not abandoned", name)
		}
	}
	if left["KL-0011223344556677"] {
		t.Error("the abandoned package survived the sweep")
	}
}

// TestPausedTaskSurvivesTheSweep closes the one hole the sweep's safety
// argument has.
//
// "A package nothing is watching is abandoned" is true of every state except
// one: a paused task is alive, the user means to come back to it, and nothing
// is watching it. Pause a download while JD is still crawling it, open a
// container in the same few seconds, and a sweep with no memory of the pause
// would take the crawl away under it.
func TestPausedTaskSurvivesTheSweep(t *testing.T) {
	f := &fakeJDDupes{t: t, packages: []grabPkg{
		{uuid: 631, name: "KL-aaaabbbbccccdddd"},
		{uuid: 632, name: "KL-0011223344556677"}, // abandoned, for contrast
	}}
	mux := http.NewServeMux()
	mux.Handle("/linkgrabberv2/", f.handler())
	mux.HandleFunc("/downloadsV2/queryPackages", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	b := NewBackend(srv.URL, func(string, core.Update) {})
	// A poller is watching, exactly as Download would have left it.
	stop := make(chan struct{})
	b.mu.Lock()
	b.stop["aaaabbbbccccdddd"] = stop
	b.mu.Unlock()
	b.holdGrabber("KL-aaaabbbbccccdddd")

	b.Pause("aaaabbbbccccdddd")
	// The poller notices and exits, giving its own hold back.
	b.releaseGrabber("KL-aaaabbbbccccdddd")

	b.sweepGrabber()

	f.mu.Lock()
	left := map[string]bool{}
	for _, p := range f.packages {
		left[p.name] = true
	}
	f.mu.Unlock()
	if !left["KL-aaaabbbbccccdddd"] {
		t.Error("the paused task's crawl was swept out of JD's grabber; the user only pressed pause")
	}
	if left["KL-0011223344556677"] {
		t.Error("the abandoned package survived; the pause must not disarm the sweep for everything else")
	}
}

// TestRemoveTakesTheTaskOutOfTheGrabberToo pins the leak at its source.
//
// Remove used to clear the DOWNLOAD list only. A task whose link never got that
// far - and on the measured instance twenty-three had not - left its grabber
// package behind for ever, JD reloads the grabber on every restart
// (GeneralSettings "savelinkgrabberlistenabled":true), and from then on every
// container carrying that link opens into nothing.
func TestRemoveTakesTheTaskOutOfTheGrabberToo(t *testing.T) {
	f := &fakeJDDupes{t: t, packages: []grabPkg{
		{uuid: 621, name: "KL-107a116b0657e424", links: []grabLink{
			{UUID: 821, URL: "https://rapidgator.example/file/aaa", PackageUUID: 621},
		}},
	}}
	mux := http.NewServeMux()
	mux.Handle("/linkgrabberv2/", f.handler())
	mux.HandleFunc("/downloadsV2/queryPackages", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	mux.HandleFunc("/downloadsV2/removeLinks", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":true}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	b := NewBackend(srv.URL, func(string, core.Update) {})
	b.Remove("107a116b0657e424", true)

	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.packages {
		if p.name == "KL-107a116b0657e424" {
			t.Error("the removed task's package is still in JD's link grabber, where it will eat that link out of every future container")
		}
	}
}

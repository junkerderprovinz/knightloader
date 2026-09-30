package debrid

// Real-Debrid, AllDebrid and Debrid-Link have no cache check, so with only
// cached torrents allowed they are asked by adding the torrent and reading its
// job. These tests drive each service's fake through that, and through a job
// the service makes no progress on.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// seedbox is the state behind one fake service: every add makes the one job,
// and each read of it reports what fetch says for the time since the add.
type seedbox struct {
	mu      sync.Mutex
	added   time.Time
	adds    int
	deletes int
	fetch   func(since time.Duration) (percent float64, ready bool)
}

func newSeedbox(fetch func(time.Duration) (float64, bool)) *seedbox {
	return &seedbox{added: time.Now(), fetch: fetch}
}

func (s *seedbox) add() {
	s.adds++
	s.added = time.Now()
}

func (s *seedbox) read() (float64, bool) {
	return s.fetch(time.Since(s.added))
}

func (s *seedbox) counts() (adds, deletes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.adds, s.deletes
}

func cachedAtOnce(time.Duration) (float64, bool) { return 100, true }

func stuckAt(percent float64) func(time.Duration) (float64, bool) {
	return func(time.Duration) (float64, bool) { return percent, false }
}

// seedboxService is one service's fake, with the id its job gets.
type seedboxService struct {
	name, job string
	start     func(t *testing.T, s *seedbox) TorrentService
}

var seedboxServices = []seedboxService{
	{"Real-Debrid", "J1", realDebridSeedbox},
	{"AllDebrid", "1", allDebridSeedbox},
	{"Debrid-Link", "J1", debridLinkSeedbox},
}

func (s *seedbox) serve(t *testing.T, h func(w http.ResponseWriter, r *http.Request)) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// realDebridSeedbox waits for a file selection before the job counts, as
// Real-Debrid does: a torrent it has cached is "downloaded" once the files
// are chosen.
func realDebridSeedbox(t *testing.T, s *seedbox) TorrentService {
	selected := false
	rd := NewRealDebrid("tok")
	rd.base = s.serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /torrents/addMagnet":
			s.add()
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":"J1"}`)
		case "GET /torrents":
			fmt.Fprint(w, `[]`)
		case "GET /torrents/info/J1":
			if !selected {
				fmt.Fprint(w, `{"filename":"Show","bytes":700,"status":"waiting_files_selection","files":[{"id":1,"path":"/e01.mkv","bytes":700,"selected":0}],"links":[]}`)
				return
			}
			const files = `[{"id":1,"path":"/e01.mkv","bytes":700,"selected":1}]`
			if percent, ready := s.read(); !ready {
				fmt.Fprintf(w, `{"filename":"Show","bytes":700,"status":"downloading","progress":%g,"files":%s,"links":[]}`, percent, files)
				return
			}
			fmt.Fprintf(w, `{"filename":"Show","bytes":700,"status":"downloaded","progress":100,"files":%s,"links":["https://real-debrid.com/d/A"]}`, files)
		case "POST /torrents/selectFiles/J1":
			selected = true
			w.WriteHeader(http.StatusNoContent)
		case "POST /unrestrict/link":
			fmt.Fprint(w, `{"download":"https://dl.example/A","filename":"e01.mkv","filesize":700}`)
		case "DELETE /torrents/delete/J1":
			s.deletes++
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	return rd
}

// allDebridSeedbox reports a magnet it has cached as status code 4, Ready.
func allDebridSeedbox(t *testing.T, s *seedbox) TorrentService {
	ad := NewAllDebrid("key")
	ad.base = s.serve(t, func(w http.ResponseWriter, r *http.Request) {
		ok := func(data string) { fmt.Fprintf(w, `{"status":"success","data":%s}`, data) }
		switch r.URL.Path {
		case "/v4/magnet/upload":
			s.add()
			ok(`{"magnets":[{"magnet":"x","id":1,"ready":false}]}`)
		case "/v4.1/magnet/status":
			if r.FormValue("id") == "" {
				ok(`{"magnets":[]}`)
				return
			}
			percent, ready := s.read()
			code := 1
			if ready {
				code, percent = 4, 100
			}
			ok(fmt.Sprintf(`{"magnets":{"id":1,"filename":"Show","size":700,"status":"x","statusCode":%d,"downloaded":%d}}`, code, int(percent*7)))
		case "/v4/magnet/files":
			ok(`{"magnets":[{"id":"1","files":[{"n":"e01.mkv","s":700,"l":"https://alldebrid.com/f/A"}]}]}`)
		case "/v4/link/unlock":
			ok(`{"link":"https://dl.example/A","filename":"e01.mkv"}`)
		case "/v4/magnet/delete":
			s.deletes++
			ok(`{"message":"Magnet was successfully deleted"}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	ad.base += "/v4"
	return ad
}

// debridLinkSeedbox reports a torrent it has cached as finished, every file
// at 100 percent with its link.
func debridLinkSeedbox(t *testing.T, s *seedbox) TorrentService {
	dl := NewDebridLink("key")
	dl.base = s.serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /seedbox/add":
			s.add()
			fmt.Fprint(w, `{"success":true,"value":{"id":"J1","name":"Show","status":1,"files":[]}}`)
		case "GET /seedbox/list":
			if r.URL.Query().Has("perPage") {
				fmt.Fprint(w, `{"success":true,"value":[]}`)
				return
			}
			percent, ready := s.read()
			status, link := 4, ""
			if ready {
				status, percent, link = 100, 100, "https://seed.example/1"
			}
			fmt.Fprintf(w, `{"success":true,"value":[{"id":"J1","name":"Show","status":%d,"totalSize":700,"downloadPercent":%g,"files":[`+
				`{"id":"J1-1","name":"e01.mkv","size":700,"downloadUrl":%q,"downloadPercent":%g}]}]}`, status, percent, link, percent)
		case "DELETE /seedbox/J1/remove":
			s.deletes++
			fmt.Fprint(w, `{"success":true,"value":["J1"]}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	return dl
}

func TestACachedTorrentIsFetchedWithoutASecondAddWhenOnlyCachedOnesMay(t *testing.T) {
	for _, svc := range seedboxServices {
		t.Run(svc.name, func(t *testing.T) {
			s := newSeedbox(cachedAtOnce)
			parts := &partRecorder{}
			b, up := newTestBackend(t, svc.start(t, s), parts)
			b.CachedOnly = onlyCached

			b.Download("t1", testMagnet, nil, 1)
			up.until(t, core.StatusDone)

			if adds, _ := s.counts(); adds != 1 {
				t.Errorf("the torrent was added %d times, want once", adds)
			}
			if got := parts.parts(); len(got) != 1 || got[0].Path != "e01.mkv" {
				t.Errorf("the engine got %v, want the one file", partURLs(got))
			}
			var probed, kept bool
			for _, u := range up.all() {
				if u.Job != nil && u.Job.ID == svc.job {
					probed = probed || u.Job.Probe
					kept = kept || probed && !u.Job.Probe
				}
			}
			if !probed || !kept {
				t.Errorf("the task was told of the job as a probe %v and then as its own %v; want both", probed, kept)
			}
		})
	}
}

func TestAnUncachedTorrentMovesOnAndIsDeletedThere(t *testing.T) {
	for _, svc := range seedboxServices {
		t.Run(svc.name, func(t *testing.T) {
			s := newSeedbox(stuckAt(0))
			parts := &partRecorder{}
			b, up := newTestBackend(t, svc.start(t, s), parts)
			b.CachedOnly = onlyCached
			b.cacheWait = 50 * time.Millisecond

			b.Download("t1", testMagnet, nil, 1)
			u := up.until(t, core.StatusError)

			if !u.Unsupported || !strings.Contains(u.Err, "did not have it ready") {
				t.Errorf("the task came back as %+v; it goes on to the next backend with the reason", u)
			}
			waitFor(t, func() bool { _, deletes := s.counts(); return deletes == 1 }, "the torrent deleted on the service")
			if len(parts.parts()) != 0 {
				t.Error("files of the uncached torrent went to the engine")
			}
		})
	}
}

// The job stays with the task over the shutdown, as any job does, and the
// next start reads it again rather than adding the torrent twice.
func TestAProbeAShutdownCutShortIsDeletedAtTheNextStart(t *testing.T) {
	for _, svc := range seedboxServices {
		t.Run(svc.name, func(t *testing.T) {
			s := newSeedbox(stuckAt(0))
			service := svc.start(t, s)
			b, up := newTestBackend(t, service, &partRecorder{})
			b.CachedOnly = onlyCached
			b.cacheWait = time.Minute

			b.Download("t1", testMagnet, nil, 1)
			var held core.ServiceJob
			waitFor(t, func() bool {
				for _, u := range up.all() {
					if u.Job != nil && u.Job.Probe {
						held = *u.Job
						return true
					}
				}
				return false
			}, "the probe noted on the task")
			b.runs.Stop()
			if held.ID != svc.job || !held.Owned {
				t.Fatalf("the task holds %+v, want the probe as a job of its own", held)
			}

			next, up := newTestBackend(t, service, &partRecorder{})
			next.runs.Restore("t1", testMagnet, held)
			next.CachedOnly = onlyCached
			next.cacheWait = 50 * time.Millisecond
			next.Download("t1", testMagnet, nil, 1)
			u := up.until(t, core.StatusError)

			if !u.Unsupported {
				t.Errorf("the task came back as %+v; it goes on to the next backend", u)
			}
			waitFor(t, func() bool { _, deletes := s.counts(); return deletes == 1 }, "the probe deleted on the service")
			if adds, _ := s.counts(); adds != 1 {
				t.Errorf("the torrent was added %d times, want once", adds)
			}
		})
	}
}

func TestATorrentTheServiceMakesNoProgressOnMovesOnAndIsDeletedThere(t *testing.T) {
	for _, svc := range seedboxServices {
		t.Run(svc.name, func(t *testing.T) {
			s := newSeedbox(stuckAt(10))
			b, up := newTestBackend(t, svc.start(t, s), &partRecorder{})
			b.StallMinutes = func(string) int { return 2 }
			b.minute = 20 * time.Millisecond

			b.Download("t1", testMagnet, nil, 1)
			u := up.until(t, core.StatusError)

			if !u.Unsupported || !strings.HasSuffix(u.Err, "the service made no progress on this torrent for 2 minutes") {
				t.Errorf("the task came back as %+v; it goes on to the next backend with the reason", u)
			}
			waitFor(t, func() bool { _, deletes := s.counts(); return deletes == 1 }, "the torrent deleted on the service")
		})
	}
}

// The fetch takes three times as long as the service may stand still, and
// it moves all the while.
func TestProgressKeepsATorrentOnTheService(t *testing.T) {
	for _, svc := range seedboxServices {
		t.Run(svc.name, func(t *testing.T) {
			const took = 300 * time.Millisecond
			s := newSeedbox(func(since time.Duration) (float64, bool) {
				return float64(since) / float64(took) * 100, since >= took
			})
			b, up := newTestBackend(t, svc.start(t, s), &partRecorder{})
			b.StallMinutes = func(string) int { return 2 }
			b.minute = 50 * time.Millisecond

			b.Download("t1", testMagnet, nil, 1)
			up.until(t, core.StatusDone)

			for _, u := range up.all() {
				if u.Unsupported {
					t.Errorf("the task was handed on while the service made progress: %+v", u)
				}
			}
		})
	}
}

func TestAnImportedTorrentIsNeverGivenUpOnForWantOfProgress(t *testing.T) {
	for _, svc := range seedboxServices {
		t.Run(svc.name, func(t *testing.T) {
			s := newSeedbox(stuckAt(10))
			b, up := newTestBackend(t, svc.start(t, s), &partRecorder{})
			b.StallMinutes = func(string) int { return 1 }
			b.minute = 10 * time.Millisecond

			b.Download("t1", JobLink("fake", svc.job), nil, 1)
			time.Sleep(200 * time.Millisecond)
			b.Remove("t1", false)
			time.Sleep(50 * time.Millisecond)

			if _, deletes := s.counts(); deletes != 0 {
				t.Errorf("the user's own job was deleted %d times", deletes)
			}
			for _, u := range up.all() {
				if u.Status == core.StatusError {
					t.Errorf("the imported download was given up on: %+v", u)
				}
			}
		})
	}
}

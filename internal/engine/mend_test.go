package engine

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GopeedLab/gopeed/pkg/download"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

// The library saves each connection with its range and how much of it
// arrived; what did not arrive of each range is the file's gaps.
func TestTheGapsAreWhatTheConnectionsDidNotFetch(t *testing.T) {
	raw := []byte(`{"Connections":[
		{"ID":0,"Chunk":{"Begin":0,"End":399,"Downloaded":400},"Downloaded":400,"Completed":true},
		{"ID":1,"Chunk":{"Begin":400,"End":599,"Downloaded":50},"Downloaded":50},
		{"ID":2,"Chunk":{"Begin":600,"End":799,"Downloaded":0},"Downloaded":0},
		{"ID":3,"Chunk":{"Begin":800,"End":999,"Downloaded":100},"Downloaded":100}
	],"RedirectURL":""}`)

	got := gapsIn(raw, 1000, true)

	want := []span{{450, 799}, {900, 999}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("gaps = %v, want %v", got, want)
	}
}

// Without ranges one connection wrote from the start, and the gap is the rest.
func TestAnUnrangedTransferIsMissingItsTail(t *testing.T) {
	raw := []byte(`{"Connections":[{"ID":0,"Chunk":{"Begin":0,"End":0,"Downloaded":300},"Downloaded":300}]}`)

	if got, want := gapsIn(raw, 1000, false), []span{{300, 999}}; !reflect.DeepEqual(got, want) {
		t.Errorf("gaps = %v, want %v", got, want)
	}
}

func TestALandedRangeComesOffTheGaps(t *testing.T) {
	gaps := []span{{0, 99}, {200, 299}}
	if got, want := landed(gaps, span{50, 249}), []span{{0, 49}, {250, 299}}; !reflect.DeepEqual(got, want) {
		t.Errorf("landed = %v, want %v", got, want)
	}
}

// refusingOrigin serves one file with ranges. The path /old answers a range
// that starts at or past the middle with 403, for the first refusals requests
// of that kind or, with refusals below 0, always. Any other path serves every
// range, as a fresh link does.
type refusingOrigin struct {
	srv      *httptest.Server
	data     []byte
	refusals int

	mu      sync.Mutex
	refused int
	pace    time.Duration
}

func newRefusingOrigin(t *testing.T, size, refusals int) *refusingOrigin {
	t.Helper()
	o := &refusingOrigin{data: make([]byte, size), refusals: refusals}
	_, _ = cryptorand.Read(o.data)
	o.srv = httptest.NewServer(http.HandlerFunc(o.serve))
	t.Cleanup(o.srv.Close)
	return o
}

func (o *refusingOrigin) serve(w http.ResponseWriter, r *http.Request) {
	lo, hi := 0, len(o.data)-1
	rg, ranged := strings.CutPrefix(r.Header.Get("Range"), "bytes=")
	if ranged {
		from, to, _ := strings.Cut(rg, "-")
		lo, _ = strconv.Atoi(from)
		if to != "" {
			hi, _ = strconv.Atoi(to)
		}
	}
	o.mu.Lock()
	refuse := r.URL.Path == "/old" && ranged && lo >= len(o.data)/2 && (o.refusals < 0 || o.refused < o.refusals)
	if refuse {
		o.refused++
	}
	pace := o.pace
	o.mu.Unlock()
	if refuse {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.Itoa(hi-lo+1))
	if ranged {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", lo, hi, len(o.data)))
		w.WriteHeader(http.StatusPartialContent)
	}
	for off := lo; off <= hi; off += 32 << 10 {
		if _, err := w.Write(o.data[off:min(off+32<<10, hi+1)]); err != nil {
			return
		}
		w.(http.Flusher).Flush()
		time.Sleep(pace)
	}
}

// holedFile writes o's file with its second half missing, the way the library
// leaves it after a refused range: the length reserved, the bytes not there.
func holedFile(t *testing.T, o *refusingOrigin) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f.bin")
	b := append([]byte(nil), o.data...)
	clear(b[len(b)/2:])
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// mendingEngine is an engine that knows job as t1's and records what it
// reports.
func mendingEngine(t *testing.T, job Job) (*Engine, *updates) {
	t.Helper()
	u := &updates{}
	e, err := New(t.TempDir(), u.add)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	job.TaskID = "t1"
	e.jobs["t1"] = job
	return e, u
}

// finishedTask is the library's finished task for a file of size bytes.
func finishedTask(t *testing.T, size int) *download.Task {
	t.Helper()
	var task download.Task
	if err := json.Unmarshal([]byte(fmt.Sprintf(`{"id":"g1","protocol":"http","meta":{"res":{"size":%d,"range":true,"files":[{"name":"f.bin"}]},"opts":{}}}`, size)), &task); err != nil {
		t.Fatal(err)
	}
	return &task
}

func (u *updates) settled() (core.Update, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, up := range u.list {
		if up.Status == core.StatusDone || up.Status == core.StatusError {
			return up, true
		}
	}
	return core.Update{}, false
}

// A link that stopped working part way leaves the rest of the file to a fresh
// link, and the task is done once every byte is there.
func TestMissingRangesComeFromAFreshLink(t *testing.T) {
	o := newRefusingOrigin(t, 1<<20, -1)
	var relinks atomic.Int32
	e, u := mendingEngine(t, Job{URL: o.srv.URL + "/old", Relink: func(context.Context) (string, error) {
		relinks.Add(1)
		return o.srv.URL + "/fresh", nil
	}})
	file := holedFile(t, o)
	half := int64(len(o.data) / 2)

	e.startMend("t1", finishedTask(t, len(o.data)), file, []span{{half, int64(len(o.data)) - 1}})

	waitUntil(t, "the task settling", func() bool { _, ok := u.settled(); return ok })
	last, _ := u.settled()
	if last.Status != core.StatusDone {
		t.Fatalf("the task ended %q: %s", last.Status, last.Err)
	}
	if got, _ := os.ReadFile(file); !bytes.Equal(got, o.data) {
		t.Error("the file is not the one the server has")
	}
	if relinks.Load() == 0 {
		t.Error("the fresh link was never asked for")
	}
}

// A 403 that meant too many connections is gone once the others have
// finished, so the URL the transfer had is asked first.
func TestMissingRangesComeFromTheSameLinkWhenItAnswersAgain(t *testing.T) {
	o := newRefusingOrigin(t, 1<<20, 0)
	e, u := mendingEngine(t, Job{URL: o.srv.URL + "/old"})
	file := holedFile(t, o)
	half := int64(len(o.data) / 2)

	e.startMend("t1", finishedTask(t, len(o.data)), file, []span{{half, int64(len(o.data)) - 1}})

	waitUntil(t, "the task settling", func() bool { _, ok := u.settled(); return ok })
	if last, _ := u.settled(); last.Status != core.StatusDone {
		t.Fatalf("the task ended %q: %s", last.Status, last.Err)
	}
	if got, _ := os.ReadFile(file); !bytes.Equal(got, o.data) {
		t.Error("the file is not the one the server has")
	}
}

// With nobody to hand out a fresh link and the server refusing for good, the
// task fails and says how much is missing, instead of calling a file with a
// hole in it done.
func TestMissingRangesTheServerKeepsRefusingFailTheTask(t *testing.T) {
	was := mendWait
	mendWait = 10 * time.Millisecond
	t.Cleanup(func() { mendWait = was })
	o := newRefusingOrigin(t, 1<<20, -1)
	e, u := mendingEngine(t, Job{URL: o.srv.URL + "/old"})
	file := holedFile(t, o)
	half := int64(len(o.data) / 2)

	e.startMend("t1", finishedTask(t, len(o.data)), file, []span{{half, int64(len(o.data)) - 1}})

	waitUntil(t, "the task settling", func() bool { _, ok := u.settled(); return ok })
	last, _ := u.settled()
	if last.Status != core.StatusError {
		t.Fatalf("the task ended %q, want a failure", last.Status)
	}
	if !strings.Contains(last.Err, "0.5 MiB never arrived") || !strings.Contains(last.Err, "HTTP 403") {
		t.Errorf("the failure reads %q, want the missing size and the server's answer", last.Err)
	}
	if last.File != file {
		t.Errorf("the failure names the file %q, want %q", last.File, file)
	}
}

// A pause stops the fetching and keeps what arrived; a resume carries on and
// finishes the file.
func TestAPausedMendCarriesOnWhenResumed(t *testing.T) {
	o := newRefusingOrigin(t, 4<<20, 0)
	o.pace = 20 * time.Millisecond
	e, u := mendingEngine(t, Job{URL: o.srv.URL + "/old"})
	file := holedFile(t, o)
	half := int64(len(o.data) / 2)
	e.startMend("t1", finishedTask(t, len(o.data)), file, []span{{half, int64(len(o.data)) - 1}})
	waitUntil(t, "part of the gap arriving", func() bool { return u.loaded() > half+256<<10 })

	e.Pause("t1")
	e.mu.Lock()
	ended := e.mends["t1"].ended
	e.mu.Unlock()
	<-ended
	if _, ok := u.settled(); ok {
		t.Fatal("a paused mend settled the task")
	}
	e.Resume("t1")

	waitUntil(t, "the task settling", func() bool { _, ok := u.settled(); return ok })
	if last, _ := u.settled(); last.Status != core.StatusDone {
		t.Fatalf("the task ended %q: %s", last.Status, last.Err)
	}
	if got, _ := os.ReadFile(file); !bytes.Equal(got, o.data) {
		t.Error("the file is not the one the server has")
	}
}

// The whole path through the library: a range refused with 403 part way
// through, which the library leaves out when it finishes, is fetched from a
// fresh link before the task is reported done.
func TestADownloadWithARefusedRangeIsNotDoneUntilTheRangeArrives(t *testing.T) {
	if raceEnabled {
		// See startSlow.
		t.Skip("gopeed v1.9.3 has internal data races in every real HTTP transfer")
	}
	o := newRefusingOrigin(t, 4<<20, -1)
	o.pace = 5 * time.Millisecond
	u := &updates{}
	dir := t.TempDir()
	e, err := New(dir, u.add)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	var relinks atomic.Int32
	e.Start(Job{TaskID: "t1", URL: o.srv.URL + "/old", Conns: 2, Dir: dir, Relink: func(context.Context) (string, error) {
		relinks.Add(1)
		return o.srv.URL + "/fresh", nil
	}})

	waitUntil(t, "the download settling", func() bool { _, ok := u.settled(); return ok })
	last, _ := u.settled()
	if last.Status != core.StatusDone {
		t.Fatalf("the download ended %q: %s", last.Status, last.Err)
	}
	o.mu.Lock()
	refused := o.refused
	o.mu.Unlock()
	if refused == 0 {
		t.Fatal("no range was refused, so this proves nothing")
	}
	if got, err := os.ReadFile(last.File); err != nil || !bytes.Equal(got, o.data) {
		t.Errorf("%s is not the file the server has (%v)", last.File, err)
	}
	if relinks.Load() == 0 {
		t.Error("the fresh link was never asked for")
	}
}

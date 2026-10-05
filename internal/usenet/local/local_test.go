package local

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/nntp"
	"github.com/junkerderprovinz/knightloader/internal/nntp/nntptest"
	"github.com/junkerderprovinz/knightloader/internal/usenet"
	"github.com/junkerderprovinz/knightloader/internal/yenc"
)

// release is a file cut into articles, and the .nzb that lists them.
type release struct {
	name  string
	data  []byte
	parts []yenc.Part
	ids   []string
}

func newRelease(name string, size, partSize int) release {
	r := release{name: name, data: make([]byte, size)}
	rng := rand.New(rand.NewPCG(uint64(size), 7))
	for i := range r.data {
		r.data[i] = byte(rng.IntN(256))
	}
	total := (size + partSize - 1) / partSize
	for n := range total {
		from, to := n*partSize, min((n+1)*partSize, size)
		r.parts = append(r.parts, yenc.Part{Name: name, FileSize: int64(size), Number: n + 1, Total: total, Begin: int64(from), Data: r.data[from:to]})
		r.ids = append(r.ids, fmt.Sprintf("%s.%d@test", name, n+1))
	}
	return r
}

// post puts the articles numbered in which (all when none) on s.
func (r release) post(s *nntptest.Server, which ...int) {
	if len(which) == 0 {
		for i := range r.parts {
			which = append(which, i+1)
		}
	}
	for _, n := range which {
		s.AddPart(r.ids[n-1], r.parts[n-1])
	}
}

func nzbOf(rs ...release) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?><nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">`)
	for _, r := range rs {
		fmt.Fprintf(&b, `<file poster="t" date="%d" subject="&quot;%s&quot; yEnc (1/%d)"><groups><group>alt.binaries.test</group></groups><segments>`,
			time.Now().Unix(), r.name, len(r.parts))
		for i, id := range r.ids {
			fmt.Fprintf(&b, `<segment bytes="%d" number="%d">%s</segment>`, len(r.parts[i].Data)+100, i+1, id)
		}
		b.WriteString(`</segments></file>`)
	}
	b.WriteString(`</nzb>`)
	return []byte(b.String())
}

func serverFor(s *nntptest.Server, level int) nntp.Server {
	return nntp.Server{ID: s.Addr(), Host: s.Host, Port: s.Port, Connections: 3, Level: level}
}

// harness is a backend over one stored job, recording what it reports.
type harness struct {
	t       *testing.T
	svc     *Service
	be      *Backend
	dir     string
	job     string
	updates chan core.Update
}

func newHarness(t *testing.T, client *nntp.Client, rs ...release) *harness {
	t.Helper()
	h := &harness{t: t, dir: t.TempDir(), updates: make(chan core.Update, 1000)}
	h.svc = NewService(filepath.Join(t.TempDir(), "jobs"))
	job, err := h.svc.Submit(context.Background(), "test", nzbOf(rs...))
	if err != nil {
		t.Fatal(err)
	}
	h.job = job
	h.be = h.backend(client)
	t.Cleanup(client.Close)
	return h
}

func (h *harness) backend(client *nntp.Client) *Backend {
	be := NewBackend(h.svc, func() *nntp.Client { return client }, h.dir, func(_ string, u core.Update) { h.updates <- u })
	be.Wait = time.Millisecond
	return be
}

// run downloads file index of the job and returns the update that settled it.
func (h *harness) run(be *Backend, index int, name string) core.Update {
	h.t.Helper()
	be.Download("task", FileLink(h.job, index, name), nil, 0)
	deadline := time.After(20 * time.Second)
	for {
		select {
		case u := <-h.updates:
			if u.Status == core.StatusDone || u.Status == core.StatusError {
				return u
			}
		case <-deadline:
			h.t.Fatal("the download never settled")
		}
	}
}

func (h *harness) file(name string) []byte {
	h.t.Helper()
	b, err := os.ReadFile(filepath.Join(h.dir, name))
	if err != nil {
		h.t.Fatal(err)
	}
	return b
}

func TestDownloadAcrossLevelsAndTLS(t *testing.T) {
	r := newRelease("movie.mkv", 50_000, 4_000)
	main, fill := nntptest.NewTLS(t), nntptest.New(t)
	r.post(main, 1, 2, 3, 5, 6, 7, 8, 9, 10, 11, 12)
	r.post(fill, 4, 13)
	m := serverFor(main, 0)
	m.TLS, m.TLSConfig = true, main.ClientTLS
	h := newHarness(t, nntp.NewClient([]nntp.Server{m, serverFor(fill, 1)}, nil), r)

	u := h.run(h.be, 0, r.name)
	if u.Status != core.StatusDone || u.Size != int64(len(r.data)) {
		t.Fatalf("got %+v", u)
	}
	if !bytes.Equal(h.file("movie.mkv"), r.data) {
		t.Fatal("the file on disk differs from the one posted")
	}
	part := PartFile(h.dir, FileLink(h.job, 0, r.name), "task")
	for _, leftover := range []string{part, part + mapSuffix} {
		if _, err := os.Stat(leftover); err == nil {
			t.Errorf("%s is left behind", leftover)
		}
	}
}

func TestDamagedArticleIsFetchedAgain(t *testing.T) {
	r := newRelease("a.bin", 9_000, 3_000)
	s := nntptest.New(t)
	r.post(s)
	s.Damage(r.ids[1], 1)
	h := newHarness(t, nntp.NewClient([]nntp.Server{serverFor(s, 0)}, nil), r)
	if u := h.run(h.be, 0, r.name); u.Status != core.StatusDone {
		t.Fatalf("got %+v", u)
	}
	if !bytes.Equal(h.file("a.bin"), r.data) {
		t.Fatal("the damaged copy reached the disk")
	}
}

func TestMissingArticlesFailTheFileAndResumeLater(t *testing.T) {
	r := newRelease("b.bin", 20_000, 2_000)
	s := nntptest.New(t)
	r.post(s, 1, 2, 3, 4, 6, 7, 8, 9, 10)
	h := newHarness(t, nntp.NewClient([]nntp.Server{serverFor(s, 0)}, nil), r)
	var heard []int
	h.be.Incomplete = func(job string, missing int) bool {
		if job != h.job {
			t.Errorf("heard of job %s", job)
		}
		heard = append(heard, missing)
		return false
	}

	u := h.run(h.be, 0, r.name)
	if u.Status != core.StatusError || u.Reason != core.ReasonGone || !strings.Contains(u.Err, "1 of the 10 articles") {
		t.Fatalf("got %+v", u)
	}
	if len(heard) != 1 || heard[0] != 1 {
		t.Fatalf("Incomplete heard %v", heard)
	}

	// The article turns up later. A new backend, as after a restart, asks
	// only for it.
	r.post(s, 5)
	if u := h.run(h.backend(nntp.NewClient([]nntp.Server{serverFor(s, 0)}, nil)), 0, r.name); u.Status != core.StatusDone {
		t.Fatalf("got %+v", u)
	}
	for i, id := range r.ids {
		want := 1
		if i == 4 {
			want = 2
		}
		if n := s.Bodies(id); n != want {
			t.Errorf("article %d was asked for %d times, want %d", i+1, n, want)
		}
	}
	if !bytes.Equal(h.file("b.bin"), r.data) {
		t.Fatal("the resumed file differs from the one posted")
	}
}

func TestIncompleteFileHandedOnIsNotFailed(t *testing.T) {
	r := newRelease("c.bin", 4_000, 2_000)
	s := nntptest.New(t)
	r.post(s, 1)
	h := newHarness(t, nntp.NewClient([]nntp.Server{serverFor(s, 0)}, nil), r)
	handed := make(chan struct{})
	h.be.Incomplete = func(string, int) bool {
		close(handed)
		return true
	}
	h.be.Download("task", FileLink(h.job, 0, r.name), nil, 0)
	select {
	case <-handed:
	case <-time.After(10 * time.Second):
		t.Fatal("Incomplete was never called")
	}
	// Once the run has ended, nothing it reported may have settled the task.
	h.be.Halt("task")
	close(h.updates)
	for u := range h.updates {
		if u.Status == core.StatusError || u.Status == core.StatusDone {
			t.Fatalf("a file handed to another account settled as %+v", u)
		}
	}
}

func TestUnreachableServerFailsAsNetwork(t *testing.T) {
	r := newRelease("d.bin", 4_000, 2_000)
	s := nntptest.New(t)
	s.Close()
	h := newHarness(t, nntp.NewClient([]nntp.Server{serverFor(s, 0)}, nil), r)
	u := h.run(h.be, 0, r.name)
	if u.Status != core.StatusError || u.Reason != core.ReasonNetwork {
		t.Fatalf("got %+v", u)
	}
}

func TestPauseKeepsTheMapAndRemoveDeletesIt(t *testing.T) {
	r := newRelease("e.bin", 400_000, 4_000)
	s := nntptest.New(t)
	r.post(s)
	cfg := serverFor(s, 0)
	cfg.Connections = 1
	h := newHarness(t, nntp.NewClient([]nntp.Server{cfg}, slowCopy{}), r)
	h.be.Download("task", FileLink(h.job, 0, r.name), nil, 0)
	for u := range h.updates {
		if u.Loaded > 0 {
			break
		}
	}
	h.be.Halt("task")
	part := PartFile(h.dir, FileLink(h.job, 0, r.name), "task")
	m := loadSegMap(part+mapSuffix, len(r.parts))
	if m.count() == 0 || m.size != int64(len(r.data)) {
		t.Fatalf("the map after a pause has %d articles and size %d", m.count(), m.size)
	}
	h.be.Remove("task", true)
	for _, p := range []string{part, part + mapSuffix} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s is left after Remove", p)
		}
	}
}

func TestAnUndoneRemoveCarriesOnFromTheBytesItHad(t *testing.T) {
	r := newRelease("u.bin", 160_000, 4_000)
	s := nntptest.New(t)
	r.post(s)
	cfg := serverFor(s, 0)
	cfg.Connections = 1
	h := newHarness(t, nntp.NewClient([]nntp.Server{cfg}, slowCopy{}), r)
	link := FileLink(h.job, 0, r.name)
	h.be.Download("task", link, nil, 0)
	for u := range h.updates {
		if u.Loaded > 0 {
			break
		}
	}
	h.be.Remove("task", false)
	had := loadSegMap(PartFile(h.dir, link, "task")+mapSuffix, len(r.parts))
	if had.count() == 0 {
		t.Fatal("the part file and its map are gone after a remove without files")
	}

	for len(h.updates) > 0 {
		<-h.updates
	}
	// An undo hands the backend the same task again.
	h.be.Download("task", link, nil, 0)
	if u := <-h.updates; u.Loaded == 0 {
		t.Errorf("the undone download starts at %+v, want the bytes it had", u)
	}
	for u := range h.updates {
		if u.Status == core.StatusDone || u.Status == core.StatusError {
			break
		}
	}
	if !bytes.Equal(h.file(r.name), r.data) {
		t.Fatal("the undone download's file differs from the one posted")
	}
	for i, done := range had.done {
		if n := s.Bodies(r.ids[i]); done && n != 1 {
			t.Errorf("article %d was on disk before the remove and was fetched %d times", i+1, n)
		}
	}
}

// twin is another release of r's file name and article count, with other
// bytes under other message ids.
func twin(r release) release {
	o := release{name: r.name, data: make([]byte, len(r.data))}
	for i, b := range r.data {
		o.data[i] = ^b
	}
	for i, p := range r.parts {
		p.Data = o.data[p.Begin : p.Begin+int64(len(p.Data))]
		o.parts = append(o.parts, p)
		o.ids = append(o.ids, "twin."+r.ids[i])
	}
	return o
}

func TestTwoReleasesOfOneNameInOneFolderEachKeepTheirOwnBytes(t *testing.T) {
	first := newRelease("same.bin", 160_000, 4_000)
	second := twin(first)
	s := nntptest.New(t)
	first.post(s)
	second.post(s)
	cfg := serverFor(s, 0)
	cfg.Connections = 1
	client := nntp.NewClient([]nntp.Server{cfg}, slowCopy{})
	h := newHarness(t, client, first)
	job, err := h.svc.Submit(context.Background(), "twin", nzbOf(second))
	if err != nil {
		t.Fatal(err)
	}
	type report struct {
		id string
		u  core.Update
	}
	reports := make(chan report, 1000)
	be := NewBackend(h.svc, func() *nntp.Client { return client }, h.dir, func(id string, u core.Update) { reports <- report{id, u} })
	settled := func(id string) core.Update {
		t.Helper()
		deadline := time.After(20 * time.Second)
		for {
			select {
			case r := <-reports:
				if r.id == id && (r.u.Status == core.StatusDone || r.u.Status == core.StatusError) {
					return r.u
				}
			case <-deadline:
				t.Fatalf("%s never settled", id)
			}
		}
	}

	be.Download("first", FileLink(h.job, 0, first.name), nil, 0)
	for r := range reports {
		if r.u.Loaded > 0 {
			break
		}
	}
	be.Halt("first")
	be.Download("second", FileLink(job, 0, second.name), nil, 0)
	two := settled("second")
	be.Resume("first")
	one := settled("first")

	for _, c := range []struct {
		u    core.Update
		want []byte
	}{{one, first.data}, {two, second.data}} {
		got, err := os.ReadFile(c.u.File)
		if c.u.Status != core.StatusDone || err != nil || !bytes.Equal(got, c.want) {
			t.Errorf("%s ended %s and holds other bytes than its release (%v)", c.u.File, c.u.Status, err)
		}
	}
}

func TestServiceStatusHoldsRecoveryVolumes(t *testing.T) {
	svc := NewService(t.TempDir())
	rs := []release{newRelease("x.rar", 10, 10), newRelease("x.par2", 10, 10), newRelease("x.vol00+01.par2", 10, 10)}
	data := nzbOf(rs...)
	id, err := svc.Submit(context.Background(), "x", data)
	if err != nil {
		t.Fatal(err)
	}
	st, err := svc.Status(context.Background(), []string{id, "0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st["0123456789abcdef"]; ok {
		t.Fatal("an unknown job was reported")
	}
	got := st[id]
	if got.Phase != usenet.PhaseReady || len(got.Files) != 3 {
		t.Fatalf("got %+v", got)
	}
	for i, f := range got.Files {
		if f.Held != (i == 2) {
			t.Errorf("%s held: %v", f.Name, f.Held)
		}
		if f.Link != FileLink(id, i, f.Name) {
			t.Errorf("%s link: %s", f.Name, f.Link)
		}
	}
	back, err := svc.NZB(id)
	if err != nil || !bytes.Equal(back, data) {
		t.Fatalf("the .nzb did not come back as sent: %v", err)
	}
	if err := svc.Delete(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.NZB(id); err != ErrNotStored {
		t.Fatalf("got %v after Delete", err)
	}
	if _, err := svc.Submit(context.Background(), "x", []byte(`<nzb></nzb>`)); err == nil {
		t.Fatal("an empty .nzb was taken")
	}
}

func TestLinkRoundTrip(t *testing.T) {
	link := FileLink("0123456789abcdef", 3, "a b#c.rar")
	ref, err := parseLink(link)
	if err != nil || ref.job != "0123456789abcdef" || ref.index != 3 || ref.name != "a b#c.rar" {
		t.Fatalf("%s: %+v, %v", link, ref, err)
	}
	if !(Resolver{}).Match(link) || (Resolver{}).Match("usenet://torbox/1/2/x") || (Resolver{}).Match("nntp://job/x/name") {
		t.Fatal("the resolver claims the wrong links")
	}
}

func TestConcurrentFilesShareTheClient(t *testing.T) {
	a, b := newRelease("f1.bin", 30_000, 3_000), newRelease("f2.bin", 30_000, 3_000)
	s := nntptest.New(t)
	a.post(s)
	b.post(s)
	cfg := serverFor(s, 0)
	cfg.Connections = 4
	client := nntp.NewClient([]nntp.Server{cfg}, nil)
	h := newHarness(t, client, a, b)
	var mu sync.Mutex
	done := map[string]bool{}
	be := NewBackend(h.svc, func() *nntp.Client { return client }, h.dir, func(id string, u core.Update) {
		if u.Status == core.StatusDone {
			mu.Lock()
			done[id] = true
			mu.Unlock()
		}
	})
	be.Download("one", FileLink(h.job, 0, a.name), nil, 0)
	be.Download("two", FileLink(h.job, 1, b.name), nil, 0)
	deadline := time.Now().Add(20 * time.Second)
	for {
		mu.Lock()
		n := len(done)
		mu.Unlock()
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the two files did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s.MaxOpen() > 4 {
		t.Fatalf("%d connections for a server that allows 4", s.MaxOpen())
	}
	if !bytes.Equal(h.file("f1.bin"), a.data) || !bytes.Equal(h.file("f2.bin"), b.data) {
		t.Fatal("a file differs from the one posted")
	}
}

// slowCopy takes a while over every article, so a test can stop a download
// half way.
type slowCopy struct{}

func (slowCopy) Copy(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	n, err := io.Copy(dst, src)
	select {
	case <-ctx.Done():
	case <-time.After(20 * time.Millisecond):
	}
	return n, err
}

func TestStopWaitsForTheReportOfAFileThatJustFinished(t *testing.T) {
	r := newRelease("f.bin", 9_000, 3_000)
	s := nntptest.New(t)
	r.post(s)
	h := newHarness(t, nntp.NewClient([]nntp.Server{serverFor(s, 0)}, nil), r)
	reporting, saved := make(chan struct{}), make(chan struct{})
	be := NewBackend(h.svc, h.be.client, h.dir, func(id string, u core.Update) {
		if id == "task" && u.Status == core.StatusDone {
			close(reporting)
			<-saved
		}
	})
	be.Download("task", FileLink(h.job, 0, r.name), nil, 0)
	<-reporting

	stopped := make(chan struct{})
	go func() {
		be.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("Stop returned while the finished file was still being reported")
	case <-time.After(100 * time.Millisecond):
	}
	close(saved)
	<-stopped

	be.Download("again", FileLink(h.job, 0, r.name), nil, 0)
	be.mu.Lock()
	n := len(be.runs)
	be.mu.Unlock()
	if n != 0 {
		t.Error("a download started after Stop")
	}
}

func TestMoreConnectionsReachAFileUnderWay(t *testing.T) {
	r := newRelease("f.bin", 300_000, 3_000)
	s := nntptest.New(t)
	r.post(s)
	cfg := serverFor(s, 0)
	cfg.Connections = 1
	client := nntp.NewClient([]nntp.Server{cfg}, slowCopy{})
	h := newHarness(t, client, r)
	h.be.Download("task", FileLink(h.job, 0, r.name), nil, 0)
	for u := range h.updates {
		if u.Loaded > 0 {
			break
		}
	}

	cfg.Connections = 5
	client.SetServers([]nntp.Server{cfg})
	for {
		u := <-h.updates
		if u.Status == core.StatusError {
			t.Fatal(u.Err)
		}
		if u.Status == core.StatusDone {
			break
		}
	}
	if got := s.MaxOpen(); got != 5 {
		t.Errorf("the file used at most %d connections after the server allowed 5", got)
	}
}

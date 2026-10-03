package api

// An .nzb from Sonarr fetched from a Usenet server that runs in this process.

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/nntp/nntptest"
	"github.com/junkerderprovinz/knightloader/internal/settings"
	"github.com/junkerderprovinz/knightloader/internal/usenet"
	"github.com/junkerderprovinz/knightloader/internal/yenc"
)

// posting is a file cut into articles, and its entry in an .nzb.
type posting struct {
	name  string
	data  []byte
	parts []yenc.Part
}

func newPosting(name string, size int) posting {
	p := posting{name: name, data: make([]byte, size)}
	for i := range p.data {
		p.data[i] = byte(i*31 + len(name))
	}
	const partSize = 1000
	total := (size + partSize - 1) / partSize
	for n := range total {
		from, to := n*partSize, min((n+1)*partSize, size)
		p.parts = append(p.parts, yenc.Part{Name: name, FileSize: int64(size), Number: n + 1, Total: total, Begin: int64(from), Data: p.data[from:to]})
	}
	return p
}

func (p posting) id(n int) string { return fmt.Sprintf("%s.%d@kl.test", p.name, n) }

// post puts every article on s but the ones numbered in skip.
func (p posting) post(s *nntptest.Server, skip ...int) {
	for i, part := range p.parts {
		if !containsInt(skip, i+1) {
			s.AddPart(p.id(i+1), part)
		}
	}
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func nzbFor(ps ...posting) []byte {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?><nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">`)
	for _, p := range ps {
		fmt.Fprintf(&b, `<file poster="t" date="1700000000" subject="&quot;%s&quot; yEnc"><groups><group>a.b.test</group></groups><segments>`, p.name)
		for i := range p.parts {
			fmt.Fprintf(&b, `<segment bytes="1100" number="%d">%s</segment>`, i+1, p.id(i+1))
		}
		b.WriteString(`</segments></file>`)
	}
	b.WriteString(`</nzb>`)
	return b.Bytes()
}

// ownServerClient is downloadClientServer with s as the one Usenet server.
func ownServerClient(t *testing.T, s *nntptest.Server, tune func(*settings.Settings)) (*app.App, *httptest.Server, string) {
	t.Helper()
	a, srv, key := downloadClientServer(t, func(c *settings.Settings) {
		c.UsenetServers = []settings.UsenetServer{{ID: "main", Host: s.Host, Port: s.Port, Connections: 4, Enabled: true}}
		c.Extract = false
		if tune != nil {
			tune(c)
		}
	})
	user, pass := s.Login()
	if err := a.SetUsenetLogin("main", user, pass); err != nil {
		t.Fatal(err)
	}
	a.SetHalted(false)
	return a, srv, key
}

func historyRow(t *testing.T, srv *httptest.Server, key string) map[string]any {
	t.Helper()
	var row map[string]any
	waitUntil(t, "the grab to reach the history", func() bool {
		_, doc := sabGet(t, srv, key, map[string]string{"mode": "history", "category": "tv-sonarr"})
		if rows := slots(t, doc, "history"); len(rows) == 1 {
			row = rows[0]
			return true
		}
		return false
	})
	return row
}

func TestAnNZBFromSonarrIsFetchedFromTheOwnServer(t *testing.T) {
	t.Parallel()
	s := nntptest.New(t)
	s.SetLogin("reader", "secret")
	episode, index, spare := newPosting("show.s01e01.mkv", 5500), newPosting("show.par2", 300), newPosting("show.vol00+01.par2", 900)
	episode.post(s)
	index.post(s)
	a, srv, key := ownServerClient(t, s, nil)

	_, add := sabAddFile(t, srv, key, "Show.S01E01.nzb", "tv-sonarr", nzbFor(episode, index, spare))
	nzoID := nzoIDOf(t, add)
	row := historyRow(t, srv, key)
	if row["nzo_id"] != nzoID || row["status"] != "Completed" {
		t.Fatalf("history slot = %+v, want it completed", row)
	}
	got, err := os.ReadFile(filepath.Join(row["storage"].(string), "show.s01e01.mkv"))
	if err != nil || !bytes.Equal(got, episode.data) {
		t.Fatalf("the episode on disk holds %d bytes (%v), want the %d posted", len(got), err, len(episode.data))
	}

	// The recovery volume is a row of its own, held back and never fetched.
	var held *core.Task
	for _, task := range a.Tasks() {
		if task.Name == spare.name {
			held = task
		}
	}
	if held == nil || held.Enabled || !app.HeldSpare(held) {
		t.Fatalf("recovery volume = %+v, want it staged switched off", held)
	}
	if s.Bodies(spare.id(1)) != 0 {
		t.Fatal("the recovery volume was fetched")
	}
}

func TestAnNZBWithArticlesNoServerHasFailsInSonarrsHistory(t *testing.T) {
	t.Parallel()
	s := nntptest.New(t)
	episode := newPosting("show.s01e02.mkv", 4000)
	episode.post(s, 3)
	_, srv, key := ownServerClient(t, s, func(c *settings.Settings) { c.MaxRetries = 0 })

	sabAddFile(t, srv, key, "Show.S01E02.nzb", "tv-sonarr", nzbFor(episode))
	row := historyRow(t, srv, key)
	if row["status"] != "Failed" || !strings.Contains(row["fail_message"].(string), "1 of the 4 articles") {
		t.Fatalf("history slot = %+v, want Failed naming the missing article", row)
	}
}

func TestAnNZBTheOwnServerCannotCompleteGoesToTorBox(t *testing.T) {
	t.Parallel()
	s := nntptest.New(t)
	episode := newPosting("show.s01e03.mkv", 4000)
	episode.post(s, 2)
	fake := &torboxUsenet{}
	a, srv, key := ownServerClient(t, s, nil)
	a.SetUsenetServices(a.OwnUsenetServers(), usenet.NewTorBox(fake.start(t).URL, "torbox", "tb-key"))

	_, add := sabAddFile(t, srv, key, "Show.S01E03.nzb", "tv-sonarr", nzbFor(episode))
	nzoID := nzoIDOf(t, add)
	waitUntil(t, "the job to show TorBox's progress", func() bool {
		_, doc := sabGet(t, srv, key, map[string]string{"mode": "queue", "category": "tv-sonarr"})
		rows := slots(t, doc, "queue")
		return len(rows) == 1 && rows[0]["nzo_id"] == nzoID && rows[0]["percentage"] == float64(50)
	})
	fake.mu.Lock()
	sent := fake.got
	fake.mu.Unlock()
	if !bytes.Equal(sent, nzbFor(episode)) {
		t.Fatal("TorBox did not get the .nzb Sonarr uploaded")
	}
	waitUntil(t, "the own server's tasks to be removed", func() bool { return len(a.Tasks()) == 0 })
}

// taskNamed waits for a task called name in the given state and returns it.
func taskNamed(t *testing.T, a *app.App, name string, status core.Status) *core.Task {
	t.Helper()
	var found *core.Task
	waitUntil(t, name+" to be "+string(status), func() bool {
		for _, task := range a.Tasks() {
			if task.Name == name && task.Status == status {
				found = task
				return true
			}
		}
		return false
	})
	return found
}

func TestDeletingACopySavedUnderACountedNameKeepsTheOtherDownload(t *testing.T) {
	t.Parallel()
	s := nntptest.New(t)
	film := newPosting("film.bin", 3000)
	film.post(s)
	a, _, _ := ownServerClient(t, s, func(c *settings.Settings) { c.SubfolderByPackage = false })

	if _, err := a.AddNZB(app.NZB{Name: "Film", Package: "First", Data: nzbFor(film), Start: true}); err != nil {
		t.Fatal(err)
	}
	taskNamed(t, a, "film.bin", core.StatusDone)
	if _, err := a.AddNZB(app.NZB{Name: "Film", Package: "Second", Data: nzbFor(film), Start: true}); err != nil {
		t.Fatal(err)
	}
	second := taskNamed(t, a, "film (2).bin", core.StatusDone)

	a.RemoveTasks([]string{second.ID}, true)
	dir := a.Settings.Get().DownloadDir
	if _, err := os.Stat(filepath.Join(dir, "film (2).bin")); err == nil {
		t.Error("the deleted download's own file is still there")
	}
	if got, err := os.ReadFile(filepath.Join(dir, "film.bin")); err != nil || !bytes.Equal(got, film.data) {
		t.Fatalf("the other download's file holds %d bytes (%v), want it untouched", len(got), err)
	}
}

func TestDeletingAfterARestartTakesTheFilesAndTheirPartFiles(t *testing.T) {
	t.Parallel()
	s := nntptest.New(t)
	whole, broken := newPosting("whole.bin", 3000), newPosting("broken.bin", 4000)
	whole.post(s)
	broken.post(s, 2)
	dir, downloads := t.TempDir(), t.TempDir()
	open := func() *app.App {
		a, err := app.New(dir)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	a := open()
	cfg := settings.Defaults()
	cfg.DownloadDir, cfg.SubfolderByPackage, cfg.Crawl, cfg.Extract, cfg.MaxRetries = downloads, false, false, false, 0
	cfg.UsenetServers = []settings.UsenetServer{{ID: "main", Host: s.Host, Port: s.Port, Connections: 4, Enabled: true}}
	if _, err := a.ApplySettings(cfg); err != nil {
		t.Fatal(err)
	}
	a.SetHalted(false)
	if _, err := a.AddNZB(app.NZB{Name: "Pair", Data: nzbFor(whole, broken), Start: true}); err != nil {
		t.Fatal(err)
	}
	done := taskNamed(t, a, "whole.bin", core.StatusDone)
	failed := taskNamed(t, a, "broken.bin", core.StatusError)
	a.Close()

	b := open()
	t.Cleanup(func() { b.Close() })
	b.RemoveTasks([]string{done.ID, failed.ID}, true)
	for _, name := range []string{"whole.bin", "broken.bin.klpart", "broken.bin.klpart.segments"} {
		if _, err := os.Stat(filepath.Join(downloads, name)); err == nil {
			t.Errorf("%s is still on disk", name)
		}
	}
}

func TestAFileTheOwnServerFinishedStaysWhenTheJobGoesToTorBox(t *testing.T) {
	t.Parallel()
	s := nntptest.New(t)
	episode, sample := newPosting("show.s01e01.mkv", 2000), newPosting("show.s01e01.sample.bin", 3000)
	episode.post(s)
	sample.post(s, 2)
	fake := &torboxUsenet{}
	fake.ready.Store(true)
	// One download at a time, so the episode is whole before the sample
	// hands the job on.
	a, srv, key := ownServerClient(t, s, func(c *settings.Settings) { c.MaxConcurrent = 1 })
	a.SetUsenetServices(a.OwnUsenetServers(), usenet.NewTorBox(fake.start(t).URL, "torbox", "tb-key"))

	_, add := sabAddFile(t, srv, key, "Show.S01E01.nzb", "tv-sonarr", nzbFor(episode, sample))
	nzoID := nzoIDOf(t, add)
	row := historyRow(t, srv, key)
	if row["nzo_id"] != nzoID || row["status"] != "Completed" {
		t.Fatalf("history slot = %+v, want it completed", row)
	}
	got, err := os.ReadFile(filepath.Join(row["storage"].(string), "show.s01e01.mkv"))
	if err != nil || !bytes.Equal(got, episode.data) {
		t.Fatalf("the episode on disk holds %d bytes (%v), want the %d the own server sent", len(got), err, len(episode.data))
	}
	if tasks := a.Tasks(); len(tasks) != 1 || tasks[0].Name != episode.name {
		t.Fatalf("tasks = %d, want only the episode the own server finished", len(tasks))
	}
}

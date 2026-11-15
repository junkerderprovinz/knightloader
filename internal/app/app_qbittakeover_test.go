package app

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/crossseed"
	"github.com/junkerderprovinz/knightloader/internal/qbittorrent"
	"github.com/junkerderprovinz/knightloader/internal/qbittorrent/qbittest"
	"github.com/junkerderprovinz/knightloader/internal/torrents"
	"github.com/junkerderprovinz/knightloader/internal/torrents/torrentstest"
)

const (
	hashTask    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hashTwin    = "cccccccccccccccccccccccccccccccccccccccc"
	hashOrphan  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	hashLeft    = "dddddddddddddddddddddddddddddddddddddddd"
	hashStray   = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	hashForeign = "ffffffffffffffffffffffffffffffffffffffff"
)

// takeOverRig is an app with one torrent task, and a qBittorrent in memory as
// it stands right after a start: every torrent it restored runs.
type takeOverRig struct {
	a  *App
	q  *qbittest.Server
	c  *qbittorrent.Client
	dl string
}

func takeOverApp(t *testing.T) *takeOverRig {
	t.Helper()
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	dl := t.TempDir()
	task := &core.Task{ID: "t1", Name: "Album", Resolver: "torrent", Status: core.StatusDone, InfoHash: strings.ToUpper(hashTask), File: filepath.Join(dl, "Album")}
	a.mu.Lock()
	a.tasks[task.ID] = task
	a.mu.Unlock()
	a.xsAsked.Note("OPS", hashTask, crossseed.Answer{Result: crossseed.ResultInjected, InfoHash: hashTwin})

	q := qbittest.New("knightloader", "secret")
	srv := httptest.NewServer(q)
	t.Cleanup(srv.Close)
	c, err := qbittorrent.New(srv.URL, "knightloader", "secret")
	if err != nil {
		t.Fatal(err)
	}
	return &takeOverRig{a: a, q: q, c: c, dl: dl}
}

func (r *takeOverRig) put(hash, tags, dir string) {
	r.q.Put(qbittest.Torrent{Hash: hash, Tags: tags, State: "stalledUP", SavePath: dir})
}

func TestAtItsStartQBittorrentRunsOnlyWhatKnightLoaderWants(t *testing.T) {
	r := takeOverApp(t)
	r.put(hashTask, torrents.Tag, r.dl)
	r.put(hashTwin, torrents.Tag+","+CrossSeedTag, r.dl)
	r.put(hashOrphan, torrents.Tag, r.dl)
	r.put(hashStray, torrents.Tag+","+CrossSeedTag, filepath.Join(r.dl, "links"))
	r.put(hashForeign, "", r.dl)
	if err := r.a.takeOverTorrents(context.Background(), r.c); err != nil {
		t.Fatal(err)
	}

	if got := r.q.Get(hashTask); got == nil || !strings.HasPrefix(got.State, "paused") {
		t.Errorf("the task's torrent, which nothing runs yet, is %+v", got)
	}
	if got := r.q.Get(hashTwin); got == nil || strings.HasPrefix(got.State, "paused") {
		t.Errorf("the twin of a task on the list is %+v", got)
	}
	for _, h := range []string{hashOrphan, hashStray} {
		if r.q.Get(h) != nil {
			t.Errorf("%s, whose task is gone, is still in qBittorrent", h)
		}
	}
	if got := r.q.Get(hashForeign); got == nil || got.State != "stalledUP" {
		t.Errorf("a torrent not of KnightLoader's is %+v", got)
	}
	if got := r.q.DeletedWithFiles(); len(got) != 0 {
		t.Errorf("deleted with files: %v", got)
	}
}

func TestATwinLeftWhileQBittorrentWasAwayGoesWithItsLinksAtTheNextStart(t *testing.T) {
	r := takeOverApp(t)
	gone := &core.Task{ID: "t2", Name: "Gone", Resolver: "torrent", InfoHash: hashOrphan, File: filepath.Join(r.dl, "Gone")}
	r.a.xsAsked.Note("RED", hashOrphan, crossseed.Answer{Result: crossseed.ResultInjected, InfoHash: hashLeft})
	r.a.xsAsked.Note("OPS", hashOrphan, crossseed.Answer{Result: crossseed.ResultInjected, InfoHash: hashStray})
	off := torrentstest.New()
	off.FailNext(torrents.ErrOff)
	r.a.UseTorrents(off)
	r.a.dropCrossSeedsNow(context.Background(), gone, true)

	r.put(hashLeft, torrents.Tag+","+CrossSeedTag, filepath.Join(r.dl, "links", "RED"))
	r.put(hashStray, torrents.Tag+","+CrossSeedTag, r.dl)
	if err := r.a.takeOverTorrents(context.Background(), r.c); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{hashLeft, hashStray} {
		if r.q.Get(h) != nil {
			t.Errorf("%s is still in qBittorrent", h)
		}
	}
	if got := r.q.DeletedWithFiles(); !slices.Equal(got, []string{hashLeft}) {
		t.Errorf("deleted with files: %v; want only the twin that seeded from links", got)
	}
	if left, err := r.a.leftTorrents.load(); err != nil || len(left) != 0 {
		t.Errorf("still to take out: %v, %v", left, err)
	}
}

func TestTheTorrentsTheEngineRunsStartAgainWithQBittorrent(t *testing.T) {
	r := takeOverApp(t)
	r.put(hashTask, torrents.Tag, r.dl)
	r.a.runningTorrents = func() []string { return []string{hashTask} }
	if err := r.a.takeOverTorrents(context.Background(), r.c); err != nil {
		t.Fatal(err)
	}
	if got := r.q.Get(hashTask); got == nil || strings.HasPrefix(got.State, "paused") {
		t.Errorf("a torrent the engine runs is %+v", got)
	}
}

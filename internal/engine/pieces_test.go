package engine

import (
	"reflect"
	"testing"
	"time"

	"github.com/GopeedLab/gopeed/pkg/download"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

func covered(pm core.PieceMap) int64 {
	var n int64
	for _, r := range pm.Runs {
		n += r.Len
	}
	return n
}

// Every connection that is receiving writes where its range has got to. One
// that is still connecting, has finished or has failed writes nowhere, and a
// failed one leaves its range missing.
func TestASavedLayoutIsTheWrittenBytesAndAHeadPerReceivingConnection(t *testing.T) {
	raw := []byte(`{"Connections":[
		{"ID":0,"State":3,"Chunk":{"Begin":0,"End":399,"Downloaded":400},"Downloaded":400,"Completed":true},
		{"ID":1,"State":2,"Chunk":{"Begin":400,"End":599,"Downloaded":50},"Downloaded":50},
		{"ID":2,"State":1,"Chunk":{"Begin":600,"End":799,"Downloaded":0},"Downloaded":0},
		{"ID":3,"State":4,"Chunk":{"Begin":800,"End":899,"Downloaded":20},"Downloaded":20},
		{"ID":4,"State":2,"Chunk":{"Begin":900,"End":999,"Downloaded":60},"Downloaded":60}
	]}`)

	got, ok := layoutPieces(raw, 1000, true)

	if !ok {
		t.Fatal("no map of a layout with connections")
	}
	want := core.PieceMap{
		Unit:  core.UnitBytes,
		Total: 1000,
		Runs: []core.PieceRun{
			{State: core.PieceDone, Len: 450},
			{State: core.PieceMissing, Len: 350},
			{State: core.PieceDone, Len: 20},
			{State: core.PieceMissing, Len: 80},
			{State: core.PieceDone, Len: 60},
			{State: core.PieceMissing, Len: 40},
		},
		Heads: []int64{450, 960},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("map = %+v\nwant  %+v", got, want)
	}
}

func TestAnUnrangedTransferWritesAtTheEndOfWhatItHas(t *testing.T) {
	raw := []byte(`{"Connections":[{"ID":0,"State":2,"Chunk":{"Begin":0,"End":0,"Downloaded":300},"Downloaded":300}]}`)

	got, ok := layoutPieces(raw, 1000, false)

	want := core.PieceMap{
		Unit:  core.UnitBytes,
		Total: 1000,
		Runs:  []core.PieceRun{{State: core.PieceDone, Len: 300}, {State: core.PieceMissing, Len: 700}},
		Heads: []int64{300},
	}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("map = %+v, %v\nwant  %+v", got, ok, want)
	}
}

func TestALayoutWithoutConnectionsSaysNothing(t *testing.T) {
	if _, ok := layoutPieces([]byte(`{"Connections":[]}`), 1000, true); ok {
		t.Error("a map of a transfer that has no connection yet")
	}
}

// The whole path through the library: the first question starts the watch,
// and the next save answers with ranges that add up to the file and a head on
// a range that is still open.
func TestARunningDownloadShowsWhereItsConnectionsWrite(t *testing.T) {
	e, _, _, _ := startSlow(t)

	if pm, ok := e.Pieces("unknown"); ok {
		t.Errorf("a map of a task the engine does not run: %+v", pm)
	}
	var pm core.PieceMap
	waitUntil(t, "a map with a head", func() bool {
		var ok bool
		pm, ok = e.Pieces("t1")
		return ok && len(pm.Heads) > 0
	})

	if pm.Unit != core.UnitBytes || pm.Total != 16<<20 {
		t.Errorf("unit %q, total %d", pm.Unit, pm.Total)
	}
	if n := covered(pm); n != pm.Total {
		t.Errorf("the runs cover %d of %d bytes", n, pm.Total)
	}
	// Each head stands on the first byte of a missing run.
	at := int64(0)
	open := map[int64]bool{}
	for _, r := range pm.Runs {
		if r.State == core.PieceMissing {
			open[at] = true
		}
		at += r.Len
	}
	for _, h := range pm.Heads {
		if !open[h] {
			t.Errorf("a head at byte %d, where nothing is missing (runs %+v)", h, pm.Runs)
		}
	}
}

// A mend knows its gaps to the byte and fetches the first of them.
func TestAMendShowsTheRangesItStillFetches(t *testing.T) {
	e, _ := mendingEngine(t, Job{})
	e.mu.Lock()
	e.mends["t1"] = &mend{size: 1000, gaps: []span{{500, 699}, {900, 999}}, cancel: func() {}}
	e.mu.Unlock()

	got, ok := e.Pieces("t1")

	want := core.PieceMap{
		Unit:  core.UnitBytes,
		Total: 1000,
		Runs: []core.PieceRun{
			{State: core.PieceDone, Len: 500},
			{State: core.PieceMissing, Len: 200},
			{State: core.PieceDone, Len: 200},
			{State: core.PieceMissing, Len: 100},
		},
		Heads: []int64{500},
	}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("map = %+v, %v\nwant  %+v", got, ok, want)
	}
	e.mu.Lock()
	delete(e.mends, "t1")
	e.mu.Unlock()
}

// Once nobody has asked about a task for layoutWatch, its saves are no longer
// copied, whether or not another task is asked about meanwhile.
func TestALayoutNobodyAsksAboutIsNoLongerCopied(t *testing.T) {
	s := &layoutStore{Storage: download.NewMemStorage(), want: map[string][]byte{}, files: map[string][]int64{}, watched: map[string]*watchedLayout{}}
	s.watched["gid"] = &watchedLayout{asked: time.Now().Add(-layoutWatch - time.Second)}

	if err := s.Put(savedLayoutBucket, "gid", savedLayout{}); err != nil {
		t.Fatal(err)
	}
	if w, ok := s.watched["gid"]; ok {
		t.Fatalf("a save copied the layout of a task nobody asked about for %s: %q", layoutWatch, w.raw)
	}
}

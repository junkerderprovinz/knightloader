package app

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/par2"
	"github.com/junkerderprovinz/knightloader/internal/usenet/local"
)

// A file a par2 check found damaged shows its blocks with the bad ones
// marked, for as long as the release waits for its repair.
func TestADamagedFileShowsTheBlocksItsCheckFound(t *testing.T) {
	a := newCrawlApp(t, false)
	const job = "0123456789abcdef"
	file := filepath.Join(t.TempDir(), "show.mkv")
	set := &par2.Set{Files: []par2.File{{Checks: make([]par2.Check, 6)}, {Checks: make([]par2.Check, 2)}}}
	rep := &par2.Report{Files: []par2.FileReport{{Path: file, Bad: []int{1, 2, 5}}, {}}}
	a.mu.Lock()
	a.tasks["ep"] = &core.Task{
		ID: "ep", URL: local.FileLink(job, 0, "show.mkv"), Name: "show.mkv", Status: core.StatusDone,
		Repair: &core.RepairProgress{Stage: core.RepairRepairing, Damaged: 3, Recovery: 4},
	}
	a.usenetStateFor().checks[job] = &usenetCheck{remote: job, blocks: blockMaps(set, rep, []checkFile{{id: "ep", path: file}})}
	a.mu.Unlock()

	got, ok := a.Pieces("ep")

	want := core.PieceMap{
		Unit:  core.UnitBlocks,
		Total: 6,
		Runs: []core.PieceRun{
			{State: core.PieceDone, Len: 1},
			{State: core.PieceLost, Len: 2},
			{State: core.PieceDone, Len: 2},
			{State: core.PieceLost, Len: 1},
		},
	}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("map = %+v, %v\nwant  %+v", got, ok, want)
	}

	a.mu.Lock()
	a.tasks["ep"].Repair = nil
	a.mu.Unlock()
	if pm, ok := a.Pieces("ep"); ok {
		t.Errorf("a file whose release has passed still shows blocks: %+v", pm)
	}
}

func TestATaskNobodyFetchesHasNoPieceMap(t *testing.T) {
	a := newCrawlApp(t, false)
	a.mu.Lock()
	a.tasks["q"] = &core.Task{ID: "q", URL: "https://host.example/f.bin", Status: core.StatusQueued, Resolver: "direct"}
	a.mu.Unlock()

	for _, id := range []string{"q", "nobody"} {
		if pm, ok := a.Pieces(id); ok {
			t.Errorf("task %s has a map: %+v", id, pm)
		}
	}
}

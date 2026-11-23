package app

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/parleyport/kit/fortress"

	"github.com/junkerderprovinz/knightloader/internal/afterdownload"
	"github.com/junkerderprovinz/knightloader/internal/script"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// engineFortress holds one ArrowLoop instance that starts every job it is
// asked for and remembers which.
type engineFortress struct {
	mu   sync.Mutex
	runs []string
}

func (f *engineFortress) Members() []fortress.Member {
	return []fortress.Member{{ID: "m-tower", App: fortress.App{ID: afterdownload.EngineApp}, Device: "Tower", Actions: afterdownload.Uses}}
}

func (f *engineFortress) Act(_ context.Context, id, action string, body []byte) ([]byte, error) {
	if id != "m-tower" || action != afterdownload.RunAction {
		return nil, fortress.ErrNoSuchAction
	}
	var req struct{ Job string }
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.runs = append(f.runs, req.Job)
	f.mu.Unlock()
	return json.Marshal(map[string]string{"job": req.Job, "status": afterdownload.StatusStarted})
}

func (f *engineFortress) started() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.runs)
}

func stationApp(t *testing.T) (*App, *engineFortress) {
	t.Helper()
	a, _ := newRuleApp(t, func(s *settings.Settings, _ string) {
		s.Categories = []settings.Category{
			{ID: "serien", AfterDownload: &settings.AfterDownload{Member: "m-tower", Device: "Tower", Job: "series"}},
			{ID: "extras", AfterDownload: &settings.AfterDownload{Member: "m-tower", Device: "Tower", Job: "series"}},
			{ID: "filme", AfterDownload: &settings.AfterDownload{Member: "m-tower", Device: "Tower", Job: "films"}},
			{ID: "musik"},
		}
	})
	f := &engineFortress{}
	a.SetFortress(f)
	return a, f
}

func packageDone(a *App, pkg string) {
	a.publishEvent(script.Firing{Trigger: script.TriggerPackageDone, Package: &script.PackageView{Name: pkg, Files: 1, Done: 1}})
}

func TestAFinishedPackageStartsTheJobItsDrawerNames(t *testing.T) {
	t.Parallel()
	a, f := stationApp(t)
	stageFiled(t, a, "1", "e01.mkv", "Die.Serie.S01", "serien")

	packageDone(a, "Die.Serie.S01")

	var last afterdownload.Result
	waitFor(t, "the hand-over being recorded", func() bool {
		var ok bool
		last, ok = a.LastAfterDownload("serien")
		return ok
	})
	if last.Status != afterdownload.StatusStarted || last.Job != "series" || last.Package != "Die.Serie.S01" {
		t.Errorf("LastAfterDownload = %+v", last)
	}
	if got := f.started(); !slices.Equal(got, []string{"series"}) {
		t.Errorf("ArrowLoop was asked %v", got)
	}
}

// The sample beside the episode sits in another drawer; both drawers name the
// same job, which starts once, and the film drawer's job starts as well.
func TestAPackageInSeveralDrawersStartsEachJobOnce(t *testing.T) {
	t.Parallel()
	a, f := stationApp(t)
	stageFiled(t, a, "1", "e01.mkv", "Die.Serie.S01", "serien")
	stageFiled(t, a, "2", "extra.mkv", "Die.Serie.S01", "extras")
	stageFiled(t, a, "3", "sample.mkv", "Die.Serie.S01", "filme")
	stageFiled(t, a, "4", "album.zip", "Das.Album", "musik")

	packageDone(a, "Die.Serie.S01")
	packageDone(a, "Das.Album")

	waitFor(t, "both jobs starting", func() bool { return len(f.started()) >= 2 })
	time.Sleep(200 * time.Millisecond)
	got := f.started()
	slices.Sort(got)
	if !slices.Equal(got, []string{"films", "series"}) {
		t.Errorf("ArrowLoop was asked %v, want films and series once each", got)
	}
	if _, ok := a.LastAfterDownload("musik"); ok {
		t.Error("a drawer without a station handed something over")
	}
}

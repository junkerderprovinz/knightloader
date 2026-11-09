package fortress

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/junkerderprovinz/parleyport/kit/status"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

func source(snap Snapshot) Source {
	return Source{Events: NewLog(10), Read: func(context.Context) (Snapshot, error) { return snap, nil }}
}

func TestReportListsRunningAndUnpackingDownloads(t *testing.T) {
	snap := Snapshot{Inputs: Inputs{Now: now, Tasks: []*core.Task{
		{ID: "r", Name: "a.mkv", Status: core.StatusRunning, Enabled: true, Size: 400, Loaded: 100, CreatedAt: now},
		{ID: "u", Name: "b.mkv", Status: core.StatusRunning, Enabled: true},
		{ID: "x", Name: "c.rar", Status: core.StatusExtracting, Enabled: true},
		{ID: "q", Name: "d.mkv", Status: core.StatusQueued, Enabled: true},
	}}}
	rep, err := source(snap).Report(context.Background(), "en")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Work) != 3 {
		t.Fatalf("work = %+v", rep.Work)
	}
	r, u, x := rep.Work[0], rep.Work[1], rep.Work[2]
	if r.ID != "task:r" || r.Kind != status.KindDownload || r.Title != "a.mkv" || r.Progress == nil || *r.Progress != 0.25 || r.BlocksRestart || !r.StartedAt.Equal(now) {
		t.Errorf("running = %+v", r)
	}
	if u.Progress != nil {
		t.Errorf("a download of unknown size has progress %v", *u.Progress)
	}
	if x.Kind != status.KindExtract || !x.BlocksRestart {
		t.Errorf("unpacking = %+v", x)
	}
}

func TestReportCapsTheWorkList(t *testing.T) {
	var tasks []*core.Task
	for i := range maxWork + 5 {
		tasks = append(tasks, &core.Task{ID: strconv.Itoa(i), Status: core.StatusRunning, Enabled: true})
	}
	rep, _ := source(Snapshot{Inputs: Inputs{Now: now, Tasks: tasks}}).Report(context.Background(), "en")
	if len(rep.Work) != maxWork {
		t.Errorf("work has %d entries, want %d", len(rep.Work), maxWork)
	}
}

func TestReportStateFollowsTheHealthReport(t *testing.T) {
	for health, want := range map[string]status.State{"ok": status.StateOK, "degraded": status.StateWarning, "failed": status.StateFailed, "": status.StateOK} {
		rep, _ := source(Snapshot{Health: health, Inputs: Inputs{Now: now}}).Report(context.Background(), "en")
		if rep.State != want {
			t.Errorf("health %q: state %s, want %s", health, rep.State, want)
		}
	}
}

func TestReportPassesAReadError(t *testing.T) {
	boom := errors.New("boom")
	s := Source{Events: NewLog(1), Read: func(context.Context) (Snapshot, error) { return Snapshot{}, boom }}
	if _, err := s.Report(context.Background(), "en"); !errors.Is(err, boom) {
		t.Errorf("err = %v, want boom", err)
	}
}

func TestReportPutsNeedsInTheLanguage(t *testing.T) {
	snap := Snapshot{Inputs: Inputs{Now: now, Tasks: []*core.Task{failedTask("t1", "")}}}
	rep, _ := source(snap).Report(context.Background(), "de")
	if len(rep.NeedsYou) != 1 || rep.NeedsYou[0].Text != "t1.mkv ist fehlgeschlagen" {
		t.Errorf("needsYou = %+v", rep.NeedsYou)
	}
}

package afterdownload

import (
	"context"
	"testing"
	"time"

	"github.com/junkerderprovinz/parleyport/kit/fortress"
)

func photosAndInbox() []fakeJob {
	at := time.Date(2026, 11, 10, 20, 0, 0, 0, time.UTC)
	return []fakeJob{
		{Name: "photos", Direction: "both", Mode: "sync", LastRun: &LastRun{At: at, Outcome: "done"}},
		{Name: "inbox", Direction: "leftToRight", Mode: "move", Paused: true},
	}
}

func TestEnginesListsTheArrowLoopInstancesWithTheirJobs(t *testing.T) {
	f := newFakeFortress()
	f.addEngine("m-tower", "Tower", photosAndInbox()...)
	f.addEngine("m-attic", "Attic")
	f.members = append(f.members,
		fortress.Member{ID: "m-vault", App: fortress.App{ID: "bombvault"}, Device: "Tower", Actions: []string{"backup.export"}},
		fortress.Member{ID: "m-old", App: fortress.App{ID: EngineApp}, Device: "Cellar"},
	)

	engines := Engines(context.Background(), f)
	if len(engines) != 2 {
		t.Fatalf("listed %+v, want Attic and Tower", engines)
	}
	attic, tower := engines[0], engines[1]
	if attic.ID != "m-attic" || attic.Device != "Attic" || attic.Jobs == nil || len(attic.Jobs) != 0 || attic.Error != "" {
		t.Errorf("Attic listed as %+v", attic)
	}
	if tower.ID != "m-tower" || tower.Device != "Tower" || len(tower.Jobs) != 2 || tower.Error != "" {
		t.Fatalf("Tower listed as %+v", tower)
	}
	photos, inbox := tower.Jobs[0], tower.Jobs[1]
	if photos.Name != "photos" || photos.Mode != "sync" || photos.Direction != "both" || photos.Paused ||
		photos.LastRun == nil || photos.LastRun.Outcome != "done" {
		t.Errorf("photos listed as %+v", photos)
	}
	if inbox.Name != "inbox" || inbox.Mode != "move" || !inbox.Paused || inbox.LastRun != nil {
		t.Errorf("inbox listed as %+v", inbox)
	}
}

func TestAnEngineThatDoesNotAnswerIsListedWithWhy(t *testing.T) {
	f := newFakeFortress()
	f.addEngine("m-tower", "Tower", photosAndInbox()...)
	f.setUnreachable("m-tower", true)

	engines := Engines(context.Background(), f)
	if len(engines) != 1 || engines[0].Error != CodeUnreachable || len(engines[0].Jobs) != 0 {
		t.Fatalf("listed %+v, want Tower as unreachable", engines)
	}
}

func TestWithoutAFortressThereAreNoEngines(t *testing.T) {
	if engines := Engines(context.Background(), nil); engines == nil || len(engines) != 0 {
		t.Errorf("listed %+v", engines)
	}
}

func TestRunStartsTheJobAndSaysWhenItWasQueued(t *testing.T) {
	f := newFakeFortress()
	e := f.addEngine("m-tower", "Tower", photosAndInbox()...)

	status, err := Run(context.Background(), f, Target{Member: "m-tower", Job: "photos"})
	if err != nil || status != StatusStarted {
		t.Fatalf("Run = %q, %v", status, err)
	}
	status, err = Run(context.Background(), f, Target{Member: "m-tower", Job: "photos"})
	if err != nil || status != StatusQueued {
		t.Fatalf("Run while it runs = %q, %v", status, err)
	}
	if len(e.runs) != 2 {
		t.Errorf("the engine was asked %v", e.runs)
	}
}

func TestRunSaysWhyItCouldNotStartTheJob(t *testing.T) {
	f := newFakeFortress()
	f.addEngine("m-tower", "Tower", photosAndInbox()...)
	f.addEngine("m-attic", "Attic")
	f.setUnreachable("m-attic", true)

	for _, c := range []struct {
		name string
		f    Fortress
		t    Target
		off  string
		want string
	}{
		{"unknown job", f, Target{Member: "m-tower", Job: "outbox"}, "", CodeNoSuchJob},
		{"paused job", f, Target{Member: "m-tower", Job: "inbox"}, "", CodeJobPaused},
		{"permission off", f, Target{Member: "m-tower", Job: "photos"}, RunAction, CodePermissionOff},
		{"unreachable", f, Target{Member: "m-attic", Job: "photos"}, "", CodeUnreachable},
		{"left the fortress", f, Target{Member: "m-gone", Job: "photos"}, "", CodeGone},
		{"no fortress", nil, Target{Member: "m-tower", Job: "photos"}, "", CodeNoFortress},
	} {
		t.Run(c.name, func(t *testing.T) {
			f.off = map[string]bool{c.off: c.off != ""}
			status, err := Run(context.Background(), c.f, c.t)
			if err == nil || Code(err) != c.want {
				t.Errorf("Run = %q, %v (%s), want %s", status, err, Code(err), c.want)
			}
		})
	}
}

func TestAPermissionArrowLoopDoesNotListReadsAsOff(t *testing.T) {
	if got := Code(fortress.ErrNotUsed); got != CodePermissionOff {
		t.Errorf("Code(ErrNotUsed) = %q, want %q", got, CodePermissionOff)
	}
}

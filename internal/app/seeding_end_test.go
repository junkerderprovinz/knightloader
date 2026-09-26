package app

import (
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

func TestWhenATorrentStopsSeedingIsKeptAcrossARestart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	finished := time.Now().Add(-2 * time.Hour)
	seed := func(id string) *core.Task {
		return &core.Task{
			ID: id, URL: "magnet:?xt=urn:btih:" + id, Name: id, Resolver: "torrent",
			Status: core.StatusDone, Enabled: true, FinishedAt: finished, Seeding: true,
		}
	}
	atTarget, atShutdown := seed("0123456789abcdef0123456789abcdef01234567"), seed("fedcba9876543210fedcba9876543210fedcba98")
	a.mu.Lock()
	a.tasks[atTarget.ID], a.tasks[atShutdown.ID] = atTarget, atShutdown
	a.mu.Unlock()
	for _, task := range []core.Task{*atTarget, *atShutdown} {
		if err := a.Store.Save(&task); err != nil {
			t.Fatal(err)
		}
	}

	before := time.Now().Add(-time.Second)
	// The poll that finds the first one has reached its targets.
	a.onUpdate(atTarget.ID, core.Update{Torrent: &core.TorrentStats{Seeding: false}})
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	byID := map[string]*core.Task{}
	for _, task := range again.Tasks() {
		byID[task.ID] = task
	}
	for name, id := range map[string]string{"at its targets": atTarget.ID, "at the shutdown": atShutdown.ID} {
		task := byID[id]
		if task == nil {
			t.Fatalf("the torrent that stopped seeding %s is not in the list after the restart", name)
		}
		if task.SeedingEnded.Before(before) {
			t.Errorf("the torrent that stopped seeding %s reads as ending at %v, want the moment it stopped", name, task.SeedingEnded)
		}
		if task.Seeding {
			t.Errorf("the torrent that stopped seeding %s still reads as seeding", name)
		}
	}
}

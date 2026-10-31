//go:build updatetest

package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/store"
)

func TestAFreshTestInstallStartsWithSampleRows(t *testing.T) {
	dir := t.TempDir()
	seedSamples(dir, nil)

	st, err := store.Open(filepath.Join(dir, "knightloader.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	tasks, err := st.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != len(samples) {
		t.Fatalf("%d rows, want %d", len(tasks), len(samples))
	}
	states := map[core.Status]int{}
	packages := map[core.Status]map[string]bool{}
	for _, task := range tasks {
		states[task.Status]++
		if packages[task.Status] == nil {
			packages[task.Status] = map[string]bool{}
		}
		packages[task.Status][task.Package] = true
	}
	if len(packages[core.StatusCollected]) != 2 {
		t.Errorf("collector packages %v, want two", packages[core.StatusCollected])
	}
	for _, s := range []core.Status{core.StatusDone, core.StatusError, core.StatusQueued} {
		if states[s] == 0 {
			t.Errorf("no %s row", s)
		}
	}
	for _, s := range []core.Status{core.StatusRunning, core.StatusPaused} {
		if states[s] != 0 {
			t.Errorf("%d %s rows; nothing may start by itself", states[s], s)
		}
	}
}

func TestSamplesLeaveAListThatExistsAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "knightloader.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	mine := &core.Task{ID: "mine", URL: "https://example.com/a.bin", Status: core.StatusDone, Enabled: true, CreatedAt: time.Now()}
	if err := st.Save(mine); err != nil {
		t.Fatal(err)
	}
	st.Close()

	seedSamples(dir, nil)

	st, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	tasks, err := st.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != "mine" {
		t.Errorf("the list holds %d rows after the seed, want only its own", len(tasks))
	}
}

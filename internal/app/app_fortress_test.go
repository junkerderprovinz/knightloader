package app

import (
	"context"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
)

// A backup app finds the member that runs in a container by this id, and
// only then stores the member's own backup with the container's.
func TestTheStatusNamesTheContainerKnightLoaderRunsIn(t *testing.T) {
	containerID := buildinfo.ContainerID
	t.Cleanup(func() { buildinfo.ContainerID = containerID })
	a, _ := newImportApp(t)

	for _, id := range []string{"e8fc22d4db7b5d0c1f3a9e6b2c4d8f0a1b3c5d7e9f0a2b4c6d8e0f1a3b5c7d9e", ""} {
		buildinfo.ContainerID = func() string { return id }
		snap, err := a.FortressSnapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if snap.Instance.Container != id {
			t.Errorf("the status names the container %q, want %q", snap.Instance.Container, id)
		}
	}
}

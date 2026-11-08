package hoster

import (
	"context"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

type idleTransfers struct{}

func (idleTransfers) Pause(string)        {}
func (idleTransfers) Resume(string)       {}
func (idleTransfers) Remove(string, bool) {}

func TestARunPausedAfterTheHosterAnsweredDoesNotHandItsLinkOn(t *testing.T) {
	answered := make(chan struct{})
	paused := make(chan struct{})
	handed := make(chan bool, 1)
	var runs *Runs
	runs = NewRuns(idleTransfers{}, func(string, core.Update) {}, func(ctx context.Context, taskID, _ string, _ int) {
		close(answered)
		<-paused
		handed <- runs.Handing(ctx, taskID)
	}, Sleep)
	t.Cleanup(func() { runs.Remove("t1", false) })

	runs.Download("t1", "https://example.com/file", nil, 1)
	<-answered
	runs.Pause("t1")
	close(paused)
	if <-handed {
		t.Error("a paused run handed its link to the engine")
	}
}

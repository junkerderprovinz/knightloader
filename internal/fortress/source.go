// Package fortress is how this instance answers the other members of a
// fortress and the dashboards that watch it: the status report, with what
// needs the person and what is running, and a log of what happened. The
// contract itself, its paths and its checks, come from parleyport's kit.
package fortress

import (
	"context"
	"fmt"

	"github.com/junkerderprovinz/parleyport/kit/status"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// maxWork bounds the running work in a report, which has to stay under the
// contract's 64 KiB with long file names.
const maxWork = 50

// Snapshot is the instance at one moment.
type Snapshot struct {
	App      status.App
	Instance status.Instance
	// Health is the health report's overall state: ok, degraded or failed.
	Health string
	Inputs Inputs
}

// Source serves the report and the log through status.Handler.
type Source struct {
	Events *Log
	Read   func(context.Context) (Snapshot, error)
}

// Report puts the instance into the contract's terms, its texts in lang.
func (s Source) Report(ctx context.Context, lang string) (status.Report, error) {
	snap, err := s.Read(ctx)
	if err != nil {
		return status.Report{}, fmt.Errorf("fortress: read the instance: %w", err)
	}
	rep := status.Report{App: snap.App, Instance: snap.Instance, State: status.StateOK}
	switch snap.Health {
	case "degraded":
		rep.State = status.StateWarning
	case "failed":
		rep.State = status.StateFailed
	}
	for _, n := range needs(snap.Inputs) {
		rep.NeedsYou = append(rep.NeedsYou, n.render(lang))
	}
	rep.Work = work(snap.Inputs.Tasks)
	return rep, nil
}

// Log pages through the events.
func (s Source) Log(_ context.Context, q status.LogQuery) (status.LogPage, error) {
	return s.Events.Page(q), nil
}

// work is what is loading or unpacking. A download picks up where it was after
// a restart, an unpacking starts over, so only the second holds a restart up.
func work(tasks []*core.Task) []status.Task {
	var out []status.Task
	for _, t := range tasks {
		if len(out) == maxWork {
			break
		}
		if !t.Enabled || (t.Status != core.StatusRunning && t.Status != core.StatusExtracting) {
			continue
		}
		task := status.Task{ID: "task:" + t.ID, Kind: status.KindDownload, Title: t.Name, StartedAt: t.CreatedAt.UTC()}
		if task.Title == "" {
			task.Title = t.URL
		}
		if t.Status == core.StatusExtracting {
			task.Kind, task.BlocksRestart = status.KindExtract, true
		} else if t.Size > 0 {
			p := min(float64(t.Loaded)/float64(t.Size), 1)
			task.Progress = &p
		}
		out = append(out, task)
	}
	return out
}

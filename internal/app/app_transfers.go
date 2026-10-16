package app

import (
	"cmp"
	"slices"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// TransferReading is one running download as a bug report needs it: which
// backend moves it, how fast, and over how many connections. It names no file,
// link or host.
type TransferReading struct {
	Resolver string `json:"resolver"`
	Meter    string `json:"meter"`
	Speed    int64  `json:"speed"`
	Size     int64  `json:"size"`
	Loaded   int64  `json:"loaded"`
	// Connections and Retries are only known for a transfer the engine
	// fetches over HTTP.
	Connections *int `json:"connections,omitempty"`
	Retries     *int `json:"retries,omitempty"`
}

var meterNames = [familyCount]string{familyEngine: "engine", familyJD: "jd", familyYtdlp: "ytdlp"}

// TransferReadings is every running download, fastest first.
func (a *App) TransferReadings() []TransferReading {
	type running struct {
		r        TransferReading
		engineID string
	}
	a.mu.Lock()
	var rows []running
	for id := range a.active {
		t := a.tasks[id]
		if t == nil || t.Status != core.StatusRunning {
			continue
		}
		rows = append(rows, running{
			r: TransferReading{
				Resolver: t.Resolver, Meter: meterNames[meterFor(t.Resolver)],
				Speed: t.Speed, Size: t.Size, Loaded: t.Loaded,
			},
			engineID: a.torrentFiles.engineIDFor(id),
		})
	}
	a.mu.Unlock()

	out := make([]TransferReading, 0, len(rows))
	for _, row := range rows {
		if a.Engine != nil {
			if open, retries, ok := a.Engine.Connections(row.engineID); ok {
				row.r.Connections, row.r.Retries = &open, &retries
			}
		}
		out = append(out, row.r)
	}
	slices.SortFunc(out, func(x, y TransferReading) int { return cmp.Compare(y.Speed, x.Speed) })
	return out
}

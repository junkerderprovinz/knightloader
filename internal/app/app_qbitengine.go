package app

// The qBittorrent this app runs itself, in the background on 127.0.0.1, at
// the version the settings hold. It is fetched when missing and started with
// the app, and the automatic component update leaves it alone: private
// trackers admit only the versions on their list, so the version moves only
// when somebody picks another one.

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/qbittorrent"
	"github.com/junkerderprovinz/knightloader/internal/qbittorrent/nox"
)

const componentQBittorrent = "qbittorrent"

// The states QBittorrentStatus adds to nox's own.
const (
	QBitFetching = "fetching"
	QBitFailed   = "failed"
	QBitOff      = "off"
)

// qbitRunner is a started qBittorrent, a *nox.Instance outside the tests.
type qbitRunner interface {
	Status() nox.Status
	Ready(ctx context.Context) (*qbittorrent.Client, error)
	Stop() error
}

// qbitState is embedded on App so its fields stay in this file.
type qbitState struct {
	qbitMu sync.Mutex
	// qbitGen counts the bring-ups, so one overtaken by a later choice of
	// version starts nothing.
	qbitGen    int
	qbitTarget string
	qbitRun    qbitRunner
	// qbitPhase is QBitFetching or QBitFailed while there is no runner for
	// qbitTarget, with qbitDetail the reason of a failure.
	qbitPhase  string
	qbitDetail string
	qbitNote   string
	qbitDone   int64
	qbitTotal  int64

	// fetchQBit and runQBit are nox.Install and nox.Start, which the tests
	// replace so that nothing is downloaded or started.
	fetchQBit func(ctx context.Context, v nox.Version, progress func(done, total int64)) error
	runQBit   func(v nox.Version) qbitRunner
}

func (a *App) initQBit() {
	a.fetchQBit = func(ctx context.Context, v nox.Version, progress func(done, total int64)) error {
		_, _, err := nox.Install(ctx, a.DataDir, v, progress)
		return err
	}
	a.runQBit = func(v nox.Version) qbitRunner {
		return nox.Start(nox.Options{
			Binary:  nox.BinaryPath(a.DataDir, v),
			Profile: filepath.Join(a.DataDir, "qbittorrent"),
			Key:     a.AppKey,
			Logf:    log.Printf,
		})
	}
}

// QBittorrentVersion is one version the settings can hold.
type QBittorrentVersion struct {
	Version    string   `json:"version"`
	Libtorrent string   `json:"libtorrent"`
	Whitelists []string `json:"whitelists"`
	Note       string   `json:"note"`
}

// QBittorrentStatus is the Components card's qBittorrent row.
type QBittorrentStatus struct {
	State    string               `json:"state"`
	Version  string               `json:"version,omitempty"`
	Chosen   string               `json:"chosen"`
	Default  string               `json:"default"`
	Versions []QBittorrentVersion `json:"versions"`
	Done     int64                `json:"done,omitempty"`
	Total    int64                `json:"total,omitempty"`
	Restarts int                  `json:"restarts,omitempty"`
	Detail   string               `json:"detail,omitempty"`
}

// pickQBitVersion is the version the settings hold, or the default with a
// note when this build does not know the one they hold.
func pickQBitVersion(held string) (nox.Version, string) {
	table := nox.Versions()
	if v, err := table.Pick(held); err == nil {
		return v, ""
	}
	v, _ := table.Pick("")
	return v, fmt.Sprintf("qBittorrent %s is not one this version of KnightLoader knows, so it runs %s", held, v.Version)
}

// StartQBittorrent fetches the held version when it is missing and starts it.
// Called again it replaces the one running, once the new one is fetched.
func (a *App) StartQBittorrent() {
	v, note := pickQBitVersion(a.Settings.Get().Torrent.EngineVersion)
	a.qbitMu.Lock()
	a.qbitGen++
	gen := a.qbitGen
	a.qbitTarget, a.qbitNote = v.Version, note
	a.qbitPhase, a.qbitDetail, a.qbitDone, a.qbitTotal = QBitFetching, "", 0, -1
	a.qbitMu.Unlock()
	a.spawn(func() { a.bringUpQBit(gen, v) })
}

func (a *App) bringUpQBit(gen int, v nox.Version) {
	err := a.fetchQBit(a.ctx, v, func(done, total int64) {
		a.qbitMu.Lock()
		if gen == a.qbitGen {
			a.qbitDone, a.qbitTotal = done, total
		}
		a.qbitMu.Unlock()
	})
	a.qbitMu.Lock()
	if gen != a.qbitGen || a.ctx.Err() != nil {
		a.qbitMu.Unlock()
		return
	}
	if err != nil {
		a.qbitPhase, a.qbitDetail = QBitFailed, err.Error()
		a.qbitMu.Unlock()
		log.Printf("qBittorrent %s could not be fetched: %v", v.Version, err)
		return
	}
	old := a.qbitRun
	a.qbitRun = nil
	a.qbitMu.Unlock()
	// The old one goes first: both would want the same profile.
	if old != nil {
		_ = old.Stop()
	}
	r := a.runQBit(v)
	a.qbitMu.Lock()
	defer a.qbitMu.Unlock()
	if gen != a.qbitGen {
		go r.Stop()
		return
	}
	a.qbitRun, a.qbitPhase = r, ""
}

// applyQBitVersion swaps the running qBittorrent when the settings now hold
// another version. Before StartQBittorrent ran there is nothing to swap.
func (a *App) applyQBitVersion(held string) {
	v, _ := pickQBitVersion(held)
	a.qbitMu.Lock()
	started, same := a.qbitGen > 0, a.qbitTarget == v.Version
	a.qbitMu.Unlock()
	if started && !same {
		a.StartQBittorrent()
	}
}

// stopQBit ends the running qBittorrent. Close calls it once no bring-up is
// under way any more.
func (a *App) stopQBit() {
	a.qbitMu.Lock()
	r := a.qbitRun
	a.qbitRun = nil
	a.qbitGen++
	a.qbitMu.Unlock()
	if r != nil {
		_ = r.Stop()
	}
}

// QBittorrentStatus is how the qBittorrent this app runs stands.
func (a *App) QBittorrentStatus() QBittorrentStatus {
	table := nox.Versions()
	st := QBittorrentStatus{Default: table.Default}
	for _, v := range table.Versions {
		st.Versions = append(st.Versions, QBittorrentVersion{Version: v.Version, Libtorrent: v.Libtorrent, Whitelists: v.Whitelists, Note: v.Note})
	}
	a.qbitMu.Lock()
	defer a.qbitMu.Unlock()
	st.Chosen = a.qbitTarget
	if st.Chosen == "" {
		v, _ := pickQBitVersion(a.Settings.Get().Torrent.EngineVersion)
		st.Chosen = v.Version
	}
	switch {
	case a.qbitPhase == QBitFailed && a.qbitRun != nil:
		// The version picked could not be fetched, and the one running goes
		// on carrying the torrents.
		rs := a.qbitRun.Status()
		st.State, st.Version, st.Restarts = rs.State, rs.Version, rs.Restarts
		st.Detail = fmt.Sprintf("qBittorrent %s could not be fetched, so %s keeps running: %s", a.qbitTarget, rs.Version, a.qbitDetail)
	case a.qbitPhase != "":
		st.State, st.Detail = a.qbitPhase, a.qbitDetail
		st.Done, st.Total = a.qbitDone, a.qbitTotal
	case a.qbitRun != nil:
		rs := a.qbitRun.Status()
		st.State, st.Version, st.Restarts, st.Detail = rs.State, rs.Version, rs.Restarts, rs.Detail
	default:
		st.State = QBitOff
	}
	if st.Detail == "" {
		st.Detail = a.qbitNote
	}
	return st
}

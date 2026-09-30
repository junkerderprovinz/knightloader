package app

// What the built-in torrent client has moved: the bytes it received and sent,
// today and since the count began, and how fast each torrent sends right now.
// The download library reports a torrent's upload only as a running total
// (core.TorrentStats.Uploaded), so the totals grow by how much each reading
// adds to the one before, and a torrent's upload rate is two readings apart.
//
// The totals live in torrent-totals.json beside the store. They are written at
// most once in torrentTotalsSaveEvery and at shutdown, so a restart keeps them
// and a crash costs at most that much of them.

import (
	"cmp"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/settings"
)

const torrentTotalsFile = "torrent-totals.json"

// torrentTotalsSaveEvery is how often the totals are written while they grow.
const torrentTotalsSaveEvery = time.Minute

// uploadRateFresh is how long a torrent's upload rate stands without a new
// reading. The engine reads every torrent every few seconds, so a rate older
// than this belongs to a torrent that has stopped.
const uploadRateFresh = 10 * time.Second

// topUploaders is how many torrents the overview names as uploading most.
const topUploaders = 3

// torrentTotals is the part of the tally kept on disk.
type torrentTotals struct {
	// Day is the local calendar day the two Today figures count.
	Day             string `json:"day"`
	DownloadedToday int64  `json:"downloadedToday"`
	UploadedToday   int64  `json:"uploadedToday"`
	Downloaded      int64  `json:"downloaded"`
	Uploaded        int64  `json:"uploaded"`
}

// uploadReading is one torrent's upload total and when it was read.
type uploadReading struct {
	bytes int64
	at    time.Time
	// rate is bytes a second between this reading and the one before.
	rate int64
}

// torrentTally counts the torrent client's traffic. It has a lock of its own
// and calls nothing in App, so App may use it under a.mu.
type torrentTally struct {
	mu      sync.Mutex
	path    string
	totals  torrentTotals
	dirty   bool
	savedAt time.Time
	uploads map[string]uploadReading
}

// openTorrentTally reads the totals at path. Without a file the count starts
// from what the torrents on the list already hold, so an existing install does
// not show zero for a year of seeding.
func openTorrentTally(path string, held func() (down, up int64), now time.Time) *torrentTally {
	t := &torrentTally{path: path, uploads: map[string]uploadReading{}}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		t.totals.Downloaded, t.totals.Uploaded = held()
		t.dirty = true
	case err != nil:
		log.Printf("torrent totals could not be read and start at zero: %v", err)
	default:
		if err := json.Unmarshal(b, &t.totals); err != nil {
			log.Printf("torrent totals could not be read and start at zero: %v", err)
			t.totals = torrentTotals{}
		}
	}
	t.rollLocked(now)
	return t
}

// rollLocked starts the Today figures afresh on a new day. Caller holds t.mu.
func (t *torrentTally) rollLocked(now time.Time) {
	if day := now.Format(time.DateOnly); t.totals.Day != day {
		t.totals.Day, t.totals.DownloadedToday, t.totals.UploadedToday = day, 0, 0
		t.dirty = true
	}
}

// add counts bytes received and sent.
func (t *torrentTally) add(now time.Time, down, up int64) {
	if down <= 0 && up <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rollLocked(now)
	down, up = max(down, 0), max(up, 0)
	t.totals.Downloaded += down
	t.totals.DownloadedToday += down
	t.totals.Uploaded += up
	t.totals.UploadedToday += up
	t.dirty = true
}

// readUpload takes a torrent's upload total and keeps the rate since its last
// one. A total below the last, as after the torrent was started again from
// nothing, begins a new count rather than a negative rate.
func (t *torrentTally) readUpload(id string, bytes int64, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	next := uploadReading{bytes: bytes, at: now}
	if last, ok := t.uploads[id]; ok && bytes >= last.bytes {
		if secs := now.Sub(last.at).Seconds(); secs > 0 {
			next.rate = int64(float64(bytes-last.bytes) / secs)
		}
	}
	t.uploads[id] = next
}

// uploadRate is a torrent's current upload rate, 0 once its readings stop.
func (t *torrentTally) uploadRate(id string, now time.Time) int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	r, ok := t.uploads[id]
	if !ok || now.Sub(r.at) > uploadRateFresh {
		return 0
	}
	return r.rate
}

// forget drops a torrent's readings, for a task that is gone.
func (t *torrentTally) forget(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.uploads, id)
}

// snapshot is the totals as of now.
func (t *torrentTally) snapshot(now time.Time) torrentTotals {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rollLocked(now)
	return t.totals
}

// save writes the totals when they changed, and with force unset only once
// torrentTotalsSaveEvery has passed since the last write.
func (t *torrentTally) save(now time.Time, force bool) {
	t.mu.Lock()
	if !t.dirty || (!force && now.Sub(t.savedAt) < torrentTotalsSaveEvery) {
		t.mu.Unlock()
		return
	}
	b, err := json.Marshal(t.totals)
	t.dirty, t.savedAt = false, now
	t.mu.Unlock()
	if err != nil {
		log.Printf("could not write the torrent totals: %v", err)
		return
	}
	tmp := t.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		log.Printf("could not write the torrent totals: %v", err)
		return
	}
	if err := os.Rename(tmp, t.path); err != nil {
		log.Printf("could not put the torrent totals in place: %v", err)
		_ = os.Remove(tmp)
	}
}

// isBuiltInTorrent reports whether a task belongs to the built-in torrent
// client. A torrent a debrid service fetches downloads over HTTP and never
// seeds, so it is none of the tally's business.
func isBuiltInTorrent(t *core.Task) bool {
	return t.Resolver == (torrent.Resolver{}).Info().ID
}

// tallyTorrentLocked counts what one engine update adds to a torrent's
// figures, before the update is written onto the task. Caller holds a.mu.
func (a *App) tallyTorrentLocked(t *core.Task, u core.Update, now time.Time) {
	if !isBuiltInTorrent(t) {
		return
	}
	var down, up int64
	if u.Loaded > t.Loaded {
		down = u.Loaded - t.Loaded
	}
	if u.Torrent != nil {
		up = u.Torrent.Uploaded - t.Uploaded
		a.tally.readUpload(t.ID, u.Torrent.Uploaded, now)
	}
	a.tally.add(now, down, up)
}

// saveTorrentTally writes the totals once they are due, or at once with force.
// It runs outside a.mu. Close reaches it on an App built only in part, as it
// does every subsystem.
func (a *App) saveTorrentTally(force bool) {
	if a.tally != nil {
		a.tally.save(time.Now(), force)
	}
}

// heldTorrentBytes is what the torrents on the list have received and sent,
// where a new tally starts.
func (a *App) heldTorrentBytes() (down, up int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.tasks {
		if isBuiltInTorrent(t) {
			down += t.Loaded
			up += t.Uploaded
		}
	}
	return down, up
}

// TorrentOverview is the overview's torrent card: what the built-in client is
// doing now and what it has moved.
type TorrentOverview struct {
	// Any says this instance has a torrent on its list or has ever moved a
	// byte of one; without either the card has nothing to say.
	Any             bool  `json:"any"`
	Leeching        int   `json:"leeching"`
	Seeding         int   `json:"seeding"`
	DownloadSpeed   int64 `json:"downloadSpeed"`
	UploadSpeed     int64 `json:"uploadSpeed"`
	DownloadedToday int64 `json:"downloadedToday"`
	UploadedToday   int64 `json:"uploadedToday"`
	Downloaded      int64 `json:"downloaded"`
	Uploaded        int64 `json:"uploaded"`
	// Ratio is Uploaded over Downloaded, 0 before anything came in.
	Ratio float64 `json:"ratio"`
	// Top is the torrents sending most right now, fastest first.
	Top []TorrentUploader `json:"top"`
}

// TorrentUploader is one torrent among the overview's busiest.
type TorrentUploader struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	UploadSpeed int64   `json:"uploadSpeed"`
	Ratio       float64 `json:"ratio"`
	// SecondsLeft is how long until the torrent reaches a seeding target at
	// the rate it sends now, -1 when no target applies.
	SecondsLeft int64 `json:"secondsLeft"`
}

// TorrentOverview reads the torrent card's figures.
func (a *App) TorrentOverview() TorrentOverview {
	now := time.Now()
	tc := a.Settings.Get().Torrent
	tot := a.tally.snapshot(now)
	out := TorrentOverview{
		DownloadedToday: tot.DownloadedToday,
		UploadedToday:   tot.UploadedToday,
		Downloaded:      tot.Downloaded,
		Uploaded:        tot.Uploaded,
	}
	if out.Downloaded > 0 {
		out.Ratio = float64(out.Uploaded) / float64(out.Downloaded)
	}
	out.Any = out.Downloaded > 0 || out.Uploaded > 0

	a.mu.Lock()
	for _, t := range a.tasks {
		if !isBuiltInTorrent(t) {
			continue
		}
		out.Any = true
		leeching := t.Status == core.StatusRunning
		switch {
		case leeching:
			out.Leeching++
			out.DownloadSpeed += t.Speed
		case t.Seeding:
			out.Seeding++
		default:
			continue
		}
		rate := a.tally.uploadRate(t.ID, now)
		out.UploadSpeed += rate
		if rate > 0 {
			out.Top = append(out.Top, TorrentUploader{
				ID: t.ID, Name: t.Name, UploadSpeed: rate, Ratio: t.Ratio,
				SecondsLeft: seedSecondsLeft(t, rate, tc),
			})
		}
	}
	a.mu.Unlock()

	slices.SortFunc(out.Top, func(x, y TorrentUploader) int { return cmp.Compare(y.UploadSpeed, x.UploadSpeed) })
	if len(out.Top) > topUploaders {
		out.Top = out.Top[:topUploaders]
	}
	return out
}

// seedSecondsLeft is how long a torrent sending rate bytes a second has until
// the first of its seeding targets, counted as seedingEnd counts them, or -1
// with no target set. The ratio target is paid in bytes, the time target in
// seeding seconds, so the two are measured apart and the nearer one wins.
func seedSecondsLeft(t *core.Task, rate int64, tc settings.Torrent) int64 {
	since := t.SeedMark.Since(core.TorrentStats{Ratio: t.Ratio, SeedSeconds: t.SeedSeconds})
	left := int64(-1)
	if tc.SeedDurationSeconds > 0 {
		left = max(0, int64(tc.SeedDurationSeconds)-since.SeedSeconds)
	}
	if tc.SeedRatioTarget > 0 && rate > 0 && t.Size > 0 {
		owed := (tc.SeedRatioTarget - since.Ratio) * float64(t.Size)
		byRatio := max(0, int64(owed/float64(rate)))
		if left < 0 || byRatio < left {
			left = byRatio
		}
	}
	return left
}

package app

// The download history as a duplicate check. A link this instance has already
// downloaded is rejected with the name and date of that download, so a feed
// that lists it again or a second paste does not fetch the file twice. It goes
// to the rejected links rather than away, and a restore adds it anyway.

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/rules"
)

// skipDownloaded is the code of a link rejected because the history has it.
const skipDownloaded = "downloaded"

// downloadedIndex is the history filed in a mirror set, rebuilt when the
// history or the mirror policy has changed since. The history can hold tens of
// thousands of rows and a paste can stage hundreds of links, so it is not read
// once per link.
type downloadedIndex struct {
	mu       sync.Mutex
	built    bool
	rev      uint64
	set      *dedupe.Set
	finished map[string]time.Time // by history task id
}

// downloadedVerdict rejects a candidate the history already has: the same URL,
// or under the mirror policy the same file at another URL. It answers with an
// empty verdict when the check is switched off or the history cannot be read,
// since a link the history could not vouch for is added as before.
func (a *App) downloadedVerdict(cand rules.Candidate) rules.Verdict {
	s := a.Settings.Get()
	if !s.RejectDownloaded {
		return rules.Verdict{}
	}
	m, at := a.downloaded.match(a, dedupe.ParsePolicy(s.MirrorPolicy),
		dedupe.Entry{URL: cand.URL, Name: cand.Filename, Size: cand.Filesize})
	if !m.Seen() {
		return rules.Verdict{}
	}
	name := m.Of.Name
	if name == "" {
		name = m.Of.URL
	}
	return rules.Verdict{
		Rejected: true,
		Reason:   fmt.Sprintf("already downloaded as %q on %s", name, at.Format("2006-01-02")),
		Code:     skipDownloaded,
		Params:   map[string]string{"name": name, "finished": at.Format(time.RFC3339)},
	}
}

// match looks a candidate up in the history and reports the entry it repeats
// with that download's finish time.
func (d *downloadedIndex) match(a *App, p dedupe.Policy, e dedupe.Entry) (dedupe.Match, time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rev := a.Store.HistoryRevision()
	if !d.built || d.rev != rev || d.set.Policy() != p {
		// Read before the build, so a write during it marks the copy stale.
		d.rev = rev
		if !d.build(a, p) {
			return dedupe.Match{}, time.Time{}
		}
	}
	m := d.set.Check(e)
	return m, d.finished[m.Of.ID]
}

// build files the whole history, oldest first so that a URL downloaded twice
// names its latest download.
func (d *downloadedIndex) build(a *App, p dedupe.Policy) bool {
	entries, err := a.History(0)
	if err != nil {
		log.Printf("the download history could not be read for the duplicate check: %v", err)
		d.built = false
		return false
	}
	d.set = dedupe.New(p)
	d.finished = make(map[string]time.Time, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		d.set.Add(dedupe.Entry{ID: e.TaskID, URL: e.URL, Name: e.Name, Size: e.Size})
		d.finished[e.TaskID] = e.FinishedAt
	}
	d.built = true
	return true
}

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

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/dedupe"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
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
	m, at := a.downloaded.match(a, dedupe.ParsePolicy(s.MirrorPolicy), asSaved(cand))
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

// historyNeedsSize reports whether the history holds a file of the
// candidate's name and only its unknown size decides whether this is that
// file (see dedupe.Set.NeedsSize).
func (a *App) historyNeedsSize(cand rules.Candidate) bool {
	s := a.Settings.Get()
	if !s.RejectDownloaded {
		return false
	}
	d := &a.downloaded
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.fresh(a, dedupe.ParsePolicy(s.MirrorPolicy)) &&
		d.set.NeedsSize(asSaved(cand))
}

// asSaved is the candidate under the name its file would be saved as, which
// is the name the history keeps.
func asSaved(cand rules.Candidate) dedupe.Entry {
	return dedupe.Entry{URL: dedupeURL(cand.URL), Name: collide.SafeName(cand.Filename), Size: cand.Filesize}
}

// dedupeURL spells an uploaded .torrent as a magnet of its info hash, so the
// history and the list know a torrent whether it came as a file or as a
// magnet.
func dedupeURL(u string) string {
	if !torrent.IsURI(u) || torrent.IsMagnet(u) {
		return u
	}
	md, err := (torrent.Resolver{}).Describe(u)
	if err != nil {
		return u
	}
	return "magnet:?xt=urn:btih:" + md.InfoHash
}

// match looks a candidate up in the history and reports the entry it repeats
// with that download's finish time.
func (d *downloadedIndex) match(a *App, p dedupe.Policy, e dedupe.Entry) (dedupe.Match, time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.fresh(a, p) {
		return dedupe.Match{}, time.Time{}
	}
	m := d.set.Check(e)
	return m, d.finished[m.Of.ID]
}

// fresh rebuilds the index when the history or the policy has changed since,
// and reports whether it is usable. Caller holds d.mu.
func (d *downloadedIndex) fresh(a *App, p dedupe.Policy) bool {
	rev := a.Store.HistoryRevision()
	if d.built && d.rev == rev && d.set.Policy() == p {
		return true
	}
	// Read before the build, so a write during it marks the copy stale.
	d.rev = rev
	return d.build(a, p)
}

// build files the whole history, oldest first so that a URL downloaded twice
// names its latest download. Every row is kept, since a second download of a
// URL is often saved under a numbered name, and the first name still
// identifies the file.
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
		d.set.Keep(dedupe.Entry{ID: e.TaskID, URL: dedupeURL(e.URL), Name: e.Name, Size: e.Size})
		d.finished[e.TaskID] = e.FinishedAt
	}
	d.built = true
	return true
}

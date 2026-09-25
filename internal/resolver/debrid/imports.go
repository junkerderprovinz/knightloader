package debrid

// The import from an account, as rdt-client's Provider.AutoImport does it:
// what is added to the account outside this instance, usually on the
// service's own website, is picked up here and fetched from the service like
// a torrent this instance added itself. Each download is handed over once,
// and nothing this instance added is taken for one added elsewhere.

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// Listed is one download on a service's account, as its list shows it.
type Listed struct {
	// ID is the service's id for the download, as TorrentStatus takes it.
	ID   string
	Name string
	Size int64
	// Hash is a torrent's info hash, empty for any other kind of download.
	Hash string
	// Added is when the download was added there, zero when the service does
	// not say.
	Added time.Time
}

// Lister is a service whose downloads can be listed for the import.
type Lister interface {
	// List returns the downloads on the account. complete reports whether that
	// is all of them rather than the newest page.
	List(ctx context.Context) (list []Listed, complete bool, err error)
}

const jobScheme = "debrid"

// JobLink is the link a download imported from an account is staged under:
// debrid://realdebrid/ABC123?account=work names Real-Debrid's download ABC123
// on the account "work". Only that account's resolver claims it, so the
// download is fetched from there and never added anywhere again. It carries
// no secret.
func JobLink(slot, id string) string {
	service, account := resolver.SplitSlot(slot)
	u := url.URL{Scheme: jobScheme, Host: service, Path: "/" + id}
	if account != "" {
		u.RawQuery = url.Values{"account": {account}}.Encode()
	}
	return u.String()
}

// ParseJobLink reads a JobLink back into the account's slot and the id.
func ParseJobLink(raw string) (slot, id string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != jobScheme || u.Host == "" {
		return "", "", false
	}
	id = strings.TrimPrefix(u.Path, "/")
	if id == "" {
		return "", "", false
	}
	return resolver.SlotID(u.Host, u.Query().Get("account")), id, true
}

// ImportRecord is what one account's import remembers across restarts.
type ImportRecord struct {
	// Since is when the import first read the account. Nothing the service
	// dates earlier is imported. Zero means it has not read it yet.
	Since time.Time `json:"since"`
	// Seen is every id already dealt with, oldest first: imported, added by
	// this instance, or on the account when the import began.
	Seen []string `json:"seen,omitempty"`
}

// maxSeen bounds an account's record where its service lists only the newest
// page. Ids still listed are never dropped, and a page is far shorter.
const maxSeen = 2000

// AccountImport follows one account's list and hands over each download the
// user added there. The first poll hands nothing over: what is on the account
// then was there before the import, and stays where it is.
type AccountImport struct {
	// Load reads what the import remembers, the zero record when nothing is
	// stored. Save stores it; the zero record forgets it.
	Load func() ImportRecord
	Save func(ImportRecord)
	// Known reports a download that is in the list by another way in, such
	// as a torrent with the same info hash. It may be nil.
	Known func(Listed) bool
	// Hand stages one new download.
	Hand func(Listed)
	// Now is the clock, time.Now when nil.
	Now func() time.Time

	mu     sync.Mutex
	loaded bool
	since  time.Time
	seen   map[string]bool
	order  []string
	// changed says the memory differs from what was last saved.
	changed bool
	// sighted holds the downloads without an info hash the last poll found
	// for the first time. They are handed over on the next one, by when a
	// web download this instance was starting has been claimed.
	sighted map[string]bool
	// forgets counts the calls to Forget, so a poll that was reading the list
	// across one leaves what it read alone.
	forgets int
}

// Poll reads the account's list once and hands over what is new.
func (im *AccountImport) Poll(ctx context.Context, l Lister) error {
	im.mu.Lock()
	im.loadLocked()
	// A download claimed while the list is on its way may be missing from it,
	// so only what was remembered before the call can be found gone.
	before, round := len(im.order), im.forgets
	im.mu.Unlock()
	list, complete, err := l.List(ctx)
	if err != nil {
		return err
	}
	im.mu.Lock()
	if im.forgets != round {
		im.mu.Unlock()
		return nil
	}
	if im.since.IsZero() {
		im.since = im.now()
		for _, j := range list {
			im.rememberLocked(j.ID)
		}
		im.changed = true
		im.saveLocked()
		im.mu.Unlock()
		return nil
	}
	since := im.since
	var unseen []Listed
	for _, j := range list {
		if !im.seen[j.ID] {
			unseen = append(unseen, j)
		}
	}
	im.mu.Unlock()

	// Known takes the app's lock, so it is asked outside im.mu.
	known := make(map[string]bool, len(unseen))
	for _, j := range unseen {
		old := !j.Added.IsZero() && j.Added.Before(since)
		known[j.ID] = old || (im.Known != nil && im.Known(j))
	}

	im.mu.Lock()
	if im.forgets != round {
		im.mu.Unlock()
		return nil
	}
	var hand []Listed
	sighted := map[string]bool{}
	for _, j := range unseen {
		switch {
		case im.seen[j.ID]:
			// Claimed while Known was asked.
		case known[j.ID]:
			im.rememberLocked(j.ID)
		case j.Hash == "" && !im.sighted[j.ID]:
			sighted[j.ID] = true
		default:
			im.rememberLocked(j.ID)
			hand = append(hand, j)
		}
	}
	im.sighted = sighted
	im.trimLocked(list, complete, before)
	im.mu.Unlock()

	slices.SortStableFunc(hand, func(a, b Listed) int { return a.Added.Compare(b.Added) })
	for _, j := range hand {
		im.Hand(j)
	}
	im.mu.Lock()
	im.saveLocked()
	im.mu.Unlock()
	return nil
}

// Claim records a download this instance added to the account itself, so it
// is never handed over as one the user added.
func (im *AccountImport) Claim(id string) {
	im.mu.Lock()
	defer im.mu.Unlock()
	im.loadLocked()
	if im.seen[id] {
		return
	}
	im.rememberLocked(id)
	if !im.since.IsZero() {
		im.saveLocked()
	}
}

// Forget drops what the import remembers, so the next poll starts afresh and
// takes what is on the account then as already there.
func (im *AccountImport) Forget() {
	im.mu.Lock()
	defer im.mu.Unlock()
	im.loaded = true
	im.forgets++
	im.since = time.Time{}
	im.seen, im.order, im.sighted = map[string]bool{}, nil, nil
	im.changed = true
	im.saveLocked()
}

func (im *AccountImport) now() time.Time {
	if im.Now != nil {
		return im.Now()
	}
	return time.Now()
}

func (im *AccountImport) loadLocked() {
	if im.loaded {
		return
	}
	im.loaded = true
	rec := im.Load()
	im.since = rec.Since
	im.seen = make(map[string]bool, len(rec.Seen))
	im.order = nil
	for _, id := range rec.Seen {
		im.rememberLocked(id)
	}
}

func (im *AccountImport) rememberLocked(id string) {
	if im.seen[id] {
		return
	}
	im.seen[id] = true
	im.order = append(im.order, id)
	im.changed = true
}

func (im *AccountImport) saveLocked() {
	if !im.changed {
		return
	}
	im.Save(ImportRecord{Since: im.since, Seen: slices.Clone(im.order)})
	im.changed = false
}

// trimLocked forgets ids that have gone from the account, among the first
// `before` ids. A complete list shows that for every id missing from it; a
// single page only for the oldest ids off it, once there are more than
// maxSeen.
func (im *AccountImport) trimLocked(list []Listed, complete bool, before int) {
	listed := make(map[string]bool, len(list))
	for _, j := range list {
		listed[j.ID] = true
	}
	excess := len(im.order) - maxSeen
	if !complete && excess <= 0 {
		return
	}
	kept := im.order[:0]
	for i, id := range im.order {
		if i < before && !listed[id] && (complete || excess > 0) {
			delete(im.seen, id)
			excess--
			im.changed = true
			continue
		}
		kept = append(kept, id)
	}
	im.order = kept
}

// parseStamp reads an RFC 3339 time from a service, zero for one it left out
// or wrote some other way.
func parseStamp(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

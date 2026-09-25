// Package trackerlist keeps the list of public BitTorrent trackers somebody
// publishes at an address, such as ngosang/trackerslist's trackers_best.txt:
// fetched at most once a day, kept on disk across restarts, and left as it was
// when a fetch fails.
package trackerlist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
)

// RefreshAfter is how old a list gets before it is fetched again. The public
// lists are rebuilt once a day, and every fetch is a request to somebody's
// server that each instance pointing at it makes.
const RefreshAfter = 24 * time.Hour

// RetryAfter is how long a failed fetch waits for the next try: long enough
// that a list which is down is not asked again for every torrent, short enough
// to pick it up the same day.
const RetryAfter = time.Hour

// maxBody caps an answer. The longest public list, every tracker seen alive,
// is a few kilobytes.
const maxBody = 1 << 20

// List is the tracker list as last fetched, one address at a time: changing the
// address makes the old list unused rather than wrong. Build it with New.
type List struct {
	path   string
	client *http.Client
	now    func() time.Time

	mu     sync.Mutex
	loaded bool
	good   saved
	last   attempt
	busy   bool
}

// saved is the last good answer, in the shape it is kept on disk.
type saved struct {
	URL       string    `json:"url"`
	FetchedAt time.Time `json:"fetchedAt"`
	Trackers  []string  `json:"trackers"`
}

// attempt is the last fetch, good or not.
type attempt struct {
	url string
	at  time.Time
	err string
}

// New keeps the list in the file at path and fetches it with client.
func New(path string, client *http.Client) *List {
	return &List{path: path, client: client, now: time.Now}
}

// Trackers returns what address listed when it was last fetched, or nil when
// it never was.
func (l *List) Trackers(address string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.loadLocked()
	if address == "" || l.good.URL != address {
		return nil
	}
	return slices.Clone(l.good.Trackers)
}

// Due reports whether address should be fetched now: its list is missing or
// older than RefreshAfter, nothing is fetching it, and a fetch that failed was
// at least RetryAfter ago.
func (l *List) Due(address string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.dueLocked(address)
}

func (l *List) dueLocked(address string) bool {
	if address == "" || l.busy {
		return false
	}
	l.loadLocked()
	now := l.now()
	if l.last.url == address && l.last.err != "" && now.Sub(l.last.at) < RetryAfter {
		return false
	}
	return l.good.URL != address || now.Sub(l.good.FetchedAt) >= RefreshAfter
}

// ErrNotSaved is a list that was fetched and is in use, but could not be
// written to disk, so the next start fetches it again.
var ErrNotSaved = errors.New("the tracker list was fetched and is in use, but could not be saved")

// Refresh fetches address when it is due and returns the fetch's error. A good
// answer replaces the list, on disk as well; a failed one leaves the list that
// was there. When only the disk copy failed, the error is ErrNotSaved.
func (l *List) Refresh(ctx context.Context, address string) error {
	l.mu.Lock()
	if !l.dueLocked(address) {
		l.mu.Unlock()
		return nil
	}
	l.busy = true
	l.mu.Unlock()

	trackers, err := l.fetch(ctx, address)

	l.mu.Lock()
	defer l.mu.Unlock()
	l.busy = false
	l.last = attempt{url: address, at: l.now()}
	if err != nil {
		l.last.err = err.Error()
		return err
	}
	l.good = saved{URL: address, FetchedAt: l.last.at, Trackers: trackers}
	b, err := json.Marshal(l.good)
	if err == nil {
		// A torn file reads back as no list at all, which only costs a fetch.
		err = os.WriteFile(l.path, b, 0o600)
	}
	if err != nil {
		return fmt.Errorf("%w (%v)", ErrNotSaved, err)
	}
	return nil
}

func (l *List) fetch(ctx context.Context, address string) ([]string, error) {
	// Both errors leave the address out, since it may carry a login for a
	// list kept behind one.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, errors.New("the list address is not a usable URL")
	}
	resp, err := l.client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			return nil, ue.Err
		}
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the list answered %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxBody {
		return nil, fmt.Errorf("the answer is larger than %d bytes, which no tracker list is", maxBody)
	}
	trackers := parse(body)
	if len(trackers) == 0 {
		// A login page or an error page answered with 200 is not a list, and
		// must not replace one.
		return nil, errors.New("the answer lists no tracker addresses")
	}
	return trackers, nil
}

// parse reads the format every public list uses: one address per line, with
// blank lines between groups. A line that is no tracker address, such as a
// comment, is skipped, and so is a repeat.
func parse(body []byte) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if seen[line] || !torrent.ValidTracker(line) {
			continue
		}
		seen[line] = true
		out = append(out, line)
		if len(out) == torrent.MaxTrackers {
			break
		}
	}
	return out
}

func (l *List) loadLocked() {
	if l.loaded {
		return
	}
	l.loaded = true
	b, err := os.ReadFile(l.path)
	if err != nil {
		return
	}
	var s saved
	if json.Unmarshal(b, &s) == nil {
		l.good = s
	}
}

// Status is what the settings page shows about the list at one address.
type Status struct {
	URL string `json:"url"`
	// Trackers counts the addresses in use from this list.
	Trackers  int       `json:"trackers"`
	FetchedAt time.Time `json:"fetchedAt,omitzero"`
	// Error is why the last fetch failed, and TriedAt when that was. Both are
	// empty when the last fetch went well.
	Error    string    `json:"error,omitempty"`
	TriedAt  time.Time `json:"triedAt,omitzero"`
	Fetching bool      `json:"fetching"`
}

// Status reports the list at address.
func (l *List) Status(address string) Status {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.loadLocked()
	s := Status{URL: address, Fetching: l.busy}
	if address != "" && l.good.URL == address {
		s.Trackers = len(l.good.Trackers)
		s.FetchedAt = l.good.FetchedAt
	}
	if l.last.url == address && l.last.err != "" {
		s.Error, s.TriedAt = l.last.err, l.last.at
	}
	return s
}

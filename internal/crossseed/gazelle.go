// Package crossseed finds a torrent KnightLoader seeds on another tracker,
// so the same files are seeded there too without being downloaded again.
//
// RED and OPS share their releases and differ only in the source tag inside
// the info dictionary, so the torrent for the other site has the same files
// and pieces under another info hash, which the other site's API is asked
// for, the way fertilizer does. Other private trackers are searched by name
// through Prowlarr or Jackett. A Runner asks each tracker about each finished
// torrent once and hands the twins it finds to the torrent client.
package crossseed

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/bencode"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// Site is a Gazelle tracker: the source tags its torrents carry, the hosts
// it announces on and the address of its API.
type Site struct {
	Name     string
	Sources  []string
	Trackers []string
	API      string
}

// The two sites whose torrents cross over. RED's torrents from before its
// rename carry PTH, OPS's carry APL.
var (
	RED = Site{Name: "RED", Sources: []string{"RED", "PTH"}, Trackers: []string{"flacsfor.me"}, API: "https://redacted.sh"}
	OPS = Site{Name: "OPS", Sources: []string{"OPS", "APL"}, Trackers: []string{"home.opsfet.ch"}, API: "https://orpheus.network"}
)

// Gazelle allows five calls in ten seconds on both sites.
const (
	gazelleCalls  = 5
	gazelleWindow = 10 * time.Second
)

// Candidate is a torrent's info dictionary with another source tag, and the
// info hash that gives.
type Candidate struct {
	Source   string
	InfoHash string
	Info     []byte
}

var (
	ErrNotGazelle = errors.New("the torrent is not a private torrent from RED or OPS")
	ErrSameSite   = errors.New("the torrent is from that site already")
)

// Candidates are the info dictionaries the torrent would have on target, one
// for each of target's source tags.
func Candidates(torrent []byte, target Site) ([]Candidate, error) {
	raw, err := bencode.Raw(torrent, "info")
	if err != nil {
		return nil, err
	}
	v, err := bencode.Decode(raw)
	if err != nil {
		return nil, err
	}
	info, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("the torrent's info is not a dictionary")
	}
	if p, _ := info["private"].(int64); p != 1 {
		return nil, ErrNotGazelle
	}
	source, _ := info["source"].([]byte)
	if slices.Contains(target.Sources, strings.ToUpper(string(source))) {
		return nil, ErrSameSite
	}
	if !fromGazelle(torrent, string(source)) {
		return nil, ErrNotGazelle
	}
	out := make([]Candidate, 0, len(target.Sources))
	for _, s := range target.Sources {
		info["source"] = []byte(s)
		b, err := bencode.Encode(info)
		if err != nil {
			return nil, err
		}
		out = append(out, Candidate{Source: s, InfoHash: bencode.InfoHash(b), Info: b})
	}
	return out, nil
}

// fromGazelle reports a torrent that carries the source tag of one of the
// sites or announces on one of their trackers.
func fromGazelle(torrent []byte, source string) bool {
	for _, s := range []Site{RED, OPS} {
		if slices.Contains(s.Sources, strings.ToUpper(source)) {
			return true
		}
	}
	v, err := bencode.Decode(torrent)
	if err != nil {
		return false
	}
	announce, _ := v.(map[string]any)["announce"].([]byte)
	u, err := url.Parse(string(announce))
	if err != nil {
		return false
	}
	for _, s := range []Site{RED, OPS} {
		if slices.Contains(s.Trackers, u.Hostname()) {
			return true
		}
	}
	return false
}

// Build writes the .torrent for c, announcing to the user's own address on
// the other site.
func Build(c Candidate, announce string) ([]byte, error) {
	info, err := bencode.Decode(c.Info)
	if err != nil {
		return nil, err
	}
	return bencode.Encode(map[string]any{"announce": announce, "info": info})
}

// Gazelle asks one site's API with the user's key. One client per site
// keeps every call of the app inside the site's limit.
type Gazelle struct {
	site  Site
	http  *http.Client
	limit *limiter

	mu  sync.Mutex
	key string
}

// NewGazelle is a client for site with the user's API key.
func NewGazelle(site Site, key string) *Gazelle {
	return &Gazelle{
		site:  site,
		key:   key,
		http:  httpx.New(httpx.Options{Timeout: 30 * time.Second}),
		limit: newLimiter(gazelleCalls, gazelleWindow),
	}
}

// Site is the site the client asks.
func (g *Gazelle) Site() Site { return g.site }

// SetKey has the client ask with key from now on.
func (g *Gazelle) SetKey(key string) {
	g.mu.Lock()
	g.key = key
	g.mu.Unlock()
}

func (g *Gazelle) apiKey() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.key
}

// Match is the torrent the other site has for a candidate.
type Match struct {
	Candidate Candidate
	TorrentID int64
	GroupID   int64
}

// Find asks the site for each of the torrent's candidates in turn and
// returns the first it has, or nil when it has none.
func (g *Gazelle) Find(ctx context.Context, torrent []byte) (*Match, error) {
	cands, err := Candidates(torrent, g.site)
	if err != nil {
		return nil, err
	}
	for _, c := range cands {
		m, err := g.lookup(ctx, c.InfoHash)
		if err != nil {
			return nil, err
		}
		if m != nil {
			m.Candidate = c
			return m, nil
		}
	}
	return nil, nil
}

func (g *Gazelle) lookup(ctx context.Context, hash string) (*Match, error) {
	var body struct {
		Group struct {
			ID int64 `json:"id"`
		} `json:"group"`
		Torrent struct {
			ID int64 `json:"id"`
		} `json:"torrent"`
	}
	refusal, err := g.call(ctx, url.Values{"action": {"torrent"}, "hash": {strings.ToUpper(hash)}}, &body)
	switch {
	case err != nil:
		return nil, err
	case refusal == "":
		return &Match{TorrentID: body.Torrent.ID, GroupID: body.Group.ID}, nil
	case refusal == "bad hash parameter" || refusal == "bad parameters":
		return nil, nil
	}
	return nil, fmt.Errorf("%s refused the request: %s", g.site.Name, refusal)
}

// Index asks the site whose key it is, which tests the key with one call.
func (g *Gazelle) Index(ctx context.Context) (string, error) {
	var body struct {
		Username string `json:"username"`
	}
	refusal, err := g.call(ctx, url.Values{"action": {"index"}}, &body)
	if err != nil {
		return "", err
	}
	if refusal != "" {
		return "", fmt.Errorf("%s refused the request: %s", g.site.Name, refusal)
	}
	return body.Username, nil
}

// call sends one ajax.php request inside the site's limit and decodes the
// response of a success into v, or returns the site's refusal.
func (g *Gazelle) call(ctx context.Context, q url.Values, v any) (refusal string, err error) {
	if err := g.limit.wait(ctx); err != nil {
		return "", err
	}
	u := strings.TrimRight(g.site.API, "/") + "/ajax.php?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", g.apiKey())
	resp, err := g.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s could not be reached: %w", g.site.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", fmt.Errorf("%s asks for fewer requests", g.site.Name)
	}
	var body struct {
		Status   string          `json:"status"`
		Error    string          `json:"error"`
		Response json.RawMessage `json:"response"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); err != nil {
		return "", fmt.Errorf("%s answered %s with something that is not its API's JSON", g.site.Name, resp.Status)
	}
	if body.Status != "success" {
		return cmp.Or(body.Error, "no reason given"), nil
	}
	if err := json.Unmarshal(body.Response, v); err != nil {
		return "", fmt.Errorf("%s answered with a response its API does not give: %w", g.site.Name, err)
	}
	return "", nil
}

// limiter lets n calls through in any window of length per.
type limiter struct {
	n     int
	per   time.Duration
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error

	mu   sync.Mutex
	sent []time.Time
}

func newLimiter(n int, per time.Duration) *limiter {
	return &limiter{n: n, per: per, now: time.Now, sleep: sleepCtx}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// wait blocks until a call may go out and counts it.
func (l *limiter) wait(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for {
		now := l.now()
		for len(l.sent) > 0 && now.Sub(l.sent[0]) >= l.per {
			l.sent = l.sent[1:]
		}
		if len(l.sent) < l.n {
			l.sent = append(l.sent, now)
			return nil
		}
		if err := l.sleep(ctx, l.per-now.Sub(l.sent[0])); err != nil {
			return err
		}
	}
}

package debrid

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// RPNet speaks the undocumented RPNet API. Calls and field names are the ones
// three open clients agree on: JDownloader's hoster/RPNetBiz.java (svn rev
// 51886), pyLoad's RPNetBiz plugins and ResolveURL's rpnet.py, with
// JDownloader deciding the queue poll where they differ.
type RPNet struct {
	customerID string
	apiKey     string
	base       string
	hc         *http.Client

	// pollEvery and queueWait pace the wait for a file RPNet first copies to
	// its own storage before it hands out a link.
	pollEvery time.Duration
	queueWait time.Duration

	mu sync.Mutex
	// queued maps a hoster link to RPNet's queue entry for it, so a retry
	// follows that entry instead of queueing the file a second time.
	queued map[string]string
	// caps maps a hoster domain to the smallest max_connections RPNet has
	// named for one of its links.
	caps map[string]int
}

// NewRPNet takes the numeric customer ID and the 40-character API key listed
// at https://premium.rpnet.biz/account.
func NewRPNet(customerID, apiKey string) *RPNet {
	return &RPNet{
		customerID: strings.TrimSpace(customerID),
		apiKey:     strings.TrimSpace(apiKey),
		base:       "https://premium.rpnet.biz",
		hc:         httpx.New(httpx.Options{Timeout: rpnetCallTimeout}),
		pollEvery:  10 * time.Second,
		queueWait:  time.Minute,
		queued:     map[string]string{},
		caps:       map[string]int{},
	}
}

func (*RPNet) ID() string    { return "rpnet" }
func (*RPNet) Label() string { return "RPNet" }

const (
	rpnetCallTimeout = 30 * time.Second

	// rpnetDeadlineMargin is kept free before the caller's deadline. It is
	// longer than one call may take, so a last poll that hangs until
	// rpnetCallTimeout still ends first, and the user sees the service's own
	// error rather than a bare timeout.
	rpnetDeadlineMargin = rpnetCallTimeout + 10*time.Second
)

func (r *RPNet) get(ctx context.Context, pathAndQuery string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.base+pathAndQuery, nil)
	if err != nil {
		return nil, 0, rpnetWithoutURL(err)
	}
	resp, err := r.hc.Do(req)
	if err != nil {
		return nil, 0, rpnetWithoutURL(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return raw, resp.StatusCode, err
}

// rpnetWithoutURL unwraps a *url.Error, whose text quotes the request URL and
// with it the API key.
func rpnetWithoutURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// rpnetNoAnswer is a call that brought no JSON answer back: it did not get
// through, or something other than the API replied. Such a call says nothing
// about the queue entry it asked for.
type rpnetNoAnswer struct{ err error }

func (e rpnetNoAnswer) Error() string { return e.err.Error() }
func (e rpnetNoAnswer) Unwrap() error { return e.err }

// call runs one client_api.php action and returns the answer once it is known
// not to be a refusal. Refusals arrive as {"error":["sentence"]}.
func (r *RPNet) call(ctx context.Context, action string, q url.Values) (json.RawMessage, error) {
	if r.customerID == "" || r.apiKey == "" {
		return nil, errors.New("rpnet: a customer ID and an API key are required")
	}
	if q == nil {
		q = url.Values{}
	}
	q.Set("username", r.customerID)
	q.Set("password", r.apiKey)
	q.Set("action", action)
	raw, code, err := r.get(ctx, "/client_api.php?"+q.Encode())
	if err != nil {
		return nil, rpnetNoAnswer{fmt.Errorf("rpnet %s: %w", action, err)}
	}
	var top struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(raw, &top) != nil {
		// JDownloader looks for the ban sentence in the raw body, so it may
		// come without the JSON wrapper.
		if s := rpnetBanSentence(raw); s != "" {
			return nil, rpnetNoAnswer{rpnetRefusal(s)}
		}
		return nil, rpnetNoAnswer{fmt.Errorf("rpnet %s: unreadable answer (HTTP %d)", action, code)}
	}
	if msg := rpnetText(top.Error); msg != "" {
		return nil, rpnetRefusal(msg)
	}
	return raw, nil
}

func rpnetBanSentence(body []byte) string {
	i := bytes.Index(body, []byte("IP Ban in effect for"))
	if i < 0 {
		return ""
	}
	s := string(body[i:])
	if j := strings.IndexAny(s, "<\r\n"); j >= 0 {
		s = s[:j]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return strings.TrimSpace(s)
}

// rpnetRefusal turns one of RPNet's error sentences into an error. The
// sentences are not published, so they are sorted by their wording and the
// service's own text is always kept.
func rpnetRefusal(msg string) error {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "invalid authentication"):
		return fmt.Errorf("rpnet: RPNet refused the customer ID or API key (%s); both are listed at premium.rpnet.biz/account", msg)
	case strings.Contains(low, "ip ban"):
		return fmt.Errorf("rpnet: RPNet has banned this IP address for a while (%s)", msg)
	case rpnetMentions(low, "not supported", "unsupported"):
		return fmt.Errorf("rpnet: RPNet does not support this hoster (%s)", msg)
	case rpnetMentions(low, "limit", "quota", "exceeded", "traffic", "bandwidth", "points"):
		return fmt.Errorf("rpnet: RPNet quota exceeded for this account (%s)", msg)
	case rpnetMentions(low, "not found", "deleted", "removed", "does not exist", "doesn't exist", "no longer exists"):
		return fmt.Errorf("rpnet: the file is gone from the hoster (%s)", msg)
	}
	return fmt.Errorf("rpnet: %s", msg)
}

func rpnetMentions(s string, phrases ...string) bool {
	for _, p := range phrases {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

// Authenticate checks the customer ID and the API key against the account
// call, since hostlist.php needs no account. It wants the account block
// itself: an answer that only lacks an error, such as a proxy's JSON error
// page, proves nothing about the key.
func (r *RPNet) Authenticate(ctx context.Context) error {
	_, err := r.Account(ctx)
	return err
}

// rpnetDomain keeps the domains out of hostlist.php, which also carries
// feature words such as "torrent", "directlink" and "cloud_download".
var rpnetDomain = regexp.MustCompile(`^[a-z0-9-]+(\.[a-z0-9-]+)+$`)

// Hosts reads hostlist.php, a bare comma-separated list that needs no account.
// It names no status and no alias domains, so every domain on it is taken.
func (r *RPNet) Hosts(ctx context.Context) (map[string]bool, error) {
	raw, code, err := r.get(ctx, "/hostlist.php")
	if err != nil {
		return nil, fmt.Errorf("rpnet hostlist.php: %w", err)
	}
	if code/100 != 2 {
		return nil, fmt.Errorf("rpnet hostlist.php: HTTP %d", code)
	}
	set := map[string]bool{}
	for _, tok := range strings.FieldsFunc(string(raw), func(c rune) bool { return c == ',' || c == '\n' || c == '\r' }) {
		if d := NormalizeHost(tok); rpnetDomain.MatchString(d) {
			set[d] = true
		}
	}
	if len(set) == 0 {
		return nil, errors.New("rpnet: hostlist.php named no hosts")
	}
	return set, nil
}

// rpnetEntry is one link of a generate answer or one item of a queue answer.
// The raw fields are the ones the open clients read as different types.
type rpnetEntry struct {
	Generated      string          `json:"generated"`
	RPNetLink      string          `json:"rpnet_link"`
	Filename       string          `json:"filename"`
	ID             json.RawMessage `json:"id"`
	Error          json.RawMessage `json:"error"`
	TextStatus     json.RawMessage `json:"text_status"`
	MaxConnections json.RawMessage `json:"max_connections"`
}

// rpnetFirst decodes the first entry of a list that RPNet sends as an array
// or, as JDownloader also accepts, as a single object.
func rpnetFirst(raw json.RawMessage) (rpnetEntry, bool) {
	var many []rpnetEntry
	if json.Unmarshal(raw, &many) == nil {
		if len(many) == 0 {
			return rpnetEntry{}, false
		}
		return many[0], true
	}
	var one rpnetEntry
	if json.Unmarshal(raw, &one) == nil {
		return one, true
	}
	return rpnetEntry{}, false
}

// Unlock asks generate for a link. A file RPNet cannot pass straight through
// comes back as a queue id instead of a link, and is followed in awaitQueue.
func (r *RPNet) Unlock(ctx context.Context, link string) (Direct, error) {
	r.mu.Lock()
	id := r.queued[link]
	r.mu.Unlock()
	if id != "" {
		return r.awaitQueue(ctx, link, id)
	}

	raw, err := r.call(ctx, "generate", url.Values{"links": {link}})
	if err != nil {
		return Direct{}, err
	}
	var ans struct {
		Links     json.RawMessage `json:"links"`
		Downloads json.RawMessage `json:"downloads"`
	}
	_ = json.Unmarshal(raw, &ans)
	e, ok := rpnetFirst(ans.Links)
	if !ok {
		e, ok = rpnetFirst(ans.Downloads)
	}
	if !ok {
		return Direct{}, errors.New("rpnet: unreadable answer to generate")
	}
	if msg := rpnetText(e.Error); msg != "" {
		return Direct{}, rpnetRefusal(msg)
	}
	// JDownloader and pyLoad both take an id as the queue, whatever else the
	// entry carries.
	if id = rpnetText(e.ID); id != "" && id != "0" {
		r.mu.Lock()
		r.queued[link] = id
		r.mu.Unlock()
		return r.awaitQueue(ctx, link, id)
	}
	if e.Generated == "" {
		return Direct{}, errors.New("rpnet: no direct link returned")
	}
	r.rememberCap(link, e.MaxConnections)
	return Direct{URL: e.Generated, Name: e.Filename}, nil
}

// awaitQueue polls a queue entry until RPNet has the file or the wait runs
// out. Running out is reported as a temporary error before the caller's
// deadline, so the app retries, and the retry finds the stored entry.
func (r *RPNet) awaitQueue(ctx context.Context, link, id string) (Direct, error) {
	stop := time.Now().Add(r.queueWait)
	if dl, ok := ctx.Deadline(); ok && dl.Add(-rpnetDeadlineMargin).Before(stop) {
		stop = dl.Add(-rpnetDeadlineMargin)
	}
	for {
		raw, err := r.call(ctx, "downloadsInformation", url.Values{"type": {"queue"}, "ids[]": {id}})
		if err != nil {
			// As in JDownloader, a poll without a JSON answer keeps the entry
			// and a refusal starts over.
			var none rpnetNoAnswer
			if !errors.As(err, &none) {
				r.forget(link)
			}
			return Direct{}, err
		}
		var ans struct {
			Downloads json.RawMessage `json:"downloads"`
		}
		_ = json.Unmarshal(raw, &ans)
		e, ok := rpnetFirst(ans.Downloads)
		if !ok {
			r.forget(link)
			return Direct{}, fmt.Errorf("rpnet: RPNet no longer lists queue entry %s, the next attempt asks for the link again", id)
		}
		if msg := rpnetText(e.Error); msg != "" {
			r.forget(link)
			return Direct{}, rpnetRefusal(msg)
		}
		status := rpnetText(e.TextStatus)
		pct, known := rpnetPercent(status)
		// JDownloader takes a bracketed 100 for done as well.
		if strings.EqualFold(status, "completed") || (known && pct == 100) {
			r.forget(link)
			if e.RPNetLink == "" {
				return Direct{}, fmt.Errorf("rpnet: queue entry %s finished without a download link", id)
			}
			r.rememberCap(link, e.MaxConnections)
			return Direct{URL: e.RPNetLink, Name: e.Filename}, nil
		}
		if time.Now().Add(r.pollEvery).After(stop) {
			// The app files "temporarily unavailable" as a passing outage.
			if known {
				return Direct{}, fmt.Errorf("rpnet: the file is temporarily unavailable while RPNet copies it to its own storage (%d%% done)", pct)
			}
			return Direct{}, errors.New("rpnet: the file is temporarily unavailable while RPNet copies it to its own storage")
		}
		select {
		case <-ctx.Done():
			return Direct{}, ctx.Err()
		case <-time.After(r.pollEvery):
		}
	}
}

func (r *RPNet) forget(link string) {
	r.mu.Lock()
	delete(r.queued, link)
	r.mu.Unlock()
}

// rememberCap records max_connections for link's host, which JDownloader
// obeys as the chunk limit. The smallest value wins: too few chunks only slow
// a download, too many can get it refused. Zero or less means no limit to
// JDownloader and is left out, so it cannot hide a real figure seen later.
func (r *RPNet) rememberCap(link string, raw json.RawMessage) {
	n := looseInt(raw)
	if n <= 0 {
		return
	}
	u, err := url.Parse(link)
	if err != nil || u.Hostname() == "" {
		return
	}
	host := NormalizeHost(u.Hostname())
	r.mu.Lock()
	defer r.mu.Unlock()
	if cur, ok := r.caps[host]; !ok || int(n) < cur {
		r.caps[host] = int(n)
	}
}

// HostLimit satisfies HostLimiter. It is 0 (no opinion) until an answer has
// named max_connections for a link of this host.
func (r *RPNet) HostLimit(host string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.caps[NormalizeHost(host)]
}

// rpnetPercent reads the progress of a queue entry, which JDownloader parses
// as a number inside brackets.
func rpnetPercent(status string) (int, bool) {
	n, err := strconv.Atoi(strings.Trim(status, "[]()% "))
	if err != nil || n < 0 || n > 100 {
		return 0, false
	}
	return n, true
}

// Account reads showAccountInformation, whose premiumExpiry is in Unix seconds
// and null on a free or lapsed account. No field the open clients read carries
// traffic, so Traffic stays empty rather than claiming Unlimited.
func (r *RPNet) Account(ctx context.Context) (AccountInfo, error) {
	raw, err := r.call(ctx, "showAccountInformation", nil)
	if err != nil {
		return AccountInfo{}, err
	}
	var ans struct {
		AccountInfo *struct {
			PremiumExpiry json.RawMessage `json:"premiumExpiry"`
		} `json:"accountInfo"`
	}
	if json.Unmarshal(raw, &ans) != nil || ans.AccountInfo == nil {
		return AccountInfo{}, errors.New("rpnet: unreadable answer to showAccountInformation")
	}
	info := AccountInfo{Tier: "free"}
	if secs := looseInt(ans.AccountInfo.PremiumExpiry); secs > 0 {
		info.ExpiresAt = time.Unix(secs, 0).UTC()
		if info.ExpiresAt.After(time.Now()) {
			info.Tier = "premium"
		}
	}
	return info, nil
}

// rpnetText reads a field RPNet sends as a string, a number or a list of
// strings, and returns "" for anything else, false and null included.
func rpnetText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return strings.TrimSpace(strings.Join(list, "; "))
	}
	return ""
}

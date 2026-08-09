package debrid

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// RealDebrid speaks the Real-Debrid REST 1.0 API.
// https://api.real-debrid.com/ — auth is a Bearer API token.
type RealDebrid struct {
	token string
	base  string
	hc    *http.Client

	// chunkMu guards chunkCaps, which Unlock writes from whatever goroutine a
	// download happens to run on (see debrid.Backend.start) and HostLimit
	// reads from the dispatch path - two callers that were never one before
	// this field existed.
	chunkMu sync.Mutex
	// chunkCaps remembers, per hoster host, the smallest "chunks" Real-Debrid
	// has ever reported for a link on it - see HostLimit for what this
	// answers and rememberChunkCap for why the smallest one wins.
	chunkCaps map[string]int
}

func NewRealDebrid(token string) *RealDebrid {
	return &RealDebrid{token: token, base: "https://api.real-debrid.com/rest/1.0", hc: httpx.New(httpx.Options{Timeout: 30 * time.Second})}
}

func (*RealDebrid) ID() string    { return "realdebrid" }
func (*RealDebrid) Label() string { return "Real-Debrid" }

func (r *RealDebrid) do(ctx context.Context, method, path string, form url.Values, out any) error {
	status, raw, err := r.raw(ctx, method, path, form, true)
	if err != nil {
		return err
	}
	if status >= 400 {
		var e rdError
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			return fmt.Errorf("realdebrid %s: %s", path, e.Error)
		}
		return fmt.Errorf("realdebrid %s: HTTP %d", path, status)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// rdError is Real-Debrid's failure body. The numeric code is the part worth
// reading: the sentence is prose, but 24 is "File unavailable" whatever else
// changes around it.
type rdError struct {
	Error string `json:"error"`
	Code  int    `json:"error_code"`
}

// raw performs the call and hands back the status alongside the body, because
// one caller needs the status itself rather than an error built from it - a
// check has to tell a 503 that means "this file is gone" from a 503 that means
// "come back later", and both arrive as the same status with a different code.
//
// auth is a parameter for the same reason: /unrestrict/check is deliberately
// called without the token. See CheckLinks.
func (r *RealDebrid) raw(ctx context.Context, method, path string, form url.Values, auth bool) (int, []byte, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, r.base+path, body)
	if err != nil {
		return 0, nil, err
	}
	if auth && r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := r.hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return resp.StatusCode, raw, nil
}

func (r *RealDebrid) Hosts(ctx context.Context) (map[string]bool, error) {
	// /hosts/domains is a plain array of supported domains and needs no auth.
	var domains []string
	if err := r.do(ctx, http.MethodGet, "/hosts/domains", nil, &domains); err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, d := range domains {
		if d = NormalizeHost(d); d != "" {
			set[d] = true
		}
	}
	return set, nil
}

// rdCheckParallel is how many /unrestrict/check calls are in flight at once.
//
// Small on purpose. Real-Debrid meters by address, and the endpoint takes one
// link per call, so a fifty-link collector is fifty requests however they are
// arranged - the only thing left to decide is whether they arrive as a burst
// that earns a "Slow down" for the whole household or as a queue nobody
// notices. Four is the queue.
const rdCheckParallel = 4

// CheckLinks asks /unrestrict/check about each link.
//
// Free, and the API's own shape is the proof rather than a promise in a
// sentence: the endpoint requires no authentication at all, so there is no
// account for it to bill. That is also why the call goes out with the token
// deliberately stripped - the one way this could ever start costing the user
// something is if Real-Debrid began attributing it to whoever signed it, and a
// request carrying no credential cannot be attributed to anybody. The unlock
// path, which does spend traffic, is /unrestrict/link and is somewhere else.
//
// It is fanned out rather than batched because Real-Debrid has no batch form
// for it. That is not a reason to call it link by link from the caller: the
// interface stays a batch so the fan-out is bounded here, once, instead of
// being however fast the caller's loop happens to run.
func (r *RealDebrid) CheckLinks(ctx context.Context, links []string) ([]core.Availability, error) {
	out := make([]core.Availability, len(links))
	sem := make(chan struct{}, rdCheckParallel)
	var wg sync.WaitGroup
	for i, link := range links {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			// Each goroutine owns its own index and reads nothing the others write,
			// which is what makes the shared slice safe without a mutex here.
			out[i] = r.checkOne(ctx, link)
		}()
	}
	wg.Wait()
	// No length guard on the way out: the slice was sized from the request and
	// every index is written by exactly one goroutine, so the contract holds by
	// construction here rather than by inspection.
	return out, nil
}

// checkOne is one link's verdict. It never returns an error: a link the service
// would not talk about is uncheckable, and letting that end the whole batch
// would throw away the forty-nine answers that did arrive.
func (r *RealDebrid) checkOne(ctx context.Context, link string) core.Availability {
	status, raw, err := r.raw(ctx, http.MethodPost, "/unrestrict/check", url.Values{"link": {link}}, false)
	if err != nil {
		return core.AvailUncheckable
	}
	if status >= 400 {
		var e rdError
		_ = json.Unmarshal(raw, &e)
		return rdVerdict(e.Code)
	}
	var out struct {
		// A pointer, because absent and zero are different answers. Real-Debrid
		// sends supported:0 for a host it cannot unlock, which is worth knowing;
		// a response that omits the field entirely says nothing, and reading that
		// as a zero would file every such link as uncheckable.
		Supported *int `json:"supported"`
	}
	if json.Unmarshal(raw, &out) == nil && out.Supported != nil && *out.Supported == 0 {
		return core.AvailUncheckable
	}
	return core.AvailOnline
}

// rdVerdict reads Real-Debrid's numeric error code as a statement about the
// link. Two codes are the host saying the file is not there; everything else is
// about the service, the hoster or this address, and none of those is a reason
// to tell somebody their link is dead.
func rdVerdict(code int) core.Availability {
	switch code {
	case 24, // File unavailable
		35: // Infringing file - taken down, and not coming back
		return core.AvailOffline
	}
	return core.AvailUncheckable
}

// Account reads /user: account type and premium expiry. Field names and the
// "2032-06-06T04:42:42.000Z" expiration shape are Real-Debrid's own
// documented example response.
//
// Real-Debrid markets and behaves as unlimited bandwidth for a premium
// account, and /user carries no overall byte-cap field to contradict that -
// so a premium account reads Unlimited here. /traffic exists but answers a
// different question: it is keyed per hoster, for the handful of restricted
// hosts Real-Debrid itself rations, and deliberately not summarised into this
// one account-wide reading, the same reasoning AllDebrid.Account gives for
// leaving out limitedHostersQuotas.
func (r *RealDebrid) Account(ctx context.Context) (AccountInfo, error) {
	var data struct {
		Type       string `json:"type"` // "premium" or "free"
		Expiration string `json:"expiration"`
	}
	if err := r.do(ctx, http.MethodGet, "/user", nil, &data); err != nil {
		return AccountInfo{}, err
	}
	info := AccountInfo{Tier: data.Type, Traffic: TrafficInfo{Unlimited: data.Type == "premium"}}
	if data.Expiration != "" {
		// RFC3339Nano, not RFC3339: Real-Debrid's own example carries
		// milliseconds ("...042.000Z") that the plain RFC3339 layout has no
		// verb for, and Go's parser only accepts a fractional-seconds
		// component when the layout itself expresses one.
		if t, err := time.Parse(time.RFC3339Nano, data.Expiration); err == nil {
			info.ExpiresAt = t
		}
	}
	return info, nil
}

func (r *RealDebrid) Unlock(ctx context.Context, link string) (Direct, error) {
	var out struct {
		Filename string `json:"filename"`
		Filesize int64  `json:"filesize"`
		Download string `json:"download"`
		// Chunks is Real-Debrid's own "Max Chunks allowed" for this specific
		// link (documented on /unrestrict/link, and identically on
		// /unrestrict/check, /downloads and /torrents - verified against
		// api.real-debrid.com). See rememberChunkCap for what happens to it.
		Chunks int `json:"chunks"`
	}
	if err := r.do(ctx, http.MethodPost, "/unrestrict/link", url.Values{"link": {link}}, &out); err != nil {
		return Direct{}, err
	}
	if out.Download == "" {
		return Direct{}, fmt.Errorf("realdebrid: no direct link returned")
	}
	r.rememberChunkCap(link, out.Chunks)
	return Direct{URL: out.Download, Name: out.Filename, Size: out.Filesize}, nil
}

// rememberChunkCap records the smallest "chunks" Real-Debrid has reported for
// link's host.
//
// LEARNED OPPORTUNISTICALLY, NOT FETCHED UP FRONT, because there is nothing
// to fetch up front: Real-Debrid publishes no per-host table of this anywhere
// in its API (checked against /hosts, /hosts/status, /hosts/domains and
// /hosts/regex - none of them carry it). The only place the number is ever
// stated is on a response about a link Real-Debrid has just unlocked, so the
// cache starts empty for every host and fills in as real downloads use it -
// see HostLimit for the "0 means nothing learned yet" half of that contract.
//
// The smallest seen, not the latest: two different unlocks reporting 4 and
// then 16 for the same host both came from the one API, and the smaller of
// the two is the one no request against that host has ever been refused for.
// A chunks of 0 or less is dropped rather than remembered - Real-Debrid
// omitting the field entirely must not be read as "zero chunks allowed",
// which connsFor would then apply as a hard stop on every future download to
// that host.
func (r *RealDebrid) rememberChunkCap(link string, chunks int) {
	if chunks <= 0 {
		return
	}
	u, err := url.Parse(link)
	if err != nil || u.Hostname() == "" {
		return
	}
	host := NormalizeHost(u.Hostname())
	r.chunkMu.Lock()
	defer r.chunkMu.Unlock()
	if r.chunkCaps == nil {
		r.chunkCaps = map[string]int{}
	}
	if cur, ok := r.chunkCaps[host]; !ok || chunks < cur {
		r.chunkCaps[host] = chunks
	}
}

// HostLimit satisfies debrid.HostLimiter: 0 until a real Unlock response has
// said otherwise for this exact host, which is the same "no opinion" zero
// every other absent ceiling in connsFor's chain already means.
func (r *RealDebrid) HostLimit(host string) int {
	r.chunkMu.Lock()
	defer r.chunkMu.Unlock()
	return r.chunkCaps[NormalizeHost(host)]
}

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

// RealDebrid speaks the Real-Debrid REST 1.0 API (https://api.real-debrid.com/)
// with a Bearer API token.
type RealDebrid struct {
	token string
	base  string
	hc    *http.Client

	// chunkMu guards chunkCaps, written by Unlock on download goroutines and
	// read by HostLimit from dispatch.
	chunkMu sync.Mutex
	// chunkCaps holds the smallest "chunks" reported per hoster host.
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

// rdError is Real-Debrid's failure body. The numeric code is stable where the
// message text is not.
type rdError struct {
	Error string `json:"error"`
	Code  int    `json:"error_code"`
}

// raw performs the call and returns status and body, so a check can read the
// error code behind a 503. auth is false for /unrestrict/check (see
// CheckLinks).
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
// Real-Debrid rate-limits by address and checks one link per call, so a large
// batch goes out as a small queue rather than a burst.
const rdCheckParallel = 4

// CheckLinks asks /unrestrict/check about each link. The endpoint needs no
// authentication, and the token is left off so the check can never be billed
// to the account.
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
			// Each goroutine writes only its own index.
			out[i] = r.checkOne(ctx, link)
		}()
	}
	wg.Wait()
	return out, nil
}

// checkOne is one link's verdict. A failure makes that link uncheckable
// rather than failing the whole batch.
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
		// supported:0 marks a host Real-Debrid cannot unlock; an absent
		// field says nothing.
		Supported *int `json:"supported"`
	}
	if json.Unmarshal(raw, &out) == nil && out.Supported != nil && *out.Supported == 0 {
		return core.AvailUncheckable
	}
	return core.AvailOnline
}

// rdVerdict reads Real-Debrid's numeric error code as a statement about the
// link. Only two codes say the file is gone; the rest are about the service,
// the hoster or this address.
func rdVerdict(code int) core.Availability {
	switch code {
	case 24, // File unavailable
		35: // Infringing file
		return core.AvailOffline
	}
	return core.AvailUncheckable
}

// Account reads /user for account type and premium expiry. A premium account
// reads Unlimited: /user has no byte cap, and /traffic only covers the few
// hosts Real-Debrid rations individually.
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
		// The value carries milliseconds ("2032-06-06T04:42:42.000Z").
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
		// Chunks is Real-Debrid's "Max Chunks allowed" for this link.
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
// link's host. No endpoint publishes a per-host table, so the limit is learned
// from unlock answers as downloads happen. The smallest value wins because no
// request has been refused at that count, and a missing field (0) is not
// recorded, since connsFor would read it as a hard stop.
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

// HostLimit satisfies HostLimiter. It is 0 (no opinion) until an Unlock
// answer has reported chunks for this host.
func (r *RealDebrid) HostLimit(host string) int {
	r.chunkMu.Lock()
	defer r.chunkMu.Unlock()
	return r.chunkCaps[NormalizeHost(host)]
}

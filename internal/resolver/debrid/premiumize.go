package debrid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// Premiumize speaks the Premiumize.me API.
//
// VERIFIED, NOT GUESSED - read off Premiumize's own API reference at
// https://www.premiumize.me/api (fetched 2026-09-06):
//
//   - Base URL https://www.premiumize.me/api, and the key travels as
//     "Authorization: Bearer YOUR_API_KEY" (a query parameter and a POST field
//     are documented as legacy alternatives; the header is the current one).
//   - Every answer carries {"status": "success"|"error"}, and a business-logic
//     failure is an HTTP 200 with status "error" plus "message" and "code" -
//     so the status line is never the thing to test.
//   - GET /account/info answers {status, customer_id, premium_until,
//     limit_used, booster_points, space_used}. premium_until is a unix
//     timestamp and null on a free account; limit_used is a FRACTION in [0,1]
//     of the fair-use allowance, not a byte figure.
//   - GET /services/list answers {status, cache[], directdl[], queue[],
//     fairusefactor{}, aliases{}, regexpatterns{}} - directdl is the list this
//     file routes on, because it is precisely "services supporting instant
//     download generation", and aliases carries the alternative domains a link
//     may actually be written with.
//   - POST /transfer/directdl takes src and answers {status, content: [{path,
//     size, link}]}.
type Premiumize struct {
	key  string
	base string
	hc   *http.Client
}

func NewPremiumize(key string) *Premiumize {
	return &Premiumize{
		key:  key,
		base: "https://www.premiumize.me/api",
		hc:   httpx.New(httpx.Options{Timeout: 30 * time.Second}),
	}
}

func (*Premiumize) ID() string    { return "premiumize" }
func (*Premiumize) Label() string { return "Premiumize.me" }

// pmStatus is the part of every answer that says whether the rest of it means
// anything. Embedded into each response type rather than unmarshalled twice,
// so no call site can forget to look at it.
type pmStatus struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

func (s pmStatus) err(path string) error {
	if s.Status == "success" {
		return nil
	}
	switch {
	case s.Message != "":
		return fmt.Errorf("premiumize %s: %s", path, s.Message)
	case s.Code != "":
		return fmt.Errorf("premiumize %s: %s", path, s.Code)
	}
	return fmt.Errorf("premiumize %s: the call failed and Premiumize named no reason", path)
}

func (p *Premiumize) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.base+path, nil)
	if err != nil {
		return err
	}
	return p.send(req, path, out)
}

func (p *Premiumize) post(ctx context.Context, path string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return p.send(req, path, out)
}

func (p *Premiumize) send(req *http.Request, path string, out any) error {
	if p.key != "" {
		req.Header.Set("Authorization", "Bearer "+p.key)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := p.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("premiumize %s: %s", path, resp.Status)
	}
	return nil
}

// Hosts asks /services/list.
//
// directdl, not cache: cache is what Premiumize can answer FROM ITS CLOUD, and
// a link only in that list would be claimed by a resolver whose Unlock -
// /transfer/directdl - is documented for the directdl set. Claiming a link this
// backend then cannot hand over is worse than not claiming it, because the
// lower-priority backends never get their turn.
//
// The aliases map is folded in beside the names, because that is what it is
// for: a service listed as "uploaded" is written "ul.to" in half the links
// people actually paste.
func (p *Premiumize) Hosts(ctx context.Context) (map[string]bool, error) {
	var data struct {
		pmStatus
		DirectDL []string            `json:"directdl"`
		Aliases  map[string][]string `json:"aliases"`
	}
	if err := p.get(ctx, "/services/list", &data); err != nil {
		return nil, err
	}
	if err := data.err("/services/list"); err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, name := range data.DirectDL {
		if h := NormalizeHost(name); h != "" && strings.Contains(h, ".") {
			set[h] = true
		}
		for _, alias := range data.Aliases[name] {
			if h := NormalizeHost(alias); h != "" && strings.Contains(h, ".") {
				set[h] = true
			}
		}
	}
	if len(set) == 0 {
		return nil, errors.New("premiumize: /services/list named no direct-download hosts")
	}
	return set, nil
}

// Unlock asks /transfer/directdl for one link.
//
// content is an ARRAY because src may be a folder or an archive Premiumize can
// expand; a hoster link is one file, so the first entry is the answer. A
// success with an empty array is not a link and must not be reported as one -
// the engine handed an empty URL would fail with something unrelated further
// down.
func (p *Premiumize) Unlock(ctx context.Context, link string) (Direct, error) {
	var data struct {
		pmStatus
		Content []struct {
			Path string `json:"path"`
			Size int64  `json:"size"`
			Link string `json:"link"`
		} `json:"content"`
	}
	if err := p.post(ctx, "/transfer/directdl", url.Values{"src": {link}}, &data); err != nil {
		return Direct{}, err
	}
	if err := data.err("/transfer/directdl"); err != nil {
		return Direct{}, err
	}
	if len(data.Content) == 0 || data.Content[0].Link == "" {
		return Direct{}, errors.New("premiumize: no direct link returned")
	}
	first := data.Content[0]
	// path is slash-joined inside the source; the file's own name is the last
	// segment of it, which is what a task's name is meant to be.
	name := first.Path
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	return Direct{URL: first.Link, Name: name, Size: first.Size}, nil
}

// Account reads /account/info.
//
// limit_used is the fair-use fraction, which is why this fills UsedPercent and
// leaves the byte fields alone: Premiumize publishes no byte ceiling for it
// anywhere, and multiplying the fraction by an invented total would put a
// figure on screen their own account page never shows.
//
// premium_until is null for a free account, hence the pointer: 0 and absent are
// the same thing here, but a plain int64 would also swallow a malformed answer
// as "expired long ago" rather than as "not stated".
func (p *Premiumize) Account(ctx context.Context) (AccountInfo, error) {
	var data struct {
		pmStatus
		PremiumUntil *int64  `json:"premium_until"`
		LimitUsed    float64 `json:"limit_used"`
	}
	if err := p.get(ctx, "/account/info", &data); err != nil {
		return AccountInfo{}, err
	}
	if err := data.err("/account/info"); err != nil {
		return AccountInfo{}, err
	}
	info := AccountInfo{Tier: "free"}
	if data.PremiumUntil != nil && *data.PremiumUntil > 0 {
		until := time.Unix(*data.PremiumUntil, 0).UTC()
		info.ExpiresAt = until
		if until.After(time.Now()) {
			info.Tier = "premium"
		}
	}
	// No "> 0" guard: limit_used is a documented field of this answer, and 0.0
	// from it means the fair-use allowance is untouched, not that Premiumize
	// declined to say - see TrafficInfo.PercentKnown.
	info.Traffic.UsedPercent = data.LimitUsed * 100
	info.Traffic.PercentKnown = true
	return info, nil
}

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

// Premiumize speaks the Premiumize.me API (https://www.premiumize.me/api) with
// the key as Bearer token. Failures arrive as HTTP 200 with status "error",
// so the body decides, never the status line.
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

// pmStatus is the status part of every answer, embedded in each response type.
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

// Hosts asks /services/list for the directdl set, the hosts /transfer/directdl
// can unlock; the cache list only covers what Premiumize holds in its cloud.
// Aliases are added so links written with an alternative domain match too.
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

// Unlock asks /transfer/directdl for one link. The answer is a list because
// src may be a folder; a hoster link is one file, so the first entry is taken.
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
	name := first.Path
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	return Direct{URL: first.Link, Name: name, Size: first.Size}, nil
}

// Account reads /account/info. limit_used is a fair-use fraction without a
// published byte ceiling, so it fills UsedPercent only. premium_until is null
// on a free account.
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
	// limit_used is always present, so 0 means the allowance is untouched.
	info.Traffic.UsedPercent = data.LimitUsed * 100
	info.Traffic.PercentKnown = true
	return info, nil
}

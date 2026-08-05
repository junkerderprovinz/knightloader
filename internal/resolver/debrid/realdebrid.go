package debrid

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RealDebrid speaks the Real-Debrid REST 1.0 API.
// https://api.real-debrid.com/ — auth is a Bearer API token.
type RealDebrid struct {
	token string
	base  string
	hc    *http.Client
}

func NewRealDebrid(token string) *RealDebrid {
	return &RealDebrid{token: token, base: "https://api.real-debrid.com/rest/1.0", hc: &http.Client{Timeout: 30 * time.Second}}
}

func (*RealDebrid) ID() string    { return "realdebrid" }
func (*RealDebrid) Label() string { return "Real-Debrid" }

func (r *RealDebrid) do(ctx context.Context, method, path string, form url.Values, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, r.base+path, body)
	if err != nil {
		return err
	}
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := r.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
			Code  int    `json:"error_code"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			return fmt.Errorf("realdebrid %s: %s", path, e.Error)
		}
		return fmt.Errorf("realdebrid %s: HTTP %d", path, resp.StatusCode)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
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

func (r *RealDebrid) Unlock(ctx context.Context, link string) (Direct, error) {
	var out struct {
		Filename string `json:"filename"`
		Filesize int64  `json:"filesize"`
		Download string `json:"download"`
	}
	if err := r.do(ctx, http.MethodPost, "/unrestrict/link", url.Values{"link": {link}}, &out); err != nil {
		return Direct{}, err
	}
	if out.Download == "" {
		return Direct{}, fmt.Errorf("realdebrid: no direct link returned")
	}
	return Direct{URL: out.Download, Name: out.Filename, Size: out.Filesize}, nil
}

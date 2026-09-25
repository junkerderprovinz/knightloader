package debrid

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// DebridLink speaks the Debrid-Link v2 API (https://debrid-link.com/api_doc/v2)
// with a private API key as Bearer token.
//
// Answers are wrapped as {"success", "value", "error"}, where error is a code
// from the documented list that errorText translates. The API key is used
// instead of going through JD because JD's Debrid-Link login needs an OAuth2
// device confirmation that headless JD never shows.
type DebridLink struct {
	key  string
	base string
	hc   *http.Client
}

func NewDebridLink(key string) *DebridLink {
	return &DebridLink{
		key:  key,
		base: "https://debrid-link.com/api/v2",
		hc:   httpx.New(httpx.Options{Timeout: 30 * time.Second}),
	}
}

func (*DebridLink) ID() string    { return "debridlink" }
func (*DebridLink) Label() string { return "Debrid-Link" }

// dlEnvelope is the uniform wrapper every v2 answer carries. Error is a code
// from the documented table (see errorText).
type dlEnvelope struct {
	Success bool            `json:"success"`
	Value   json.RawMessage `json:"value"`
	Error   string          `json:"error"`
}

func (d *DebridLink) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.base+path, nil)
	if err != nil {
		return err
	}
	return d.send(req, path, out)
}

func (d *DebridLink) postJSON(ctx context.Context, path string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.base+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return d.send(req, path, out)
}

// send performs the call and unwraps the envelope. An HTTP error still carries
// the error code in its body, and a 200 can be success:false.
func (d *DebridLink) send(req *http.Request, path string, out any) error {
	if d.key != "" {
		req.Header.Set("Authorization", "Bearer "+d.key)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := d.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	var env dlEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("debrid-link %s: %s", path, resp.Status)
	}
	if !env.Success {
		return &dlError{path: path, code: env.Error}
	}
	if out != nil && len(env.Value) > 0 {
		return json.Unmarshal(env.Value, out)
	}
	return nil
}

// errorText turns a documented error code this client can provoke into a
// sentence a person can act on, and returns any other code unchanged.
func errorText(code string) string {
	switch code {
	case "badToken":
		return "the API key was refused; generate a new one at debrid-link.com/webapp/apikey"
	case "hidedToken":
		return "this API key is not enabled yet; confirm it in your Debrid-Link account"
	case "notDebrid":
		return "Debrid-Link could not unlock this link; the hoster may be down"
	case "hostNotValid":
		return "Debrid-Link does not support this file hoster"
	case "fileNotFound":
		return "the hoster says this file is gone"
	case "fileNotAvailable":
		return "the hoster says this file is temporarily unavailable"
	case "badFilePassword":
		return "this link needs a password, and the one given was wrong or missing"
	case "notFreeHost":
		return "this hoster is not available on a free Debrid-Link account"
	case "maintenanceHost":
		return "this hoster is in maintenance at Debrid-Link"
	case "maxLink", "maxLinkHost":
		return "the daily link limit for this account is used up"
	case "maxData", "maxDataHost":
		return "the daily traffic limit for this account is used up"
	case "serverNotAllowed":
		return "Debrid-Link refuses requests from this server or VPN; contact them to allow it"
	case "floodDetected":
		return "too many requests: Debrid-Link rate-limited this account, try again in an hour"
	case "":
		return "the call failed and Debrid-Link named no reason"
	}
	return code
}

// dlHost is one entry of /downloader/hosts.
type dlHost struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Status  int      `json:"status"`
	Domains []string `json:"domains"`
}

// Hosts asks /downloader/hosts for file hosters that are online (status >= 1,
// per the documentation). keys= leaves out the per-host regexes, which make
// the full answer several hundred kilobytes.
func (d *DebridLink) Hosts(ctx context.Context) (map[string]bool, error) {
	var hosts []dlHost
	if err := d.get(ctx, "/downloader/hosts?types=host&keys=name,type,status,domains", &hosts); err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, h := range hosts {
		if h.Status < 1 {
			continue
		}
		for _, dom := range h.Domains {
			if dom = NormalizeHost(dom); dom != "" {
				set[dom] = true
			}
		}
	}
	return set, nil
}

// dlLink is the link object /downloader/add answers with.
type dlLink struct {
	Name        string `json:"name"`
	DownloadURL string `json:"downloadUrl"`
	Size        int64  `json:"size"`
}

func (d *DebridLink) Unlock(ctx context.Context, link string) (Direct, error) {
	var raw json.RawMessage
	if err := d.postJSON(ctx, "/downloader/add", map[string]string{"url": link}, &raw); err != nil {
		return Direct{}, err
	}
	got, err := firstLink(raw)
	if err != nil {
		return Direct{}, err
	}
	if got.DownloadURL == "" {
		return Direct{}, errors.New("debrid-link: no direct link returned")
	}
	return Direct{URL: got.DownloadURL, Name: got.Name, Size: got.Size}, nil
}

// firstLink decodes the answer of /downloader/add, which is a link object for
// an ordinary link and an array of them for a folder link. A hoster link is
// one file, so the first entry is taken.
func firstLink(raw json.RawMessage) (dlLink, error) {
	var one dlLink
	if err := json.Unmarshal(raw, &one); err == nil {
		return one, nil
	}
	var many []dlLink
	if err := json.Unmarshal(raw, &many); err != nil {
		return dlLink{}, fmt.Errorf("debrid-link: unreadable answer to /downloader/add: %w", err)
	}
	if len(many) == 0 {
		return dlLink{}, errors.New("debrid-link: the link expanded to nothing")
	}
	return many[0], nil
}

// Account reads /account/infos for the plan and /downloader/limits for what is
// left of today's allowance.
//
// premiumLeft, the seconds of premium remaining, decides both tier and expiry
// because the documentation never states what accountType's numbers mean.
// Traffic is carried as a percentage: Debrid-Link caps bytes per hoster and
// only states usagePercent for the account as a whole.
func (d *DebridLink) Account(ctx context.Context) (AccountInfo, error) {
	var acct struct {
		PremiumLeft int64 `json:"premiumLeft"`
	}
	if err := d.get(ctx, "/account/infos", &acct); err != nil {
		return AccountInfo{}, err
	}
	info := AccountInfo{Tier: "free"}
	if acct.PremiumLeft > 0 {
		info.Tier = "premium"
		info.ExpiresAt = time.Now().Add(time.Duration(acct.PremiumLeft) * time.Second).UTC()
	}

	// A failed limits call still returns the plan read above.
	var limits struct {
		UsagePercent struct {
			Current float64 `json:"current"`
			Value   float64 `json:"value"`
		} `json:"usagePercent"`
		NextResetSeconds struct {
			Value int64 `json:"value"`
		} `json:"nextResetSeconds"`
	}
	if err := d.get(ctx, "/downloader/limits", &limits); err == nil && limits.UsagePercent.Value > 0 {
		info.Traffic.UsedPercent = limits.UsagePercent.Current / limits.UsagePercent.Value * 100
		info.Traffic.PercentKnown = true
		if limits.NextResetSeconds.Value > 0 {
			info.Traffic.ResetsAt = time.Now().Add(time.Duration(limits.NextResetSeconds.Value) * time.Second).UTC()
		}
	}
	return info, nil
}

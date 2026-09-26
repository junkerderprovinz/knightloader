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

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

// AllDebrid speaks the AllDebrid v4 API. https://docs.alldebrid.com
type AllDebrid struct {
	key  string
	base string
	hc   *http.Client
}

func NewAllDebrid(key string) *AllDebrid {
	return &AllDebrid{key: key, base: "https://api.alldebrid.com/v4", hc: httpx.New(httpx.Options{Timeout: 30 * time.Second})}
}

func (*AllDebrid) ID() string    { return "alldebrid" }
func (*AllDebrid) Label() string { return "AllDebrid" }

// adEnvelope is AllDebrid's uniform wrapper: status success|error.
type adEnvelope struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (a *AllDebrid) get(ctx context.Context, path string, q url.Values, out any) error {
	if q == nil {
		q = url.Values{}
	}
	q.Set("agent", "knightloader")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.base+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	return a.send(req, path, out)
}

// post sends a form-encoded call. It is a POST because /link/infos takes
// link[] once per link, and a batch of those belongs in a body.
func (a *AllDebrid) post(ctx context.Context, path string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return a.send(req, path, out)
}

// send performs the call and unwraps AllDebrid's envelope, which reports
// failures as a code in a 200 response. The key goes as a Bearer token, the
// only authentication AllDebrid documents, and never in the address: a failed
// call's error quotes the address, and the task's error shows it to anyone who
// may read the list.
func (a *AllDebrid) send(req *http.Request, path string, out any) error {
	if a.key != "" {
		req.Header.Set("Authorization", "Bearer "+a.key)
	}
	resp, err := a.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	var env adEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("alldebrid %s: %s: %w", path, resp.Status, err)
	}
	if env.Status != "success" {
		if env.Error != nil {
			return fmt.Errorf("alldebrid %s: %s (%s)", path, env.Error.Message, env.Error.Code)
		}
		return fmt.Errorf("alldebrid %s: %s", path, resp.Status)
	}
	if out != nil && len(env.Data) > 0 {
		return json.Unmarshal(env.Data, out)
	}
	return nil
}

// adBatch is how many links go into one /link/infos call. AllDebrid documents
// a rate limit but no maximum array size, so this stays conservative.
const adBatch = 100

// CheckLinks asks /link/infos about a batch of links. The endpoint returns
// name and size but no download link, so it costs no traffic.
//
// Only LINK_DOWN in the per-link error means the file is gone. The other codes
// are maintenance (LINK_TEMPORARY_UNAVAILABLE, LINK_HOST_UNAVAILABLE), a file
// behind a password (LINK_PASS_PROTECTED) or a request problem
// (LINK_IS_MISSING), and read as uncheckable.
func (a *AllDebrid) CheckLinks(ctx context.Context, links []string) ([]core.Availability, error) {
	verdict := make(map[string]core.Availability, len(links))
	for start := 0; start < len(links); start += adBatch {
		end := min(start+adBatch, len(links))
		form := url.Values{}
		for _, l := range links[start:end] {
			form.Add("link[]", l)
		}
		var data struct {
			Infos []adLinkInfo `json:"infos"`
		}
		if err := a.post(ctx, "/link/infos", form, &data); err != nil {
			return nil, err
		}
		for _, info := range data.Infos {
			verdict[info.Link] = info.verdict()
		}
	}
	// Matched by the echoed link because AllDebrid does not promise to keep
	// the request order. A link without an entry ends up uncheckable.
	out := make([]core.Availability, len(links))
	for i, l := range links {
		out[i] = verdict[l]
	}
	return resolver.Answers(out, len(links)), nil
}

// adLinkInfo is one entry of /link/infos. An entry without an error is online.
type adLinkInfo struct {
	Link  string `json:"link"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

func (i adLinkInfo) verdict() core.Availability {
	switch {
	case i.Error == nil:
		return core.AvailOnline
	case i.Error.Code == "LINK_DOWN":
		return core.AvailOffline
	}
	return core.AvailUncheckable
}

func (a *AllDebrid) Hosts(ctx context.Context) (map[string]bool, error) {
	var data struct {
		Hosts map[string]struct {
			Domains []string `json:"domains"`
		} `json:"hosts"`
	}
	if err := a.get(ctx, "/hosts", nil, &data); err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, h := range data.Hosts {
		for _, d := range h.Domains {
			if d = NormalizeHost(d); d != "" {
				set[d] = true
			}
		}
	}
	return set, nil
}

// Account reads /user (https://docs.alldebrid.com/#user) for plan and premium
// expiry. AllDebrid has no account-wide byte cap for premium, only quotas on
// some hosts shared by all users, so a premium account reads Unlimited. A free
// account cannot unlock and keeps zero Traffic.
func (a *AllDebrid) Account(ctx context.Context) (AccountInfo, error) {
	var data struct {
		User struct {
			IsPremium    bool  `json:"isPremium"`
			IsTrial      bool  `json:"isTrial"`
			PremiumUntil int64 `json:"premiumUntil"`
		} `json:"user"`
	}
	if err := a.get(ctx, "/user", nil, &data); err != nil {
		return AccountInfo{}, err
	}
	u := data.User
	info := AccountInfo{Tier: "free", Traffic: TrafficInfo{Unlimited: u.IsPremium}}
	if u.IsTrial {
		info.Tier = "trial"
	}
	if u.IsPremium {
		info.Tier = "premium"
	}
	if u.PremiumUntil > 0 {
		info.ExpiresAt = time.Unix(u.PremiumUntil, 0).UTC()
	}
	return info, nil
}

func (a *AllDebrid) Unlock(ctx context.Context, link string) (Direct, error) {
	var data struct {
		Link     string `json:"link"`
		Filename string `json:"filename"`
		Filesize int64  `json:"filesize"`
	}
	if err := a.get(ctx, "/link/unlock", url.Values{"link": {link}}, &data); err != nil {
		return Direct{}, err
	}
	if data.Link == "" {
		return Direct{}, fmt.Errorf("alldebrid: no direct link returned")
	}
	return Direct{URL: data.Link, Name: data.Filename, Size: data.Filesize}, nil
}

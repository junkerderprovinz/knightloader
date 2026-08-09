package debrid

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
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
	if a.key != "" {
		q.Set("apikey", a.key)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.base+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
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

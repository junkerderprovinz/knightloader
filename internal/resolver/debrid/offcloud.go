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

// Offcloud speaks the Offcloud API (github.com/Offcloud/offcloud-api): the key
// goes in the query string and POST /instant unlocks a link.
//
// The supported-sites endpoint POST /api/sites exists but its answer is not
// documented, so parseSites accepts several shapes. An unrecognised shape
// yields no hosts, which leaves links to the next backend instead of claiming
// ones this service cannot unlock.
type Offcloud struct {
	key  string
	base string
	hc   *http.Client
}

func NewOffcloud(key string) *Offcloud {
	return &Offcloud{key: key, base: "https://offcloud.com/api", hc: httpx.New(httpx.Options{Timeout: 30 * time.Second})}
}

func (*Offcloud) ID() string    { return "offcloud" }
func (*Offcloud) Label() string { return "Offcloud" }

// post sends a form-encoded call with the key in the query string, as the
// documentation recommends.
func (o *Offcloud) post(ctx context.Context, path string, form url.Values) ([]byte, error) {
	u := o.base + path
	if o.key != "" {
		u += "?key=" + url.QueryEscape(o.key)
	}
	body := strings.NewReader(form.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := o.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	// A bad key answers 401 {"error":"NOAUTH"}, the only reliable signal for
	// a credential check.
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("offcloud %s: the API key was refused", path)
	}
	return raw, nil
}

func (o *Offcloud) Hosts(ctx context.Context) (map[string]bool, error) {
	raw, err := o.post(ctx, "/sites", nil)
	if err != nil {
		return nil, err
	}
	if msg := offcloudError(raw); msg != "" {
		return nil, fmt.Errorf("offcloud /sites: %s", msg)
	}
	set := parseSites(raw)
	if len(set) == 0 {
		return nil, errors.New("offcloud: /sites answered in a shape this build cannot read")
	}
	return set, nil
}

// parseSites takes every domain-shaped string out of an answer whose shape is
// not published: a flat array of names, an array of objects, or a map of
// category to names. Requiring a domain shape keeps labels and ids out of the
// routing table.
func parseSites(raw json.RawMessage) map[string]bool {
	set := map[string]bool{}
	add := func(s string) {
		if d := NormalizeHost(s); d != "" && strings.Contains(d, ".") && !strings.ContainsAny(d, " /:") {
			set[d] = true
		}
	}

	var flat []string
	if json.Unmarshal(raw, &flat) == nil && len(flat) > 0 {
		for _, s := range flat {
			add(s)
		}
		return set
	}

	var objects []map[string]json.RawMessage
	if json.Unmarshal(raw, &objects) == nil && len(objects) > 0 {
		for _, obj := range objects {
			for _, v := range obj {
				var s string
				if json.Unmarshal(v, &s) == nil {
					add(s)
				}
			}
		}
		return set
	}

	var grouped map[string][]string
	if json.Unmarshal(raw, &grouped) == nil {
		for _, names := range grouped {
			for _, s := range names {
				add(s)
			}
		}
	}
	return set
}

// offcloudError reads the two documented refusals: an "error" message, and
// the "not_available" word naming the add-on the account lacks. It returns ""
// when the answer carries neither.
func offcloudError(raw json.RawMessage) string {
	var body struct {
		Error        string `json:"error"`
		NotAvailable string `json:"not_available"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return ""
	}
	if body.Error != "" {
		return body.Error
	}
	switch body.NotAvailable {
	case "":
		return ""
	case "premium":
		return "this download needs the premium add-on on your Offcloud account"
	case "links":
		return "this download needs the link-increase add-on on your Offcloud account"
	case "proxy":
		return "this download needs the proxy add-on on your Offcloud account"
	case "video":
		return "this download needs the video-site add-on on your Offcloud account"
	}
	return "Offcloud refused this download (" + body.NotAvailable + ")"
}

func (o *Offcloud) Unlock(ctx context.Context, link string) (Direct, error) {
	raw, err := o.post(ctx, "/instant", url.Values{"url": {link}})
	if err != nil {
		return Direct{}, err
	}
	if msg := offcloudError(raw); msg != "" {
		return Direct{}, fmt.Errorf("offcloud: %s", msg)
	}
	var got struct {
		FileName string `json:"fileName"`
		URL      string `json:"url"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		return Direct{}, errors.New("offcloud: unreadable answer to /instant")
	}
	if got.URL == "" {
		return Direct{}, errors.New("offcloud: no direct link returned")
	}
	// /instant reports no size; the engine takes it from Content-Length.
	return Direct{URL: got.URL, Name: got.FileName}, nil
}

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

// Offcloud speaks the Offcloud API.
//
// WHERE THIS ONE'S FIELD NAMES COME FROM, in two halves that differ in how
// certain they are:
//
//   - DOCUMENTED by Offcloud themselves (github.com/Offcloud/offcloud-api, read
//     2026-09-06): the base is https://offcloud.com/api, the key travels as the
//     query parameter "?key=", and POST /instant takes `url` and answers
//     {requestId, fileName, url, site, status, originalLink, createdOn}. A
//     refusal answers the single word in `not_available` (premium, links,
//     proxy, video) or an `error` message. "All requests return JSON, including
//     errors."
//   - NOT DOCUMENTED anywhere: a list of supported sites. Their README has no
//     such endpoint at all. POST /api/sites?key= exists - measured 2026-09-06,
//     it answers 401 {"error":"NOAUTH"} without a key rather than 404 - but its
//     response shape is published nowhere, so parseSites below accepts three
//     plausible shapes and takes whatever looks like a domain out of them.
//
// jdp asked for it on those terms (2026-09-06: "Jetzt einbauen, Feldnamen aus
// fremden Bibliotheken"). The failure mode is bounded on purpose: a shape
// parseSites does not recognise yields an empty set, which makes this resolver
// claim NO links at all rather than claim links it cannot then unlock. An
// unclaimed link falls through to the next backend; a wrongly claimed one stops
// every other backend from getting its turn, and that is the difference this
// design is choosing between.
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

// post sends a form-encoded call with the key in the query string, which is
// what the documentation calls "the best way to authentificate".
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
	// The status line matters here, unlike Premiumize's: an unauthenticated
	// call answers 401 with {"error":"NOAUTH"}, and that is the one signal a
	// credential check can rely on.
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

// parseSites takes every domain-shaped string out of an answer whose exact
// shape is not published. Three are tried, because those are the three an
// endpoint like this is written as in practice: a flat array of names, an array
// of objects, or a map of category to names. Anything else contributes nothing.
//
// "Domain-shaped" is doing real work: it is what keeps a category label, a
// status word or an id out of a routing table where an entry means "this
// resolver will handle every link on that host".
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

// offcloudError reads the two documented ways this API says no: an `error`
// message, and the `not_available` word that names which add-on the account is
// missing. Returns "" when the answer carries neither.
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
	// No size: /instant does not report one, and the engine learns it from the
	// Content-Length of the transfer it is about to start anyway. A zero here
	// means "not stated", which every reader of Direct already handles.
	return Direct{URL: got.URL, Name: got.FileName}, nil
}

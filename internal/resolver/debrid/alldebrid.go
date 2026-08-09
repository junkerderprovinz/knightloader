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
	if a.key != "" {
		q.Set("apikey", a.key)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.base+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	return a.send(req, path, out)
}

// post sends a form-encoded call with the key in an Authorization header, which
// is the only authentication AllDebrid documents today ("You must send the
// apikey ... in an Authorization: Bearer header"; the agent parameter went in
// January 2025). The GET dialect above is left exactly as it is because it is
// what this build has been shipping and working with - this is a new call, so it
// speaks the documented form rather than inheriting a deprecated one.
//
// It is also a POST because the parameter is an array: /link/infos takes link[]
// once per link, and fifty of those belong in a body, not in a query string.
func (a *AllDebrid) post(ctx context.Context, path string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if a.key != "" {
		req.Header.Set("Authorization", "Bearer "+a.key)
	}
	return a.send(req, path, out)
}

// send performs the call and unwraps AllDebrid's envelope, which is where the
// real failure lives: the transport is happy, the status is 200, and the reason
// nothing worked is a code inside the body.
func (a *AllDebrid) send(req *http.Request, path string, out any) error {
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

// adBatch is how many links go into one /link/infos call. AllDebrid documents a
// rate limit (12 requests a second, 600 a minute) but no ceiling on the size of
// the array, so this is chosen low enough that no answer to that question can
// break it, and high enough that a two-hundred-link collector is four calls.
const adBatch = 100

// CheckLinks asks /link/infos about a batch of links.
//
// Free, and that was established before it was wired: /link/infos returns the
// file name and size a hoster reports and hands back no download link at all, so
// there is nothing for the account to be billed for. Traffic on this service is
// spent by /link/unlock, which is the call the download path makes and this one
// deliberately does not.
//
// The verdicts come from the per-link error object, which is the only place
// AllDebrid says a link is dead: the envelope is still status "success" when
// every link in it is gone. Only LINK_DOWN ("This link is not available on the
// file hoster website") is the host saying the file is not there. Everything
// else is uncheckable and deliberately so - LINK_TEMPORARY_UNAVAILABLE and
// LINK_HOST_UNAVAILABLE are maintenance, LINK_PASS_PROTECTED is a file that
// demonstrably exists behind a password, and LINK_IS_MISSING is not about the
// link at all ("No link was sent"), which is exactly the sort of code that gets
// read as "gone" by anything matching on the name.
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
	// Keyed by the link the service echoed rather than by position: nothing in
	// AllDebrid's answer promises the order of the request, and reading it back
	// positionally is how every verdict after one re-ordered entry lands on the
	// wrong row. A link with no entry at all falls through to uncheckable.
	out := make([]core.Availability, len(links))
	for i, l := range links {
		out[i] = verdict[l]
	}
	return resolver.Answers(out, len(links)), nil
}

// adLinkInfo is one entry of /link/infos. The error is a pointer because its
// absence is the answer: an entry that carries none is a link the hoster
// described, which is the only "online" this endpoint gives.
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

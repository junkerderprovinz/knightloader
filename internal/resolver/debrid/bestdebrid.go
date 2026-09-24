package debrid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// BestDebrid speaks the BestDebrid API (https://bestdebrid.com/en/restapi.php).
// The key goes in the Authorization header, as the documentation recommends,
// so it never becomes part of a URL.
type BestDebrid struct {
	key  string
	base string
	hc   *http.Client
}

func NewBestDebrid(apiKey string) *BestDebrid {
	return &BestDebrid{
		key:  apiKey,
		base: "https://bestdebrid.com/api/v1",
		// A direct link only works from the address that asked for it, and
		// downloads go out directly (see internal/netproxy), so a call made
		// through HTTP_PROXY would bind every link to the wrong address.
		hc: httpx.New(httpx.Options{Timeout: 30 * time.Second, Proxy: httpx.NoProxy}),
	}
}

func (*BestDebrid) ID() string    { return "bestdebrid" }
func (*BestDebrid) Label() string { return "BestDebrid" }

// call sends one request and returns the body. Failures come as a non-zero
// "error" code in the body, which is read before the status line so
// BestDebrid's own message survives.
func (b *BestDebrid) call(ctx context.Context, path string, form url.Values) ([]byte, error) {
	method, body := http.MethodGet, io.Reader(nil)
	if form != nil {
		method, body = http.MethodPost, strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, b.base+path, body)
	if err != nil {
		return nil, err
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Accept", "application/json")
	if b.key != "" {
		req.Header.Set("Authorization", b.key)
	}
	resp, err := b.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("bestdebrid %s: unreadable answer (%s)", path, resp.Status)
	}
	// The host list is an array, which does not decode here and carries no
	// error code either.
	var status struct {
		Error   json.RawMessage `json:"error"`
		Message json.RawMessage `json:"message"`
	}
	var msg string
	if json.Unmarshal(raw, &status) == nil {
		_ = json.Unmarshal(status.Message, &msg)
		msg = strings.TrimSpace(msg)
		if code := looseInt(status.Error); code != 0 {
			return nil, bestdebridError(path, code, msg)
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if msg == "" {
			return nil, fmt.Errorf("bestdebrid %s: %s", path, resp.Status)
		}
		return nil, fmt.Errorf("bestdebrid %s: %s (%s)", path, msg, resp.Status)
	}
	return raw, nil
}

// bestdebridReasons words the codes from the documented error table that need
// different action from the user. Codes whose entry lumps unrelated causes
// together (6, 7, 55, 56) are left to BestDebrid's own message.
var bestdebridReasons = map[int64]string{
	2:   "BestDebrid could not read this link",
	3:   "BestDebrid does not support this link, or did not accept the key",
	4:   "this hoster needs a premium plan",
	9:   "the daily limit for this hoster is used up",
	10:  "the link is dead or could not be unlocked",
	19:  "the daily limit for this hoster is used up",
	21:  "the API needs a premium plan",
	22:  "API access is blocked for this account",
	121: "the daily limit of files for this account is used up",
	122: "too many unlocks are in progress, try again in a minute",
	632: "BestDebrid refuses VPN and proxy addresses on this plan",
}

// bestdebridError names the service and keeps its message, led by a plain
// reading of the code where the table has one.
func bestdebridError(path string, code int64, msg string) error {
	if msg == "" {
		msg = "error " + strconv.FormatInt(code, 10)
	}
	if why, ok := bestdebridReasons[code]; ok {
		return fmt.Errorf("bestdebrid %s: %s (%s)", path, why, msg)
	}
	return fmt.Errorf("bestdebrid %s: %s", path, msg)
}

// bestdebridHost is one entry of /hosts. Domains holds the hoster's domain and
// its aliases; name is usually a domain as well and is all an entry without
// domains has.
type bestdebridHost struct {
	Name    string   `json:"name"`
	Status  string   `json:"status"`
	Domains []string `json:"domains"`
}

// bestdebridHostList reads /hosts as the documented array or as an object keyed
// "0", "1", and so on, a shape JDownloader's plugin also handles. An entry that
// does not decode is skipped rather than failing the list.
func bestdebridHostList(raw []byte) []bestdebridHost {
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil {
		var keyed map[string]json.RawMessage
		if json.Unmarshal(raw, &keyed) != nil {
			return nil
		}
		for _, v := range keyed {
			entries = append(entries, v)
		}
	}
	hosts := make([]bestdebridHost, 0, len(entries))
	for _, e := range entries {
		var h bestdebridHost
		if json.Unmarshal(e, &h) == nil {
			hosts = append(hosts, h)
		}
	}
	return hosts
}

// bestdebridAliases are domains BestDebrid takes for a host that an entry
// without domains cannot list: the API rewrites rg.to and keep2share.cc to the
// host itself, and Filestore's name alone is "filestore".
var bestdebridAliases = map[string][]string{
	"rapidgator.net": {"rg.to"},
	"k2s.cc":         {"keep2share.cc"},
	"filestore":      {"filestore.me"},
}

func (b *BestDebrid) Hosts(ctx context.Context) (map[string]bool, error) {
	raw, err := b.call(ctx, "/hosts", nil)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, h := range bestdebridHostList(raw) {
		// An empty status counts as up so a missing field cannot empty the
		// routing table.
		if s := strings.ToLower(strings.TrimSpace(h.Status)); s != "" && s != "up" {
			continue
		}
		for _, d := range append([]string{h.Name}, h.Domains...) {
			if d = NormalizeHost(d); strings.Contains(d, ".") {
				set[d] = true
			}
			for _, alias := range bestdebridAliases[d] {
				set[alias] = true
			}
		}
	}
	if len(set) == 0 {
		return nil, errors.New("bestdebrid: /hosts listed no host that is up")
	}
	return set, nil
}

func (b *BestDebrid) Unlock(ctx context.Context, link string) (Direct, error) {
	// The link is bound to an address. ip is left out so that address is the
	// one this call comes from, which is also where the download leaves from.
	raw, err := b.call(ctx, "/generateLink", url.Values{"link": {link}})
	if err != nil {
		return Direct{}, err
	}
	var got struct {
		Link     string          `json:"link"`
		Filename string          `json:"filename"`
		Size     json.RawMessage `json:"size"`
		Message  json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		return Direct{}, errors.New("bestdebrid: unreadable answer to /generateLink")
	}
	if got.Link == "" {
		// Some refusals come with error 0, and then the message is the only
		// reason given.
		var msg string
		_ = json.Unmarshal(got.Message, &msg)
		if msg = strings.TrimSpace(msg); msg != "" && !strings.EqualFold(msg, "OK") {
			return Direct{}, fmt.Errorf("bestdebrid /generateLink: no direct link returned (%s)", msg)
		}
		return Direct{}, errors.New("bestdebrid: no direct link returned")
	}
	// size is documented as rounded to a unit ("1.35 GiB"), which would make a
	// complete file look short or long, so only an exact byte count is kept.
	return Direct{URL: got.Link, Name: got.Filename, Size: looseInt(got.Size)}, nil
}

// Account reads /user. It carries no traffic figures, and BestDebrid limits
// files per day and per host rather than bytes, so an account that can
// download reads Unlimited.
func (b *BestDebrid) Account(ctx context.Context) (AccountInfo, error) {
	raw, err := b.call(ctx, "/user", nil)
	if err != nil {
		return AccountInfo{}, err
	}
	var u struct {
		Premium json.RawMessage `json:"premium"`
		Expire  json.RawMessage `json:"expire"`
		Bypass  json.RawMessage `json:"bypass_api_limit"`
	}
	if err := json.Unmarshal(raw, &u); err != nil {
		return AccountInfo{}, errors.New("bestdebrid: unreadable answer to /user")
	}
	info := AccountInfo{Tier: "free"}
	// expire has no zone. Read as UTC it can come out up to two hours late
	// against the server's clock, close enough for an expiry date.
	var expire string
	if json.Unmarshal(u.Expire, &expire) == nil {
		if t, err := time.Parse(time.DateTime, strings.TrimSpace(expire)); err == nil {
			info.ExpiresAt = t
		}
	}
	switch {
	case bestdebridFlag(u.Premium) || info.ExpiresAt.After(time.Now()):
		info.Tier = "premium"
	case bestdebridFlag(u.Bypass):
		// JDownloader's plugin calls these accounts resellers: they pay for
		// downloads from their credit instead of holding a plan.
		info.Tier = "reseller"
	}
	info.Traffic.Unlimited = info.Tier != "free"
	return info, nil
}

// HostLimit satisfies HostLimiter with one connection for every host, the
// setting BestDebrid's admin asked JDownloader's plugin to use.
func (*BestDebrid) HostLimit(string) int { return 1 }

// bestdebridFlag reads a yes/no field sent as a JSON bool, a number or a
// string.
func bestdebridFlag(raw json.RawMessage) bool {
	switch strings.ToLower(strings.Trim(string(raw), "\" ")) {
	case "true", "1", "yes":
		return true
	}
	return false
}

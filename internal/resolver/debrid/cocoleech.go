// Cocoleech publishes no API documentation. The calls, parameters and field
// names here follow the two maintained open clients, which agree on them:
// JDownloader's CocoleechCom.java
// (https://github.com/mycodedoesnotcompile2/jdownloader_mirror/blob/main/svn_trunk/src/jd/plugins/hoster/CocoleechCom.java)
// and ResolveURL's cocoleech.py
// (https://github.com/Gujal00/ResolveURL/blob/master/script.module.resolveurl/lib/resolveurl/plugins/cocoleech.py).
// The two host lists answer without a key and were checked against the live
// service.

package debrid

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// CocoLeech speaks the Cocoleech premium-link API with the key a user creates
// at members.cocoleech.com/settings, sent as the "key" query parameter.
type CocoLeech struct {
	key  string
	base string
	hc   *http.Client

	// readyEvery and readyFor pace the wait for a fresh direct link (see
	// ready).
	readyEvery time.Duration
	readyFor   time.Duration

	// chunkMu guards chunkCaps, written by Unlock on download goroutines and
	// read by HostLimit from dispatch.
	chunkMu sync.Mutex
	// chunkCaps holds the smallest "chunks" reported per hoster host.
	chunkCaps map[string]int
}

// cocoleechChunks is the connection cap for a host no unlock answer has named
// one for. JDownloader's plugin uses it at Cocoleech's own request.
const cocoleechChunks = 4

func NewCocoLeech(apiKey string) *CocoLeech {
	return &CocoLeech{
		key:        apiKey,
		base:       "https://members.cocoleech.com/auth",
		hc:         httpx.New(httpx.Options{Timeout: 30 * time.Second}),
		readyEvery: 5 * time.Second,
		readyFor:   time.Minute,
	}
}

func (*CocoLeech) ID() string    { return "cocoleech" }
func (*CocoLeech) Label() string { return "CocoLeech" }

// fetch performs one GET and returns the JSON part of the answer. Errors name
// the path only, because the URL carries the key.
func (c *CocoLeech) fetch(ctx context.Context, path string, q url.Values) ([]byte, error) {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, cocoleechScrub(path, err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, cocoleechScrub(path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("cocoleech %s: %w", path, err)
	}
	// Some answers arrive with a stray "1" in front of the JSON, which both
	// open clients skip. An error page is not JSON after the skip either, and
	// its status is the useful part.
	i := bytes.IndexAny(raw, "{[")
	if i < 0 || !json.Valid(raw[i:]) {
		return nil, fmt.Errorf("cocoleech %s: unreadable answer (%s)", path, resp.Status)
	}
	return raw[i:], nil
}

// cocoleechScrub drops the request URL from a transport error, since
// *url.Error prints it and with it the key.
func cocoleechScrub(path string, err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return fmt.Errorf("cocoleech %s: %w", path, err)
}

// call fetches a keyed endpoint and decodes it into out. A refusal is any
// answer with a message or with status "100". The status is "100" whatever the
// reason, so only the message tells refusals apart.
func (c *CocoLeech) call(ctx context.Context, path string, q url.Values, out any) error {
	raw, err := c.fetch(ctx, path, q)
	if err != nil {
		return err
	}
	var ans struct {
		Status  json.RawMessage `json:"status"`
		Message json.RawMessage `json:"message"`
	}
	if json.Unmarshal(raw, &ans) != nil {
		return fmt.Errorf("cocoleech %s: unreadable answer", path)
	}
	if msg := cocoleechText(ans.Message); msg != "" {
		return cocoleechRefusal(msg)
	}
	if cocoleechText(ans.Status) == "100" {
		return fmt.Errorf("cocoleech %s: refused without a reason", path)
	}
	if out != nil && json.Unmarshal(raw, out) != nil {
		return fmt.Errorf("cocoleech %s: unreadable answer", path)
	}
	return nil
}

// cocoleechRefusal words a refusal and keeps the service's message. Only the
// sentences JDownloader's plugin handles are known, so the other cases are
// matched by keyword.
func cocoleechRefusal(msg string) error {
	m := strings.ToLower(msg)
	switch {
	case m == "incorrect api key." || m == "incorrect log-in or password.":
		return fmt.Errorf("cocoleech: the API key was refused (%s); create a new one at members.cocoleech.com/settings", msg)
	case m == "premium membership expired.":
		return fmt.Errorf("cocoleech: the premium membership has run out (%s)", msg)
	case strings.Contains(m, "blocked"):
		return fmt.Errorf("cocoleech: this IP address is blocked for today (%s)", msg)
	case strings.Contains(m, "dead"):
		return fmt.Errorf("cocoleech: the hoster says this file is gone (%s)", msg)
	case strings.Contains(m, "limit") || strings.Contains(m, "quota"):
		return fmt.Errorf("cocoleech: a daily limit is used up (%s)", msg)
	case strings.Contains(m, "not supported") || strings.Contains(m, "unsupported"):
		return fmt.Errorf("cocoleech: this file hoster is not supported (%s)", msg)
	}
	return fmt.Errorf("cocoleech: %s", msg)
}

// cocoleechText reads a field sent as a string or as a number, and returns ""
// for anything else.
func cocoleechText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

// Authenticate checks the key against /api/info. Both host lists answer
// without a key, so Hosts alone would accept a wrong one.
func (c *CocoLeech) Authenticate(ctx context.Context) error {
	if strings.TrimSpace(c.key) == "" {
		return errors.New("cocoleech: an API key is required")
	}
	return c.call(ctx, "/api/info", url.Values{"key": {c.key}}, nil)
}

// Hosts reads /api/domains, which carries each host's alias domains, and drops
// the hosts /api/hosts-status marks as anything but online.
func (c *CocoLeech) Hosts(ctx context.Context) (map[string]bool, error) {
	raw, err := c.fetch(ctx, "/api/domains", nil)
	if err != nil {
		return nil, err
	}
	var entries []struct {
		Name    string   `json:"name"`
		Domains []string `json:"domains"`
	}
	if json.Unmarshal(raw, &entries) != nil {
		return nil, errors.New("cocoleech /api/domains: unreadable answer")
	}
	down := c.downHosts(ctx)
	set := map[string]bool{}
	for _, e := range entries {
		names := append([]string{e.Name}, e.Domains...)
		if slices.ContainsFunc(names, func(n string) bool { return down[NormalizeHost(n)] }) {
			continue
		}
		for _, d := range names {
			if d = NormalizeHost(d); strings.Contains(d, ".") {
				set[d] = true
			}
		}
	}
	if len(set) == 0 {
		return nil, errors.New("cocoleech: /api/domains named no hosts")
	}
	return set, nil
}

// downHosts is the set of hosts /api/hosts-status reports as not online, the
// test JDownloader's plugin applies. The list has been seen calling long-closed
// hosts online, so it only ever removes hosts, and a failed call removes none.
func (c *CocoLeech) downHosts(ctx context.Context) map[string]bool {
	raw, err := c.fetch(ctx, "/api/hosts-status", nil)
	if err != nil {
		return nil
	}
	var st struct {
		Result []struct {
			Host   string          `json:"host"`
			Status json.RawMessage `json:"status"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &st) != nil {
		return nil
	}
	down := map[string]bool{}
	for _, h := range st.Result {
		// An empty status counts as up so a missing field cannot empty the
		// routing table.
		s := cocoleechText(h.Status)
		if s != "" && !strings.EqualFold(s, "online") {
			down[NormalizeHost(h.Host)] = true
		}
	}
	return down
}

// Unlock asks /api for a direct link. The answer carries no file name or size,
// so the engine takes both from the download itself.
func (c *CocoLeech) Unlock(ctx context.Context, link string) (Direct, error) {
	var ans struct {
		Download json.RawMessage `json:"download"`
		// Chunks is how many connections this file may open.
		Chunks json.RawMessage `json:"chunks"`
	}
	if err := c.call(ctx, "/api", url.Values{"key": {c.key}, "link": {link}}, &ans); err != nil {
		return Direct{}, err
	}
	dl := cocoleechText(ans.Download)
	if dl == "" {
		return Direct{}, errors.New("cocoleech: no direct link returned")
	}
	c.rememberChunkCap(link, cocoleechText(ans.Chunks))
	if err := c.ready(ctx, dl); err != nil {
		return Direct{}, err
	}
	return Direct{URL: dl}, nil
}

// rememberChunkCap records the smallest "chunks" reported for link's host, as
// RealDebrid does, since no endpoint lists a cap per host. A missing or zero
// field is not recorded, so the host keeps the default.
func (c *CocoLeech) rememberChunkCap(link, chunks string) {
	n, err := strconv.Atoi(chunks)
	if err != nil || n <= 0 {
		return
	}
	u, err := url.Parse(link)
	if err != nil || u.Hostname() == "" {
		return
	}
	host := NormalizeHost(u.Hostname())
	c.chunkMu.Lock()
	defer c.chunkMu.Unlock()
	if c.chunkCaps == nil {
		c.chunkCaps = map[string]int{}
	}
	if cur, ok := c.chunkCaps[host]; !ok || n < cur {
		c.chunkCaps[host] = n
	}
}

// HostLimit satisfies HostLimiter. Until an unlock answer has named a cap for
// host it is cocoleechChunks, never 0, because Cocoleech asks for that limit on
// every host.
func (c *CocoLeech) HostLimit(host string) int {
	c.chunkMu.Lock()
	defer c.chunkMu.Unlock()
	if n := c.chunkCaps[NormalizeHost(host)]; n > 0 {
		return n
	}
	return cocoleechChunks
}

// ready waits until a fresh direct link stops answering with an HTTP error,
// polling it as ResolveURL does, because Cocoleech hands the link out before
// the file behind it is ready. After readyFor the link is handed on anyway and
// the engine reports whatever it then gets.
func (c *CocoLeech) ready(ctx context.Context, link string) error {
	deadline := time.Now().Add(c.readyFor)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, link, nil)
		if err != nil {
			return nil
		}
		if resp, err := c.hc.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode < 400 {
				return nil
			}
		}
		if time.Now().Add(c.readyEvery).After(deadline) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.readyEvery):
		}
	}
}

// Account reads /api/info. As in JDownloader's plugin, an account is premium
// when type says so or expire_date lies ahead, because type has been seen
// reading "free" on a paid account.
func (c *CocoLeech) Account(ctx context.Context) (AccountInfo, error) {
	var d struct {
		Type        json.RawMessage `json:"type"`
		TrafficLeft json.RawMessage `json:"traffic_left"`
		ExpireDate  json.RawMessage `json:"expire_date"`
	}
	if err := c.call(ctx, "/api/info", url.Values{"key": {c.key}}, &d); err != nil {
		return AccountInfo{}, err
	}
	info := AccountInfo{Tier: "free"}
	// expire_date carries no zone, so it is read as UTC.
	if t, err := time.Parse(time.DateTime, cocoleechText(d.ExpireDate)); err == nil {
		info.ExpiresAt = t
	}
	if !strings.EqualFold(cocoleechText(d.Type), "premium") && !info.ExpiresAt.After(time.Now()) {
		// A free account cannot download anything, so it keeps zero Traffic.
		return info, nil
	}
	info.Tier = "premium"
	left := cocoleechText(d.TrafficLeft)
	if strings.EqualFold(left, "unlimited") {
		info.Traffic.Unlimited = true
	} else if n, err := strconv.ParseInt(left, 10, 64); err == nil && n > 0 {
		// Only the bytes left are reported, so they become an allowance with
		// nothing spent, which reads back as the amount left.
		info.Traffic.LimitBytes = n
	}
	return info, nil
}

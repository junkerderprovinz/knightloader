// CoolDebrid publishes no API documentation, and the page that hands out the
// token (cooldebrid.com/api.html) sits behind the login. The public host list
// and the error envelope were measured against the live service. The token
// calls (/user, /traffic, /link/unlock) and the error codes follow the only
// open client, JDownloader's CooldebridCom.java:
// https://github.com/mycodedoesnotcompile2/jdownloader_mirror/blob/main/svn_trunk/src/jd/plugins/hoster/CooldebridCom.java

package debrid

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// CoolDebrid speaks the CoolDebrid v1 API with the API token as Bearer token.
// Everything decodes loosely, so a renamed field makes a call fail with an
// error instead of producing a bad link.
type CoolDebrid struct {
	token string
	base  string
	hc    *http.Client
}

func NewCoolDebrid(token string) *CoolDebrid {
	return &CoolDebrid{
		// A token copied off the account page often brings a line break along,
		// which would make every request fail before it is sent.
		token: strings.TrimSpace(token),
		base:  "https://cooldebrid.com/api/v1",
		hc:    httpx.New(httpx.Options{Timeout: 30 * time.Second}),
	}
}

func (*CoolDebrid) ID() string    { return "cooldebrid" }
func (*CoolDebrid) Label() string { return "CoolDebrid" }

// cooldebridEnvelope is the wrapper every answer carries, a refusal included.
type cooldebridEnvelope struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
	Error  struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// do sends one call and unwraps the envelope. Refusals arrive as HTTP 400 or
// 401 carrying the same envelope, so the body decides, not the status line.
func (c *CoolDebrid) do(ctx context.Context, method, path string, body, out any) error {
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	var env cooldebridEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("cooldebrid %s: unreadable answer (%s)", path, resp.Status)
	}
	if !strings.EqualFold(env.Status, "success") {
		if env.Error.Code == "" && env.Error.Message == "" {
			return fmt.Errorf("cooldebrid %s: refused without a reason (%s)", path, resp.Status)
		}
		reason := cooldebridReason(env.Error.Code, env.Error.Message)
		if c.token != "" {
			// The message is the service's own text, and one that quotes the
			// token back would carry it into the task list and the log.
			reason = strings.ReplaceAll(reason, c.token, "********")
		}
		return fmt.Errorf("cooldebrid %s: %s", path, reason)
	}
	if out != nil && len(env.Data) > 0 {
		// The call succeeded, so a field of an unexpected type only stays empty.
		_ = json.Unmarshal(env.Data, out)
	}
	return nil
}

// cooldebridReason puts a sentence a person can act on in front of the
// service's own message, for the codes JDownloader's plugin handles. No code
// for a dead file is known, so one reads as the service's message and code.
func cooldebridReason(code, msg string) string {
	var why string
	switch code {
	case "AUTH_MISSING_TOKEN", "AUTH_BAD_TOKEN", "AUTH_TOKEN_REVOKED", "AUTH_USER_NOT_FOUND":
		why = "the API token was refused, copy it again from cooldebrid.com/api.html"
	case "AUTH_USER_BANNED":
		why = "this CoolDebrid account is banned"
	case "AUTH_NOT_PREMIUM":
		why = "the CoolDebrid API only works with a premium account"
	case "AUTH_IP_BLOCKED":
		why = "CoolDebrid blocks requests from this IP address"
	case "RATE_LIMIT_EXCEEDED":
		why = "too many requests, CoolDebrid wants a pause of a few minutes"
	case "HOST_NOT_SUPPORTED":
		why = "CoolDebrid does not support this file hoster"
	case "HOST_OFFLINE":
		why = "this file hoster is offline at CoolDebrid"
	case "DAILY_LINK_LIMIT":
		why = "the daily link limit for this account is used up"
	case "DAILY_BW_LIMIT":
		why = "the daily traffic limit for this account is used up"
	case "HOST_DAILY_LIMIT", "HOST_COUNT_LIMIT":
		why = "today's allowance for this file hoster is used up"
	}
	switch {
	case why != "" && msg != "":
		return why + " (" + msg + ")"
	case why != "":
		return why
	case code == "":
		return msg
	case msg == "":
		return code
	}
	return msg + " (" + code + ")"
}

// Hosts reads the public host list, which names one domain per host and no
// aliases. Degraded hosts stay in because they still unlock, if less reliably.
func (c *CoolDebrid) Hosts(ctx context.Context) (map[string]bool, error) {
	var data struct {
		Hosts []struct {
			Host   string `json:"host"`
			Status string `json:"status"`
		} `json:"hosts"`
	}
	if err := c.do(ctx, http.MethodGet, "/hosts", nil, &data); err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, h := range data.Hosts {
		if strings.EqualFold(strings.TrimSpace(h.Status), "offline") {
			continue
		}
		if d := NormalizeHost(h.Host); strings.Contains(d, ".") {
			set[d] = true
		}
	}
	if len(set) == 0 {
		return nil, errors.New("cooldebrid: /hosts named no hosts")
	}
	return set, nil
}

// Authenticate checks the token against /user. The host list needs no token,
// so it cannot tell a wrong one from a right one.
func (c *CoolDebrid) Authenticate(ctx context.Context) error {
	if c.token == "" {
		return errors.New("cooldebrid: an API token is required")
	}
	return c.do(ctx, http.MethodGet, "/user", nil, nil)
}

// Unlock posts the link to /link/unlock. The answer names no file name or size
// that any client reads, so the engine takes both from the download itself.
func (c *CoolDebrid) Unlock(ctx context.Context, link string) (Direct, error) {
	var data struct {
		DownloadURL string `json:"download_url"`
		// UnlockedURL is what the API's first version called the same field.
		UnlockedURL string `json:"unlocked_url"`
	}
	if err := c.do(ctx, http.MethodPost, "/link/unlock", map[string]string{"link": link}, &data); err != nil {
		return Direct{}, err
	}
	u := data.DownloadURL
	if u == "" {
		u = data.UnlockedURL
	}
	if u == "" {
		return Direct{}, errors.New("cooldebrid: no direct link returned")
	}
	return Direct{URL: u}, nil
}

// Account reads /user for plan and expiry and /traffic for today's allowance
// in MB and links.
func (c *CoolDebrid) Account(ctx context.Context) (AccountInfo, error) {
	var user struct {
		Plan         string          `json:"plan"`
		PremiumUntil json.RawMessage `json:"premium_until"`
	}
	if err := c.do(ctx, http.MethodGet, "/user", nil, &user); err != nil {
		return AccountInfo{}, err
	}
	info := AccountInfo{Tier: strings.ToLower(strings.TrimSpace(user.Plan))}
	if secs, ok := cooldebridNumber(user.PremiumUntil); ok && secs > 0 {
		info.ExpiresAt = time.Unix(int64(secs), 0).UTC()
	}
	if info.Tier == "" {
		info.Tier = "free"
		if info.ExpiresAt.After(time.Now()) {
			info.Tier = "premium"
		}
	}

	var traffic struct {
		Daily struct {
			BWLimitMB      json.RawMessage `json:"bw_limit_mb"`
			BWRemainingMB  json.RawMessage `json:"bw_remaining_mb"`
			LinksLimit     json.RawMessage `json:"links_limit"`
			LinksRemaining json.RawMessage `json:"links_remaining"`
		} `json:"daily"`
	}
	if err := c.do(ctx, http.MethodGet, "/traffic", nil, &traffic); err != nil {
		// The plan read above is still worth showing.
		return info, nil
	}
	const mib = 1 << 20
	d := traffic.Daily
	limit, limitOK := cooldebridNumber(d.BWLimitMB)
	left, leftOK := cooldebridNumber(d.BWRemainingMB)
	switch {
	case limitOK && limit < 0:
		// -1 is how the service writes unlimited in its per-host figures.
		info.Traffic.Unlimited = true
	case limit > 0 && leftOK:
		info.Traffic.LimitBytes = int64(limit * mib)
		info.Traffic.UsedBytes = int64(max(limit-left, 0) * mib)
	}
	// Once the links run out the day is spent, whatever bytes are left.
	// JDownloader reads any count at or below zero that way.
	linksLimit, _ := cooldebridNumber(d.LinksLimit)
	if linksLeft, ok := cooldebridNumber(d.LinksRemaining); ok && linksLimit > 0 && linksLeft <= 0 {
		info.Traffic.Unlimited = false
		if info.Traffic.LimitBytes > 0 {
			info.Traffic.UsedBytes = info.Traffic.LimitBytes
		} else {
			info.Traffic.UsedPercent, info.Traffic.PercentKnown = 100, true
		}
	}
	return info, nil
}

// cooldebridNumber reads a figure sent as a JSON number or as a string. ok is
// false for a missing or null field, so an absent figure is not read as 0.
func cooldebridNumber(raw json.RawMessage) (v float64, ok bool) {
	var n *float64
	if json.Unmarshal(raw, &n) == nil {
		if n == nil {
			return 0, false
		}
		return *n, true
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

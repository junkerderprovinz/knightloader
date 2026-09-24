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

// FakirDebrid speaks the FakirDebrid API at api.fakirdebrid.net, which takes
// the account's API PIN as the last path segment of every call. There is no
// published documentation, so the endpoints, the answer shapes and the CODE
// table follow JDownloader's FakirdebridNet.java (revision 52815).
type FakirDebrid struct {
	pin  string
	base string
	hc   *http.Client
	// poll is the wait between two status calls while the service is still
	// pulling a file to its own servers.
	poll time.Duration
}

const (
	fakirdebridCallTimeout = 30 * time.Second

	// fakirdebridCallRoom is what one more status call needs before the
	// caller's deadline: a call that runs into fakirdebridCallTimeout still
	// ends first.
	fakirdebridCallRoom = fakirdebridCallTimeout + 10*time.Second
)

func NewFakirDebrid(pin string) *FakirDebrid {
	return &FakirDebrid{
		pin:  pin,
		base: "https://api.fakirdebrid.net",
		hc:   httpx.New(httpx.Options{Timeout: fakirdebridCallTimeout}),
		poll: 10 * time.Second,
	}
}

func (*FakirDebrid) ID() string    { return "fakirdebrid" }
func (*FakirDebrid) Label() string { return "FakirDebrid" }

// fakirdebridEnvelope is the part every answer shares. Its fields stay raw
// because their types are known from one client only.
type fakirdebridEnvelope struct {
	Success json.RawMessage `json:"success"`
	Status  json.RawMessage `json:"status"`
	Code    json.RawMessage `json:"code"`
	Message json.RawMessage `json:"message"`
}

func (e fakirdebridEnvelope) failed() bool {
	return fakirdebridFalse(e.Success) || strings.EqualFold(fakirdebridText(e.Status), "error")
}

// reason keeps the service's own message beside the translation, since that
// message already repeats the code.
func (e fakirdebridEnvelope) reason() string {
	code, msg := fakirdebridText(e.Code), fakirdebridText(e.Message)
	if msg == "" {
		msg = code
	}
	if msg == "" {
		return "refused without a reason"
	}
	if text := fakirdebridCodeText(code); text != "" {
		return text + " (" + msg + ")"
	}
	return msg
}

// fakirdebridCodeText translates a code into something a person can act on,
// or returns "" for a code it does not know.
func fakirdebridCodeText(code string) string {
	switch strings.ToUpper(code) {
	case "CODE1", "CODE2", "CODE3", "CODE34":
		return "the API PIN was refused; copy it again from fakirdebrid.net/api/login.php"
	case "CODE5":
		return "the account is suspended"
	case "CODE10":
		return "the account is banned"
	case "CODE35":
		return "the API needs a VIP account"
	case "CODE6", "CODE7":
		return "the account has no traffic left"
	case "CODE8":
		return "the daily download limit is used up"
	case "CODE9":
		return "the daily link limit is used up"
	case "CODE13", "CODE14":
		return "the daily limit for this hoster is used up"
	case "CODE15", "CODE16":
		return "the weekly limit for this hoster is used up"
	case "CODE11", "CODE12":
		return "this is not a valid link"
	case "CODE22", "CODE24", "CODE25", "CODE28":
		return "the file is gone or the link is wrong"
	case "CODE32", "CODE33":
		return "the file is gone from the hoster"
	case "CODE30":
		return "FakirDebrid does not support this hoster"
	case "CODE17", "CODE18", "CODE19", "CODE20", "CODE21", "CODE23", "CODE26", "CODE27", "CODE29":
		return "FakirDebrid cannot fetch this link right now"
	case "PASSWORD_REQUIRED", "WRONG_PASSWORD":
		return "the file is password-protected"
	}
	return ""
}

func (f *FakirDebrid) call(ctx context.Context, method, path string, form url.Values) ([]byte, error) {
	return f.send(ctx, method, f.base+path+"/"+url.PathEscape(f.pin), path, form)
}

// send performs one request and returns the body once the envelope reports no
// failure. Errors name the call by label, because target carries the PIN.
func (f *FakirDebrid) send(ctx context.Context, method, target, label string, form url.Values) ([]byte, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, fmt.Errorf("fakirdebrid %s: %w", label, fakirdebridBare(err))
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := f.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fakirdebrid %s: %w", label, fakirdebridBare(err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("fakirdebrid %s: %w", label, err)
	}
	// The website answers with a Cloudflare challenge page, not JSON.
	var env fakirdebridEnvelope
	if json.Unmarshal(raw, &env) != nil {
		return nil, fmt.Errorf("fakirdebrid %s: unreadable answer (%s)", label, resp.Status)
	}
	if env.failed() {
		return nil, fmt.Errorf("fakirdebrid %s: %s", label, env.reason())
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fakirdebrid %s: %s", label, resp.Status)
	}
	return raw, nil
}

// fakirdebridBare drops the request URL a *url.Error prints, because the PIN
// is part of that URL.
func fakirdebridBare(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// Hosts asks /hosts for the supported hosters. An entry without
// currently_working counts as up, so a renamed field cannot empty the routing
// table.
func (f *FakirDebrid) Hosts(ctx context.Context) (map[string]bool, error) {
	raw, err := f.call(ctx, http.MethodGet, "/hosts", nil)
	if err != nil {
		return nil, err
	}
	var list struct {
		SupportedHosts []struct {
			Host    json.RawMessage `json:"host"`
			Working json.RawMessage `json:"currently_working"`
		} `json:"supportedhosts"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, errors.New("fakirdebrid /hosts: unreadable host list")
	}
	set := map[string]bool{}
	for _, h := range list.SupportedHosts {
		if fakirdebridFalse(h.Working) {
			continue
		}
		if d := NormalizeHost(fakirdebridText(h.Host)); d != "" && strings.Contains(d, ".") {
			set[d] = true
		}
	}
	if len(set) == 0 {
		return nil, errors.New("fakirdebrid: /hosts named no working hosts")
	}
	return set, nil
}

// Unlock hands the link to /generate, which starts a transfer to the service's
// own servers, and polls that transfer until it has the file. None of the
// answers carries a file name or a size, so the engine reads both from the
// download.
func (f *FakirDebrid) Unlock(ctx context.Context, link string) (Direct, error) {
	raw, err := f.call(ctx, http.MethodPost, "/generate", url.Values{"url": {link}})
	if err != nil {
		return Direct{}, err
	}
	var gen struct {
		Data struct {
			Link string `json:"link"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw, &gen)
	if gen.Data.Link == "" {
		return Direct{}, errors.New("fakirdebrid /generate: the answer carried no transfer link")
	}
	for {
		raw, err := f.send(ctx, http.MethodGet, gen.Data.Link, "transfer status", nil)
		if err != nil {
			return Direct{}, err
		}
		var st struct {
			Data struct {
				State     string          `json:"state"`
				Completed json.RawMessage `json:"completed"`
				Link      string          `json:"link"`
			} `json:"data"`
		}
		_ = json.Unmarshal(raw, &st)
		switch strings.ToLower(st.Data.State) {
		case "completed":
			if st.Data.Link == "" {
				return Direct{}, errors.New("fakirdebrid: the transfer finished without a download link")
			}
			return Direct{URL: st.Data.Link}, nil
		case "processing":
		default:
			return Direct{}, fmt.Errorf("fakirdebrid: unknown transfer state %q", st.Data.State)
		}
		wait := "fakirdebrid: the file is still on its way to FakirDebrid"
		if p := fakirdebridText(st.Data.Completed); p != "" {
			wait += " (" + p + "%)"
		}
		// Giving up while a wait and one whole status call still fit reports how
		// far the transfer got, where a call cut off by the deadline would only
		// say that time ran out. The task's retry asks /generate again.
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < f.poll+fakirdebridCallRoom {
			return Direct{}, errors.New(wait)
		}
		select {
		case <-ctx.Done():
			return Direct{}, fmt.Errorf("%s: %w", wait, ctx.Err())
		case <-time.After(f.poll):
		}
	}
}

// Account reads /account. The expiry is measured against the service's own
// server_time and then laid onto the local clock, so a skewed local clock
// does not shift it.
func (f *FakirDebrid) Account(ctx context.Context) (AccountInfo, error) {
	raw, err := f.call(ctx, http.MethodGet, "/account", nil)
	if err != nil {
		return AccountInfo{}, err
	}
	var body struct {
		ServerTime json.RawMessage `json:"server_time"`
		Account    *struct {
			Plan         json.RawMessage `json:"plan"`
			PremiumUntil json.RawMessage `json:"premium_until"`
			Traffic      struct {
				Left  json.RawMessage `json:"left"`
				Limit json.RawMessage `json:"limit"`
			} `json:"traffic"`
		} `json:"account"`
	}
	// Without the account object every field below reads as zero, which would
	// report a paid account as free and replace the last good reading.
	if err := json.Unmarshal(raw, &body); err != nil || body.Account == nil {
		return AccountInfo{}, errors.New("fakirdebrid /account: unreadable account details")
	}
	acct := body.Account
	info := AccountInfo{Tier: "free"}
	if until := looseInt(acct.PremiumUntil); until > 0 {
		expires := time.Unix(until, 0)
		if server := looseInt(body.ServerTime); server > 0 {
			expires = time.Now().Add(time.Duration(until-server) * time.Second)
		}
		info.ExpiresAt = expires.UTC()
		if expires.After(time.Now()) {
			// The page translates "premium" and prints any other plan name
			// as the service wrote it.
			info.Tier = fakirdebridText(acct.Plan)
			if info.Tier == "" || strings.EqualFold(info.Tier, "premium") {
				info.Tier = "premium"
			}
		}
	}
	// Both figures are bytes, and the allowance does not refill.
	if left, limit := looseInt(acct.Traffic.Left), looseInt(acct.Traffic.Limit); limit > 0 {
		info.Traffic = TrafficInfo{UsedBytes: max(limit-left, 0), LimitBytes: limit}
	}
	return info, nil
}

// fakirdebridText reads a JSON string or number as text, and returns "" for
// anything else.
func fakirdebridText(raw json.RawMessage) string {
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

// fakirdebridFalse reports whether raw says false, written as a bool, a number
// or a string. A missing field is not false.
func fakirdebridFalse(raw json.RawMessage) bool {
	switch strings.ToLower(strings.Trim(string(raw), `" `)) {
	case "false", "0":
		return true
	}
	return false
}

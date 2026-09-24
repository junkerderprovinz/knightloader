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
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// NeoDebrid speaks the undocumented NeoDebrid API, whose endpoints, fields and
// error texts follow JDownloader's NeodebridCom.java (svn revision 50303).
type NeoDebrid struct {
	email string
	pass  string
	base  string
	hc    *http.Client

	// mu guards token, which unlocks on parallel download goroutines share.
	mu    loginLock
	token string

	// capMu guards chunkCaps, the smallest "chunks" seen per hoster. It is not
	// mu because mu is held across a login, and dispatch reads the caps.
	capMu     sync.Mutex
	chunkCaps map[string]int
}

func NewNeoDebrid(email, pass string) *NeoDebrid {
	return &NeoDebrid{
		email:     email,
		pass:      pass,
		base:      "https://neodebrid.com/api/v2",
		hc:        httpx.New(httpx.Options{Timeout: 30 * time.Second}),
		mu:        newLoginLock(),
		chunkCaps: map[string]int{},
	}
}

func (*NeoDebrid) ID() string    { return "neodebrid" }
func (*NeoDebrid) Label() string { return "NeoDebrid" }

// neodebridEnvelope is the part every answer shares. /status sends no status
// at all, so an empty one counts as success, as it does in JD.
type neodebridEnvelope struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// neodebridError is a refusal from the service, kept typed so authed can spot
// a dead session token and log in again.
type neodebridError struct {
	path   string
	reason string
	status int
}

func (e *neodebridError) Error() string {
	hint := e.hint()
	switch {
	case hint != "" && e.reason != "":
		return fmt.Sprintf("neodebrid %s: %s (%s)", e.path, hint, e.reason)
	case hint != "":
		return fmt.Sprintf("neodebrid %s: %s", e.path, hint)
	case e.reason != "":
		return fmt.Sprintf("neodebrid %s: %s", e.path, e.reason)
	}
	return fmt.Sprintf("neodebrid %s: refused without a reason", e.path)
}

// hint explains the refusals JD recognises, matched the way JD matches them:
// by how the reason starts, ignoring case. Any other reason, a dead file among
// them, reaches the user in the service's own words.
func (e *neodebridError) hint() string {
	if e.status == http.StatusPaymentRequired {
		return "the account has no traffic left"
	}
	r := strings.ToLower(e.reason)
	switch {
	case strings.HasPrefix(r, "wrong credentials"):
		return "the email address or password was refused"
	case strings.HasPrefix(r, "ip blocked"):
		return "NeoDebrid has blocked this server's IP address"
	case strings.HasPrefix(r, "filehost not supported"):
		return "this file hoster is not supported"
	case strings.HasPrefix(r, "user not premium"):
		return "this file hoster needs a premium account"
	case e.tokenGone():
		return "the session was refused again right after a fresh login"
	}
	return ""
}

// tokenGone reports whether the session token has run out. JD quotes "Token
// not found." and "Session expired. Please log-in again."; any other mention
// of the token is read the same way, since the cost is one extra login.
func (e *neodebridError) tokenGone() bool {
	r := strings.ToLower(e.reason)
	return strings.Contains(r, "token") || strings.HasPrefix(r, "session expired")
}

// call performs a GET and unwraps the envelope. The body decides, as it does
// in JD; the one status line read is 402, which JD takes as spent traffic.
func (n *NeoDebrid) call(ctx context.Context, path string, q url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, n.base+path, nil)
	if err != nil {
		return err
	}
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Accept", "application/json")
	resp, err := n.hc.Do(req)
	if err != nil {
		// url.Error quotes the request URL, which carries the token or, on
		// /login, the password.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("neodebrid %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("neodebrid %s: %w", path, err)
	}
	var env neodebridEnvelope
	readable := json.Unmarshal(raw, &env) == nil
	refused := env.Status != "" && !strings.EqualFold(env.Status, "success")
	if refused || resp.StatusCode == http.StatusPaymentRequired {
		return &neodebridError{path: path, reason: strings.TrimSpace(env.Reason), status: resp.StatusCode}
	}
	if !readable {
		return fmt.Errorf("neodebrid %s: unreadable answer (%s)", path, resp.Status)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("neodebrid %s: %s", path, resp.Status)
	}
	if out != nil && json.Unmarshal(raw, out) != nil {
		return fmt.Errorf("neodebrid %s: unreadable answer", path)
	}
	return nil
}

func (n *NeoDebrid) login(ctx context.Context) (string, error) {
	if n.email == "" || n.pass == "" {
		return "", errors.New("neodebrid: an email address and a password are required")
	}
	var out struct {
		APIToken string `json:"api_token"`
	}
	if err := n.call(ctx, "/login", url.Values{"email": {n.email}, "password": {n.pass}}, &out); err != nil {
		return "", err
	}
	if out.APIToken == "" {
		return "", errors.New("neodebrid /login: accepted without handing out a token")
	}
	return out.APIToken, nil
}

// Authenticate logs in afresh and keeps the session token. It is exported to
// verify the credential, since the host list needs no account.
func (n *NeoDebrid) Authenticate(ctx context.Context) error {
	if err := n.mu.lock(ctx); err != nil {
		return err
	}
	defer n.mu.unlock()
	tok, err := n.login(ctx)
	if err != nil {
		return err
	}
	n.token = tok
	return nil
}

// session returns the token to use, logging in when there is none or when the
// cached one is stale, the token a call just had refused. The lock is held
// across the login, so parallel unlocks wait for one session instead of each
// opening their own, and one that lost the race gets the winner's token.
func (n *NeoDebrid) session(ctx context.Context, stale string) (string, error) {
	if err := n.mu.lock(ctx); err != nil {
		return "", err
	}
	defer n.mu.unlock()
	if n.token != "" && n.token != stale {
		return n.token, nil
	}
	n.token = ""
	tok, err := n.login(ctx)
	if err != nil {
		return "", err
	}
	n.token = tok
	return tok, nil
}

// authed performs a call that needs the session token, and logs in again once
// when the service says the token has run out.
func (n *NeoDebrid) authed(ctx context.Context, path string, q url.Values, out any) error {
	tok, err := n.session(ctx, "")
	if err != nil {
		return err
	}
	q.Set("token", tok)
	err = n.call(ctx, path, q, out)
	var ne *neodebridError
	if !errors.As(err, &ne) || !ne.tokenGone() {
		return err
	}
	if tok, err = n.session(ctx, tok); err != nil {
		return err
	}
	q.Set("token", tok)
	return n.call(ctx, path, q, out)
}

// Hosts reads /status, which needs no account and names no alias domains. An
// entry whose status is not "online" is left out; one without a status counts
// as up, so a dropped field cannot empty the routing table. Names arrive
// capitalised and are lower-cased before NormalizeHost strips "www.".
func (n *NeoDebrid) Hosts(ctx context.Context) (map[string]bool, error) {
	var out struct {
		Result []struct {
			Host   string `json:"host"`
			Status string `json:"status"`
		} `json:"result"`
	}
	if err := n.call(ctx, "/status", nil, &out); err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, h := range out.Result {
		if s := strings.TrimSpace(h.Status); s != "" && !strings.EqualFold(s, "online") {
			continue
		}
		if d := NormalizeHost(strings.ToLower(h.Host)); strings.Contains(d, ".") {
			set[d] = true
		}
	}
	if len(set) == 0 {
		return nil, errors.New("neodebrid: /status named no hosts")
	}
	return set, nil
}

// Unlock asks /download for the direct link. JD reads neither a file name nor
// a size from the answer, so the engine takes both from the download itself.
func (n *NeoDebrid) Unlock(ctx context.Context, link string) (Direct, error) {
	var out struct {
		Download string `json:"download"`
		// Chunks is the most connections the link allows. JD reads it with
		// either JSON type, so both are accepted here too.
		Chunks json.RawMessage `json:"chunks"`
	}
	if err := n.authed(ctx, "/download", url.Values{"link": {link}}, &out); err != nil {
		return Direct{}, err
	}
	if out.Download == "" {
		return Direct{}, errors.New("neodebrid: no direct link returned")
	}
	n.rememberChunkCap(link, int(looseInt(out.Chunks)))
	return Direct{URL: out.Download}, nil
}

// rememberChunkCap keeps the smallest "chunks" reported for link's hoster, the
// ceiling JD opens a download with. A missing or zero figure is skipped, or it
// would wipe a cap learned from an earlier link.
func (n *NeoDebrid) rememberChunkCap(link string, chunks int) {
	u, err := url.Parse(link)
	if chunks <= 0 || err != nil || u.Hostname() == "" {
		return
	}
	host := NormalizeHost(u.Hostname())
	n.capMu.Lock()
	defer n.capMu.Unlock()
	if cur, ok := n.chunkCaps[host]; !ok || chunks < cur {
		n.chunkCaps[host] = chunks
	}
}

// HostLimit satisfies HostLimiter. It is 0, no opinion, until an unlock has
// reported chunks for host.
func (n *NeoDebrid) HostLimit(host string) int {
	n.capMu.Lock()
	defer n.capMu.Unlock()
	return n.chunkCaps[NormalizeHost(host)]
}

// Account reads /info, which has no plan field: the account is premium while
// timestamp, the expiry in Unix seconds, lies ahead. A sized traffic_left is a
// remainder with no ceiling to set it against, and JD found the free figure
// wrong, so only "Unlimited" is taken from it.
func (n *NeoDebrid) Account(ctx context.Context) (AccountInfo, error) {
	var out struct {
		Timestamp   json.RawMessage `json:"timestamp"`
		TrafficLeft json.RawMessage `json:"traffic_left"`
	}
	if err := n.authed(ctx, "/info", url.Values{}, &out); err != nil {
		return AccountInfo{}, err
	}
	info := AccountInfo{Tier: "free"}
	if secs := looseInt(out.Timestamp); secs > 0 {
		info.ExpiresAt = time.Unix(secs, 0).UTC()
		if info.ExpiresAt.After(time.Now()) {
			info.Tier = "premium"
		}
	}
	var left string
	_ = json.Unmarshal(out.TrafficLeft, &left)
	info.Traffic.Unlimited = strings.EqualFold(strings.TrimSpace(left), "unlimited")
	return info, nil
}

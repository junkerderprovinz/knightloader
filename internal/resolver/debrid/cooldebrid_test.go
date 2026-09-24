package debrid

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// CoolDebrid publishes no API reference. The host list and the error envelope
// below are shaped like the live service's answers; the token calls use the
// fields JDownloader's plugin reads.

const cooldebridTestToken = "0123456789abcdef0123456789abcdef01234567"

type cooldebridReply struct {
	status int
	body   string
}

func cooldebridAt(base, token string) *CoolDebrid {
	c := NewCoolDebrid(token)
	c.base = base
	return c
}

// cooldebridServe answers each path with its canned reply and fails the test on
// an unknown path or a call without the token.
func cooldebridServe(t *testing.T, routes map[string]cooldebridReply) *CoolDebrid {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reply, ok := routes[r.URL.Path]
		if !ok {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+cooldebridTestToken {
			t.Errorf("%s carried Authorization %q, want the token as bearer", r.URL.Path, got)
		}
		w.Header().Set("Content-Type", "application/json")
		if reply.status != 0 {
			w.WriteHeader(reply.status)
		}
		_, _ = io.WriteString(w, reply.body)
	}))
	t.Cleanup(srv.Close)
	return cooldebridAt(srv.URL, cooldebridTestToken)
}

func TestCoolDebridHostsDropsOfflineButKeepsDegraded(t *testing.T) {
	c := cooldebridServe(t, map[string]cooldebridReply{
		"/hosts": {body: `{"status":"success","data":{"hosts":[
			{"host":"1fichier.com","status":"online","premium_only":true},
			{"host":"envato.com","status":"degraded","premium_only":true},
			{"host":"daofile.com","status":"offline","premium_only":true},
			{"host":"www.Filer.NET","status":"online","premium_only":true}],"total":4}}`},
	})
	hosts, err := c.Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	for _, want := range []string{"1fichier.com", "envato.com", "filer.net"} {
		if !hosts[want] {
			t.Errorf("Hosts is missing %q: %v", want, hosts)
		}
	}
	if hosts["daofile.com"] {
		t.Error("a host the service reports as offline was taken as up")
	}
	// The list carries no alias domains, so nothing beyond the named hosts
	// may appear.
	if len(hosts) != 3 {
		t.Errorf("Hosts = %v, want exactly the three hosts that are up", hosts)
	}
}

func TestCoolDebridHostsWithNothingUpIsAnError(t *testing.T) {
	c := cooldebridServe(t, map[string]cooldebridReply{
		"/hosts": {body: `{"status":"success","data":{"hosts":[
			{"host":"daofile.com","status":"offline"}],"total":1}}`},
	})
	if hosts, err := c.Hosts(context.Background()); err == nil {
		t.Fatalf("Hosts = %v, want an error rather than an empty routing table", hosts)
	}
}

func TestCoolDebridUnlockPostsTheLinkAsJSON(t *testing.T) {
	const link = "https://rapidgator.net/file/abc?x=1&y=2"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/link/unlock" {
			t.Errorf("request = %s %s, want POST /link/unlock", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+cooldebridTestToken {
			t.Errorf("Authorization = %q, want the token as bearer", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q, want the token kept out of the URL", r.URL.RawQuery)
		}
		var body struct {
			Link string `json:"link"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Link != link {
			t.Errorf("body link = %q (%v), want %q", body.Link, err, link)
		}
		_, _ = io.WriteString(w, `{"status":"success","data":{"download_url":"https://dl.cooldebrid.example/d/abc/File.ext"}}`)
	}))
	defer srv.Close()

	got, err := cooldebridAt(srv.URL, cooldebridTestToken).Unlock(context.Background(), link)
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.URL != "https://dl.cooldebrid.example/d/abc/File.ext" {
		t.Errorf("Unlock = %+v, want download_url", got)
	}
}

func TestCoolDebridUnlockReadsTheFirstVersionFieldName(t *testing.T) {
	c := cooldebridServe(t, map[string]cooldebridReply{
		"/link/unlock": {body: `{"status":"success","data":{"unlocked_url":"https://dl.cooldebrid.example/d/old"}}`},
	})
	got, err := c.Unlock(context.Background(), "https://rapidgator.net/file/abc")
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.URL != "https://dl.cooldebrid.example/d/old" {
		t.Errorf("Unlock = %+v, want unlocked_url", got)
	}
}

func TestCoolDebridUnlockWithoutALinkIsAnError(t *testing.T) {
	c := cooldebridServe(t, map[string]cooldebridReply{
		"/link/unlock": {body: `{"status":"success","data":{}}`},
	})
	if got, err := c.Unlock(context.Background(), "https://rapidgator.net/file/abc"); err == nil {
		t.Fatalf("Unlock = %+v, want an error for a success without a link", got)
	}
}

func TestCoolDebridUnlockRefusalsReadDifferently(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		code    string
		message string
		want    string
	}{
		{"refused token", http.StatusUnauthorized, "AUTH_BAD_TOKEN", "Invalid API token", "API token was refused"},
		{"unsupported host", http.StatusBadRequest, "HOST_NOT_SUPPORTED", "Host not supported", "does not support this file hoster"},
		{"hoster offline", http.StatusBadRequest, "HOST_OFFLINE", "Host is offline", "offline at CoolDebrid"},
		{"daily links", http.StatusBadRequest, "DAILY_LINK_LIMIT", "Daily link limit reached", "daily link limit"},
		{"daily traffic", http.StatusBadRequest, "DAILY_BW_LIMIT", "Daily bandwidth limit reached", "daily traffic limit"},
		{"hoster quota", http.StatusBadRequest, "HOST_DAILY_LIMIT", "Host daily limit reached", "allowance for this file hoster"},
		{"rate limit", http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED", "Slow down", "too many requests"},
		// No code for a dead file is known, so a code outside the table has to
		// carry the service's own words.
		{"code outside the table", http.StatusBadRequest, "UNLISTED", "File not found", "File not found (UNLISTED)"},
	}
	seen := map[string]string{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := cooldebridServe(t, map[string]cooldebridReply{
				"/link/unlock": {status: tc.status, body: fmt.Sprintf(
					`{"status":"error","error":{"code":%q,"message":%q}}`, tc.code, tc.message)},
			})
			_, err := c.Unlock(context.Background(), "https://rapidgator.net/file/abc")
			if err == nil {
				t.Fatal("Unlock succeeded against an error answer")
			}
			msg := err.Error()
			if !strings.HasPrefix(msg, "cooldebrid") {
				t.Errorf("error = %q, want it to name the service", msg)
			}
			if !strings.Contains(msg, tc.want) || !strings.Contains(msg, tc.message) {
				t.Errorf("error = %q, want %q and the service's message %q", msg, tc.want, tc.message)
			}
			if other, dup := seen[msg]; dup {
				t.Errorf("%s reads the same as %s: %q", tc.name, other, msg)
			}
			seen[msg] = tc.name
		})
	}
}

func TestCoolDebridUnreadableAnswerNamesTheHTTPStatus(t *testing.T) {
	c := cooldebridServe(t, map[string]cooldebridReply{
		"/link/unlock": {status: http.StatusBadGateway, body: `<html>Bad gateway</html>`},
	})
	_, err := c.Unlock(context.Background(), "https://rapidgator.net/file/abc")
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Errorf("error = %v, want one naming HTTP 502", err)
	}
}

func TestCoolDebridAuthenticateAcceptsAWorkingToken(t *testing.T) {
	c := cooldebridServe(t, map[string]cooldebridReply{
		"/user": {body: `{"status":"success","data":{"email":"amy@example.com","premium_until":1893456000,"plan":"Premium"}}`},
	})
	if err := c.Authenticate(context.Background()); err != nil {
		t.Errorf("Authenticate: %v", err)
	}
}

// The refusal quotes the token back, which the error must not pass on.
func TestCoolDebridAuthenticateRejectsARevokedToken(t *testing.T) {
	c := cooldebridServe(t, map[string]cooldebridReply{
		"/user": {status: http.StatusUnauthorized, body: fmt.Sprintf(
			`{"status":"error","error":{"code":"AUTH_TOKEN_REVOKED","message":"Token %s has been revoked"}}`, cooldebridTestToken)},
	})
	err := c.Authenticate(context.Background())
	if err == nil {
		t.Fatal("Authenticate accepted a revoked token")
	}
	msg := err.Error()
	if !strings.Contains(msg, "API token was refused") || !strings.Contains(msg, "has been revoked") {
		t.Errorf("error = %q, want the refusal and the service's message", msg)
	}
	if strings.Contains(msg, cooldebridTestToken) {
		t.Errorf("error = %q carries the token", msg)
	}
}

func TestCoolDebridSendsAPastedTokenWithoutItsWhitespace(t *testing.T) {
	c := cooldebridServe(t, map[string]cooldebridReply{
		"/user": {body: `{"status":"success","data":{"premium_until":1893456000,"plan":"Premium"}}`},
	})
	pasted := cooldebridAt(c.base, " "+cooldebridTestToken+"\r\n")
	if err := pasted.Authenticate(context.Background()); err != nil {
		t.Errorf("Authenticate: %v", err)
	}
}

func TestCoolDebridAuthenticateWithoutATokenAsksNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request without a token: %s", r.URL.Path)
	}))
	defer srv.Close()

	if err := cooldebridAt(srv.URL, "").Authenticate(context.Background()); err == nil {
		t.Fatal("Authenticate succeeded without a token")
	}
}

func TestCoolDebridAccountReadsPlanExpiryAndTraffic(t *testing.T) {
	future := time.Now().Add(30 * 24 * time.Hour).Unix()
	c := cooldebridServe(t, map[string]cooldebridReply{
		"/user": {body: fmt.Sprintf(`{"status":"success","data":{"email":"amy@example.com","premium_until":%d,"plan":"Premium"}}`, future)},
		// bw_remaining_mb as a string checks the loose decoding.
		"/traffic": {body: `{"status":"success","data":{"daily":{
			"bw_limit_mb":51200,"bw_remaining_mb":"40960","links_limit":100,"links_remaining":90}}}`},
	})
	info, err := c.Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "premium" {
		t.Errorf("Tier = %q, want premium", info.Tier)
	}
	if info.ExpiresAt.Unix() != future {
		t.Errorf("ExpiresAt = %v, want Unix %d", info.ExpiresAt, future)
	}
	tr := info.Traffic
	if tr.Unlimited || tr.LimitBytes != 51200<<20 || tr.UsedBytes != 10240<<20 {
		t.Errorf("Traffic = %+v, want 10240 of 51200 MiB used", tr)
	}
}

func TestCoolDebridAccountReadsMinusOneAsUnlimited(t *testing.T) {
	c := cooldebridServe(t, map[string]cooldebridReply{
		"/user": {body: `{"status":"success","data":{"premium_until":1893456000,"plan":"Premium"}}`},
		"/traffic": {body: `{"status":"success","data":{"daily":{
			"bw_limit_mb":-1,"bw_remaining_mb":-1,"links_limit":-1,"links_remaining":-1}}}`},
	})
	info, err := c.Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if !info.Traffic.Unlimited {
		t.Errorf("Traffic = %+v, want Unlimited", info.Traffic)
	}
}

func TestCoolDebridAccountWithNoLinksLeftIsSpent(t *testing.T) {
	for _, left := range []int{0, -2} {
		t.Run(fmt.Sprint(left), func(t *testing.T) {
			c := cooldebridServe(t, map[string]cooldebridReply{
				"/user": {body: `{"status":"success","data":{"premium_until":1893456000,"plan":"Premium"}}`},
				"/traffic": {body: fmt.Sprintf(`{"status":"success","data":{"daily":{
					"bw_limit_mb":51200,"bw_remaining_mb":40960,"links_limit":100,"links_remaining":%d}}}`, left)},
			})
			info, err := c.Account(context.Background())
			if err != nil {
				t.Fatalf("Account: %v", err)
			}
			if tr := info.Traffic; tr.LimitBytes == 0 || tr.UsedBytes != tr.LimitBytes {
				t.Errorf("Traffic = %+v, want the whole allowance shown as used", tr)
			}
		})
	}
}

func TestCoolDebridAccountWithoutAPlanFallsBackOnTheExpiry(t *testing.T) {
	future := time.Now().Add(10 * 24 * time.Hour).Unix()
	c := cooldebridServe(t, map[string]cooldebridReply{
		"/user":    {body: fmt.Sprintf(`{"status":"success","data":{"premium_until":"%d"}}`, future)},
		"/traffic": {body: `{"status":"success","data":{"daily":{}}}`},
	})
	info, err := c.Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "premium" || info.ExpiresAt.Unix() != future {
		t.Errorf("Account = %+v, want premium until Unix %d read from a string", info, future)
	}
	if info.Traffic != (TrafficInfo{}) {
		t.Errorf("Traffic = %+v, want it left empty when the figures are missing", info.Traffic)
	}
}

func TestCoolDebridAccountSurvivesATrafficFailure(t *testing.T) {
	c := cooldebridServe(t, map[string]cooldebridReply{
		"/user":    {body: `{"status":"success","data":{"premium_until":1893456000,"plan":"Premium"}}`},
		"/traffic": {status: http.StatusInternalServerError, body: `{"status":"error"}`},
	})
	info, err := c.Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "premium" {
		t.Errorf("Tier = %q, want premium despite the traffic call failing", info.Tier)
	}
}

func TestCoolDebridAccountReportsANonPremiumAccount(t *testing.T) {
	c := cooldebridServe(t, map[string]cooldebridReply{
		"/user": {status: http.StatusForbidden, body: `{"status":"error","error":{"code":"AUTH_NOT_PREMIUM","message":"Premium required"}}`},
	})
	_, err := c.Account(context.Background())
	if err == nil || !strings.Contains(err.Error(), "premium account") {
		t.Errorf("error = %v, want it to say the API needs a premium account", err)
	}
}

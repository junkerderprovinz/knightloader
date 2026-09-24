package debrid

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The bodies follow https://bestdebrid.com/en/restapi.php, plus the keyed
// host list JDownloader's plugin also reads.

const bestdebridTestKey = "bd-test-key"

func bestdebridAt(base string) *BestDebrid {
	b := NewBestDebrid(bestdebridTestKey)
	b.base = base
	return b
}

// bestdebridServe answers each path with its body and fails the test when a
// request does not carry the key in the Authorization header alone.
func bestdebridServe(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != bestdebridTestKey {
			t.Errorf("%s carried Authorization %q, want the bare key", r.URL.Path, got)
		}
		if strings.Contains(r.URL.RawQuery, bestdebridTestKey) {
			t.Errorf("%s put the key in the query string", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestBestDebridHostsSkipsDownHostsAndAddsAliases(t *testing.T) {
	srv := bestdebridServe(t, map[string]string{
		"/hosts": `[
			{"name":"rapidgator.net","status":"up","picture":"rg.png","lastcheck":"2026-09-24 10:00:00",
			 "downsincedate":"","hostfolder":false,"domains":["rapidgator.net","rg.to","www.RG.to"]},
			{"name":"deadhost.example","status":"down","downsincedate":"2026-09-20 08:00:00",
			 "domains":["deadhost.example","dead-alias.example"]},
			{"name":"1fichier.com","status":"up"},
			{"name":"filestore","status":"up"}]`,
	})
	hosts, err := bestdebridAt(srv.URL).Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	for _, want := range []string{"rapidgator.net", "rg.to", "1fichier.com"} {
		if !hosts[want] {
			t.Errorf("Hosts is missing %q: %v", want, hosts)
		}
	}
	for _, gone := range []string{"deadhost.example", "dead-alias.example"} {
		if hosts[gone] {
			t.Errorf("%q belongs to a host marked down", gone)
		}
	}
	if hosts["filestore"] {
		t.Error("a name without a dot was taken as a domain")
	}
}

func TestBestDebridHostsReadsTheListKeyedByIndex(t *testing.T) {
	srv := bestdebridServe(t, map[string]string{
		"/hosts": `{"0":{"name":"rapidgator.net","status":"up","domains":["rapidgator.net"]},
			"1":{"name":"k2s.cc","status":"down","domains":["k2s.cc","keep2share.cc"]}}`,
	})
	hosts, err := bestdebridAt(srv.URL).Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	if !hosts["rapidgator.net"] || hosts["k2s.cc"] {
		t.Errorf("Hosts = %v, want rapidgator.net only", hosts)
	}
}

func TestBestDebridHostsAddsTheAliasesTheAPIRewrites(t *testing.T) {
	srv := bestdebridServe(t, map[string]string{
		"/hosts": `[{"name":"rapidgator.net","status":"up"},{"name":"k2s.cc","status":"down"},
			{"name":"filestore","status":"up"}]`,
	})
	hosts, err := bestdebridAt(srv.URL).Hosts(context.Background())
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	for _, want := range []string{"rapidgator.net", "rg.to", "filestore.me"} {
		if !hosts[want] {
			t.Errorf("Hosts is missing %q: %v", want, hosts)
		}
	}
	for _, gone := range []string{"k2s.cc", "keep2share.cc", "filestore"} {
		if hosts[gone] {
			t.Errorf("Hosts has %q: %v", gone, hosts)
		}
	}
}

func TestBestDebridRefusedKeyFailsHostsAndAccount(t *testing.T) {
	srv := bestdebridServe(t, map[string]string{
		"/hosts": `{"error":1,"message":"Invalid API key"}`,
		"/user":  `{"error":1,"message":"Invalid API key"}`,
	})
	b := bestdebridAt(srv.URL)
	_, hostsErr := b.Hosts(context.Background())
	_, accountErr := b.Account(context.Background())
	for name, err := range map[string]error{"Hosts": hostsErr, "Account": accountErr} {
		if err == nil {
			t.Errorf("%s succeeded with a refused key", name)
			continue
		}
		if !strings.Contains(err.Error(), "Invalid API key") {
			t.Errorf("%s error = %q, want BestDebrid's own message", name, err)
		}
		if strings.Contains(err.Error(), bestdebridTestKey) {
			t.Errorf("%s error carries the key: %q", name, err)
		}
	}
}

func TestBestDebridUnlockPostsTheLinkAndReadsTheDirectURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/generateLink" {
			t.Errorf("request = %s %s, want POST /generateLink", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != bestdebridTestKey {
			t.Errorf("Authorization = %q, want the bare key", got)
		}
		if got := r.PostFormValue("link"); got != "https://rapidgator.net/file/x" {
			t.Errorf("link = %q, want the hoster link", got)
		}
		_, _ = io.WriteString(w, `{"error":0,"message":"OK","original_link":"https://rapidgator.net/file/x",
			"servId":"3","hoster":"rapidgator.net","hoster-icon":"https://bestdebrid.com/img/rg.png",
			"filename":"File.ext","link":"https://dl.bestdebrid.com/abc/File.ext","size":"1.35 GiB"}`)
	}))
	defer srv.Close()

	got, err := bestdebridAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.URL != "https://dl.bestdebrid.com/abc/File.ext" || got.Name != "File.ext" {
		t.Errorf("Unlock = %+v, want the link and filename from the answer", got)
	}
	if got.Size != 0 {
		t.Errorf("Size = %d, want 0 for a size rounded to a unit", got.Size)
	}
}

func TestBestDebridUnlockKeepsAnExactByteCount(t *testing.T) {
	for _, size := range []string{`125002`, `"125002"`} {
		srv := bestdebridServe(t, map[string]string{
			"/generateLink": `{"error":0,"message":"OK","filename":"File.ext",
				"link":"https://dl.bestdebrid.com/abc/File.ext","size":` + size + `}`,
		})
		got, err := bestdebridAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x")
		if err != nil {
			t.Fatalf("Unlock with size %s: %v", size, err)
		}
		if got.Size != 125002 {
			t.Errorf("Size = %d for size %s, want 125002", got.Size, size)
		}
	}
}

func TestBestDebridUnlockErrorsTellTheirCauseApart(t *testing.T) {
	cases := []struct {
		kind string
		body string
		msg  string
		want string
	}{
		{"dead link", `{"error":10,"message":"Link Dead"}`, "Link Dead", "dead"},
		{"unsupported host", `{"error":3,"message":"Unsupported link"}`, "Unsupported link", "does not support"},
		{"hoster quota", `{"error":9,"message":"Hoster limit reached"}`, "Hoster limit reached", "limit for this hoster"},
		{"account quota", `{"error":"121","message":"Daily quota reached"}`, "Daily quota reached", "limit of files"},
		{"unmapped code", `{"error":56,"message":"Please wait 30 seconds"}`, "Please wait 30 seconds", "bestdebrid /generateLink"},
	}
	seen := map[string]string{}
	for _, c := range cases {
		srv := bestdebridServe(t, map[string]string{"/generateLink": c.body})
		_, err := bestdebridAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x")
		if err == nil {
			t.Errorf("%s: Unlock succeeded against an error answer", c.kind)
			continue
		}
		text := err.Error()
		if !strings.Contains(text, "bestdebrid") || !strings.Contains(text, c.msg) || !strings.Contains(text, c.want) {
			t.Errorf("%s: error = %q, want the service name, %q and %q", c.kind, text, c.msg, c.want)
		}
		if strings.Contains(text, bestdebridTestKey) {
			t.Errorf("%s: error carries the key: %q", c.kind, text)
		}
		seen[strings.TrimSuffix(text, "("+c.msg+")")] = c.kind
	}
	if len(seen) != len(cases) {
		t.Errorf("two kinds of failure read alike: %v", seen)
	}
}

func TestBestDebridUnlockWithoutADirectLinkFails(t *testing.T) {
	srv := bestdebridServe(t, map[string]string{"/generateLink": `{"error":0,"message":"OK"}`})
	if _, err := bestdebridAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x"); err == nil {
		t.Fatal("Unlock reported success without a link in the answer")
	}
}

// BestDebrid answers some refusals with error 0, so the message is the only
// reason given.
func TestBestDebridUnlockKeepsTheReasonOfARefusalWithErrorZero(t *testing.T) {
	srv := bestdebridServe(t, map[string]string{"/generateLink": `{"error":0,"message":"Invalid credentials"}`})
	_, err := bestdebridAt(srv.URL).Unlock(context.Background(), "https://rapidgator.net/file/x")
	if err == nil || !strings.Contains(err.Error(), "Invalid credentials") {
		t.Errorf("error = %v, want one that keeps BestDebrid's message", err)
	}
}

func TestBestDebridAnswerThatIsNotJSONNamesTheStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "<html>Bad Gateway</html>")
	}))
	defer srv.Close()

	_, err := bestdebridAt(srv.URL).Hosts(context.Background())
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Errorf("error = %v, want one that names the 502", err)
	}
}

func TestBestDebridFailedStatusWithoutAnErrorCodeIsNotAFreeAccount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"message":"Too many requests"}`)
	}))
	defer srv.Close()

	info, err := bestdebridAt(srv.URL).Account(context.Background())
	if err == nil {
		t.Fatalf("Account = %+v from a 429, want an error", info)
	}
	if !strings.Contains(err.Error(), "Too many requests") || !strings.Contains(err.Error(), "429") {
		t.Errorf("error = %q, want the message and the status", err)
	}
}

func TestBestDebridAccountReadsPremiumAndExpiry(t *testing.T) {
	srv := bestdebridServe(t, map[string]string{
		"/user": `{"error":0,"username":"amy","email":"amy@example.com","credit":"0.00","ID":"42",
			"premium":true,"expire":"2030-01-02 03:04:05","bypass_api_limit":false}`,
	})
	info, err := bestdebridAt(srv.URL).Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "premium" || !info.Traffic.Unlimited {
		t.Errorf("Account = %+v, want premium with unlimited traffic", info)
	}
	if want := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC); !info.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", info.ExpiresAt, want)
	}
}

func TestBestDebridAccountWithoutPremiumIsFree(t *testing.T) {
	srv := bestdebridServe(t, map[string]string{
		"/user": `{"error":0,"username":"amy","premium":false,"expire":null,"bypass_api_limit":false}`,
	})
	info, err := bestdebridAt(srv.URL).Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if info.Tier != "free" || info.Traffic.Unlimited {
		t.Errorf("Account = %+v, want free without traffic", info)
	}
	if !info.ExpiresAt.IsZero() {
		t.Errorf("ExpiresAt = %v, want the zero time for an account with nothing to expire", info.ExpiresAt)
	}
}

func TestBestDebridAccountReadsAFutureExpiryAsPremium(t *testing.T) {
	cases := []struct {
		expire string
		want   string
	}{
		{"2030-01-02 03:04:05", "premium"},
		{"2020-01-02 03:04:05", "free"},
	}
	for _, c := range cases {
		srv := bestdebridServe(t, map[string]string{
			"/user": `{"error":0,"premium":false,"expire":"` + c.expire + `","bypass_api_limit":false}`,
		})
		info, err := bestdebridAt(srv.URL).Account(context.Background())
		if err != nil {
			t.Fatalf("Account with expire %s: %v", c.expire, err)
		}
		if info.Tier != c.want {
			t.Errorf("Tier = %q with expire %s, want %q", info.Tier, c.expire, c.want)
		}
	}
}

func TestBestDebridAccountReadsFlagsSentAsNumbersOrStrings(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		{`{"error":"0","premium":"1","expire":null,"bypass_api_limit":"0"}`, "premium"},
		{`{"error":0,"premium":0,"expire":null,"bypass_api_limit":1}`, "reseller"},
	}
	for _, c := range cases {
		srv := bestdebridServe(t, map[string]string{"/user": c.body})
		info, err := bestdebridAt(srv.URL).Account(context.Background())
		if err != nil {
			t.Fatalf("Account(%s): %v", c.body, err)
		}
		if info.Tier != c.want || !info.Traffic.Unlimited {
			t.Errorf("Account(%s) = %+v, want %s with unlimited traffic", c.body, info, c.want)
		}
	}
}

const bestdebridProxyChildEnv = "KL_BESTDEBRID_PROXY_CHILD"

// The test runs itself again in a child process because net/http reads
// HTTP_PROXY once per process.
func TestBestDebridCallsIgnoreTheEnvironmentProxy(t *testing.T) {
	if os.Getenv(bestdebridProxyChildEnv) == "1" {
		// 192.0.2.1 is reserved for documentation: a direct call reaches
		// nothing, a proxied one reaches the parent's proxy.
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = bestdebridAt("http://192.0.2.1").Hosts(ctx)
		fmt.Println("bestdebrid child done")
		return
	}
	var proxied atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied.Add(1)
		_, _ = io.WriteString(w, `[{"name":"rapidgator.net","status":"up"}]`)
	}))
	defer proxy.Close()

	cmd := exec.Command(os.Args[0], "-test.run=^TestBestDebridCallsIgnoreTheEnvironmentProxy$")
	cmd.Env = append(os.Environ(), bestdebridProxyChildEnv+"=1", "HTTP_PROXY="+proxy.URL, "NO_PROXY=")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "bestdebrid child done") {
		t.Fatalf("child process: %v\n%s", err, out)
	}
	if n := proxied.Load(); n != 0 {
		t.Errorf("%d call(s) went through HTTP_PROXY", n)
	}
}

func TestBestDebridOpensOneConnectionPerFile(t *testing.T) {
	r := Resolver{ServiceID: "bestdebrid", Svc: NewBestDebrid(bestdebridTestKey)}
	if got := r.HostCap("rapidgator.net"); got != 1 {
		t.Errorf("HostCap = %d, want 1", got)
	}
}

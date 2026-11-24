package access

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/appkey"
	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/internal/mcpclient"
	"github.com/junkerderprovinz/knightloader/internal/secret"
	"github.com/junkerderprovinz/knightloader/internal/store"
	"github.com/junkerderprovinz/parleyport/kit/signin"
)

// testdata holds what 2.9.5 wrote with the password below, the second factor
// on and three tokens: sealed with fixtureKey, and plain without an app key.
const (
	fixturePassword = "correct horse battery"
	fixtureKey      = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
)

type fixture struct {
	totp     string
	recovery string
	sonarr   string // read and add
	status   string // the fortress status alone
	odd      string // read and control
}

var fixtures = map[string]fixture{
	"sealed": {
		totp: "GORWWRFI7GSGLAYXZDRLTMMJ4XJTNUJC", recovery: "aryvs-fzh4a",
		sonarr: "kl_adkp8NsPsThxre_MQ3E3glCGsl_AZfLuxdDbYikrpds",
		status: "kl_JmAp7MEXXCu_186Ti0kN9WDreNWmddOs-LeKhcMC25s",
		odd:    "kl_xh0l9PT1vhcsG_MB6KGe9MnAurpbxRdX-MYzRWp5Cns",
	},
	"plain": {
		totp: "CPELLB356XI2UEMUENAH6AP4EZSYVDBG", recovery: "mgx3a-2xpnu",
		sonarr: "kl_FdE-Q6mEkDNMxcdvTTwHoDEKTE5kZoulg6o6DAVIgqI",
		status: "kl_owULEMGcT8InNARPwQwszfTTrHUrXMrx_BnJo96WkvY",
		odd:    "kl_mxS7oymOMHvmPBc5n_Ygcv4KtPcvRSE1bhXCOfczN8s",
	},
}

type instance struct {
	dir    string
	opts   Options
	auth   *signin.Auth
	srv    *httptest.Server
	client *http.Client

	mu    sync.Mutex
	ended []signin.Ending
}

func (in *instance) heard(what signin.Ending) bool {
	in.mu.Lock()
	defer in.mu.Unlock()
	return slices.Contains(in.ended, what)
}

// upgraded copies a 2.9.5 data folder and opens the sign-in on it.
func upgraded(t *testing.T, which string) *instance {
	t.Helper()
	dir := t.TempDir()
	from := filepath.Join("testdata", which)
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var key *appkey.Key
	if which == "sealed" {
		if key, err = appkey.Parse(fixtureKey); err != nil {
			t.Fatal(err)
		}
	}
	return start(t, dir, key)
}

func start(t *testing.T, dir string, key *appkey.Key) *instance {
	t.Helper()
	st, err := store.OpenWithKey(filepath.Join(dir, "knightloader.db"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	tokens, err := apitoken.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	mcp, err := mcpclient.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	in := &instance{dir: dir}
	in.opts = Options{
		Dir: dir, Store: st, AppKey: key, Tokens: tokens, MCP: mcp,
		Ended: func(_ context.Context, what signin.Ending) error {
			in.mu.Lock()
			in.ended = append(in.ended, what)
			in.mu.Unlock()
			return nil
		},
	}
	in.open(t)
	return in
}

func (in *instance) open(t *testing.T) {
	t.Helper()
	a, err := Open(context.Background(), in.opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	in.auth = a
	mux := http.NewServeMux()
	mux.Handle("/api/signin/", a.Handler())
	mux.HandleFunc("/who", func(w http.ResponseWriter, r *http.Request) {
		v, ok := signin.From(r.Context())
		if !ok {
			http.Error(w, "nobody", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(v)
	})
	in.srv = httptest.NewServer(a.Guard(mux))
	t.Cleanup(in.srv.Close)
	jar, _ := cookiejar.New(nil)
	in.client = &http.Client{Jar: jar}
}

func (in *instance) call(t *testing.T, method, path string, body any, header ...string) (int, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, in.srv.URL+path, r)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// A browser says where a change comes from, and the kit takes none without it.
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := in.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func (in *instance) who(t *testing.T, header ...string) (signin.Visitor, bool) {
	t.Helper()
	status, body := in.call(t, http.MethodGet, "/who", nil, header...)
	if status != http.StatusOK {
		return signin.Visitor{}, false
	}
	var v signin.Visitor
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatal(err)
	}
	return v, true
}

func (in *instance) signIn(t *testing.T, code string) {
	t.Helper()
	status, body := in.call(t, http.MethodPost, "/api/signin/v1/login", map[string]string{"password": fixturePassword})
	if status != http.StatusOK || !strings.Contains(string(body), `"step":"code"`) {
		t.Fatalf("login = %d %s, want the step for the second factor", status, body)
	}
	if status, body := in.call(t, http.MethodPost, "/api/signin/v1/login/totp", map[string]string{"code": code}); status != http.StatusOK {
		t.Fatalf("login/totp = %d %s", status, body)
	}
}

func TestTheOldPasswordAndSecondFactorSignIn(t *testing.T) {
	for which, f := range fixtures {
		t.Run(which, func(t *testing.T) {
			in := upgraded(t, which)
			if in.auth.Anyone() {
				t.Fatal("the upgraded instance is open, want its password to close it")
			}
			code, err := secret.TOTPCode(f.totp, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			in.signIn(t, code)
			if v, ok := in.who(t); !ok || v.Account != Owner || v.Role != "admin" {
				t.Errorf("after signing in: %+v %v, want the owner as admin", v, ok)
			}
		})
	}
}

func TestAnOldRecoveryCodeSignsIn(t *testing.T) {
	in := upgraded(t, "plain")
	in.signIn(t, fixtures["plain"].recovery)
	if _, ok := in.who(t); !ok {
		t.Error("the recovery code 2.9.5 gave out did not sign in")
	}
}

// oldCookie signs a session the way 2.9.5 issued them, from auth.json in the clear.
func oldCookie(t *testing.T, dir string, expires time.Time) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, auth.FileName))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Key   string `json:"key"`
		Epoch uint64 `json:"epoch"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	key, _ := hex.DecodeString(s.Key)
	payload := strconv.FormatInt(expires.Unix(), 10) + "." + rand.Text()
	m := hmac.New(sha256.New, key)
	m.Write([]byte(payload + "|" + strconv.FormatUint(s.Epoch, 10)))
	return payload + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func TestAnOldSessionStaysSignedInAndGetsANewCookie(t *testing.T) {
	in := upgraded(t, "plain")
	cookie := auth.CookieName + "=" + oldCookie(t, in.dir, time.Now().Add(20*24*time.Hour))
	v, ok := in.who(t, "Cookie", cookie)
	if !ok || v.Account != Owner {
		t.Fatalf("with the old cookie: %+v %v, want the owner", v, ok)
	}
	// The jar holds the kit's cookie now, without the old one.
	if v, ok := in.who(t); !ok || v.Via != signin.ViaPassword {
		t.Errorf("with the new cookie alone: %+v %v, want the owner signed in by password", v, ok)
	}
}

func TestAnOldSessionThatRanOutDoesNotSignIn(t *testing.T) {
	in := upgraded(t, "plain")
	cookie := auth.CookieName + "=" + oldCookie(t, in.dir, time.Now().Add(-time.Minute))
	if v, ok := in.who(t, "Cookie", cookie); ok {
		t.Errorf("an expired old cookie let in %+v", v)
	}
}

func TestOldTokensKeepTheirRights(t *testing.T) {
	for which, f := range fixtures {
		t.Run(which, func(t *testing.T) {
			in := upgraded(t, which)
			for _, c := range []struct {
				token string
				want  []apitoken.Scope
			}{
				{f.sonarr, []apitoken.Scope{apitoken.ScopeRead, apitoken.ScopeAdd}},
				{f.status, []apitoken.Scope{apitoken.ScopeStatus}},
				{f.odd, []apitoken.Scope{apitoken.ScopeRead, apitoken.ScopeControl}},
			} {
				v, ok := in.who(t, "Authorization", "Bearer "+c.token)
				if !ok || v.Via != signin.ViaToken {
					t.Fatalf("token %s… did not sign in: %+v", c.token[:8], v)
				}
				if got := apitoken.Granted(v.Scopes); !slices.Equal(got, c.want) {
					t.Errorf("token %s… may %v, want %v", c.token[:8], got, c.want)
				}
			}
			if _, ok := in.who(t, "Authorization", "Bearer kl_notAToken"); ok {
				t.Error("an unknown token signed in")
			}
		})
	}
}

func TestAnMCPClientsTokenStaysOutOfTheKit(t *testing.T) {
	dir := t.TempDir()
	tokens, err := apitoken.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	tok, raw, err := tokens.CreateScoped("Claude", []apitoken.Scope{apitoken.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	mcp, err := mcpclient.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mcp.Add("claude", tok.ID); err != nil {
		t.Fatal(err)
	}
	in := start(t, dir, nil)
	if !in.auth.Anyone() {
		t.Error("an instance without a password is closed")
	}
	if _, ok := in.auth.TokenVisitor(context.Background(), raw); ok {
		t.Error("the MCP client's token went to the kit")
	}
}

func TestAnInstanceWithoutASignInStartsOpenAndEmpty(t *testing.T) {
	in := start(t, t.TempDir(), nil)
	if !in.auth.Anyone() {
		t.Error("a fresh instance is closed")
	}
	if _, err := in.auth.Account(context.Background(), Owner); err == nil {
		t.Error("a fresh instance has an owner before anybody set a password")
	}
}

func TestTheSealKeyIsKeptAndSealedWithTheAppKey(t *testing.T) {
	dir := t.TempDir()
	key, err := appkey.Parse(fixtureKey)
	if err != nil {
		t.Fatal(err)
	}
	first, err := sealKey(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	again, err := sealKey(dir, key)
	if err != nil || !bytes.Equal(first, again) {
		t.Fatalf("with an app key the key changed: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, KeyFile))
	if !appkey.IsSealed(strings.TrimSpace(string(b))) {
		t.Error("with an app key the key file stays plain")
	}
	if _, err := sealKey(dir, nil); err == nil {
		t.Error("a sealed key file opened without the app key")
	}
}

func TestTheKitEndingAnOldWayInRemovesTheOldSignIn(t *testing.T) {
	in := upgraded(t, "plain")
	f := fixtures["plain"]
	code, _ := secret.TOTPCode(f.totp, time.Now())
	in.signIn(t, code)
	// A new password ends the old one, so the version before must not take
	// it back.
	if status, body := in.call(t, http.MethodPut, "/api/signin/v1/me/password", map[string]string{"current": fixturePassword, "next": "a new one"}); status != http.StatusOK {
		t.Fatalf("changing the password = %d %s", status, body)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := os.Stat(filepath.Join(in.dir, auth.FileName))
		if os.IsNotExist(err) && in.heard(signin.EndPassword) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("auth.json still there (%v) or KnightLoader not told", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(in.opts.Tokens.List()) != 0 {
		t.Errorf("tokens.json still holds %d tokens the kit took over", len(in.opts.Tokens.List()))
	}
	// The tokens moved, so they keep working through the kit.
	if _, ok := in.auth.TokenVisitor(context.Background(), f.sonarr); !ok {
		t.Error("a new password ended an API token")
	}
}

func TestASecondStartDoesNotImportAgain(t *testing.T) {
	in := upgraded(t, "plain")
	in.auth.Close()
	if err := os.Remove(filepath.Join(in.dir, auth.FileName)); err != nil {
		t.Fatal(err)
	}
	in.open(t)
	if in.auth.Anyone() {
		t.Error("the second start lost the imported password")
	}
}

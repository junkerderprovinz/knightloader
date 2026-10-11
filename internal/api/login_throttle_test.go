package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/auth"
)

// TestParallelGuessesStillMeetTheThrottle: an attacker who has the password
// sends a burst of code guesses at once. Every one of them must count before
// the first answer comes back, or the burst gets as many guesses as bcrypt
// can check rather than loginFailLimit.
func TestParallelGuessesStillMeetTheThrottle(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	armTwoFactor(t, a, "a-good-password")

	const burst = 4 * loginFailLimit
	codes := make(chan int, burst)
	var start, done sync.WaitGroup
	start.Add(1)
	for range burst {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			body, _ := json.Marshal(map[string]string{"password": "a-good-password", "code": "000000"})
			resp, err := http.Post(srv.URL+"/api/auth/login", "application/json", bytes.NewReader(body))
			if err != nil {
				t.Error(err)
				return
			}
			resp.Body.Close()
			codes <- resp.StatusCode
		}()
	}
	start.Done()
	done.Wait()
	close(codes)

	checked := 0
	for c := range codes {
		if c != http.StatusTooManyRequests {
			checked++
		}
	}
	if checked > loginFailLimit {
		t.Errorf("%d of %d parallel guesses were checked, want at most %d", checked, burst, loginFailLimit)
	}
}

// TestFiveFailedSignInsAMinute: the sixth wrong password within a minute is
// refused unchecked, and the client may try again once the first has left
// the window.
func TestFiveFailedSignInsAMinute(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		if code := login(t, http.DefaultClient, srv.URL, "not-the-password"); code != http.StatusUnauthorized {
			t.Fatalf("wrong password %d answered %d, want 401", i+1, code)
		}
	}
	if code := login(t, http.DefaultClient, srv.URL, "a-good-password"); code != http.StatusTooManyRequests {
		t.Fatalf("the right password after five wrong ones answered %d, want 429", code)
	}
}

// TestTheThrottleWindowSlides: a failure stops counting a minute after it
// happened, not a minute after the last one.
func TestTheThrottleWindowSlides(t *testing.T) {
	t.Parallel()
	g := newLoginGate(nil)
	now := time.Unix(1_700_000_000, 0)
	g.now = func() time.Time { return now }
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)

	for i := range loginFailLimit {
		if !g.try(r) {
			t.Fatalf("attempt %d was refused", i+1)
		}
		now = now.Add(10 * time.Second)
	}
	if g.try(r) {
		t.Fatalf("attempt %d within %v was let through", loginFailLimit+1, loginFailWindow)
	}
	// 50 seconds have passed; the first failure leaves the window at 60.
	now = now.Add(9 * time.Second)
	if g.try(r) {
		t.Fatal("an attempt was let through before the first failure left the window")
	}
	now = now.Add(time.Second)
	if !g.try(r) {
		t.Fatal("an attempt was refused after the first failure left the window")
	}
	if g.try(r) {
		t.Fatal("two attempts were let through when one failure had left the window")
	}
}

// TestRevealSharesTheLoginThrottle:the phrase route asks for the password
// again, so it must not be a way to guess the password the login throttles.
func TestRevealSharesTheLoginThrottle(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	c, _ := signedIn(t, srv.URL, "a-good-password")

	reveal := func(password string) int {
		body, _ := json.Marshal(map[string]string{"password": password})
		resp, err := c.Post(srv.URL+"/api/connect/reveal", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	var last int
	for range loginFailLimit + 1 {
		last = reveal("not-the-password")
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("after %d wrong passwords the phrase route answered %d, want 429", loginFailLimit+1, last)
	}
	if code := login(t, http.DefaultClient, srv.URL, "a-good-password"); code != http.StatusTooManyRequests {
		t.Errorf("the login answered %d right after the phrase route ran out of attempts, want 429", code)
	}
}

// TestTheSignInRoutesRefuseAnOversizedBody: these answer anybody, so a body
// is read only up to what a password, a passkey answer or a reset code can
// need.
func TestTheSignInRoutesRefuseAnOversizedBody(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	huge := append(append([]byte(`{"password":"`), bytes.Repeat([]byte("A"), 2*maxAuthBody)...), `"}`...)
	for _, path := range []string{"/api/auth/login", "/api/auth/passkey/login/finish", "/api/auth/reset/finish"} {
		resp, err := http.Post(srv.URL+path, "application/json", bytes.NewReader(huge))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Errorf("POST %s with a %d byte body answered %d, want 413", path, len(huge), resp.StatusCode)
		}
	}
}

// TestEveryJSONBodyHasACeiling: on an instance without a password every route
// answers anybody on the network.
func TestEveryJSONBodyHasACeiling(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	defer srv.Close()
	huge := append(append([]byte(`{"links":"`), bytes.Repeat([]byte("A"), maxJSONBody)...), `"}`...)
	resp, err := http.Post(srv.URL+"/api/links", "application/json", bytes.NewReader(huge))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("POST /api/links with a %d byte body answered %d, want 413", len(huge), resp.StatusCode)
	}
}

// endlessLinks is a JSON body whose string value never ends, counting what the
// server has taken of it. The transport can still be writing the body after
// the response is in, so sent is read atomically.
type endlessLinks struct {
	sent    atomic.Int64
	started bool
}

func (e *endlessLinks) Read(p []byte) (int, error) {
	n := 0
	if !e.started {
		n = copy(p, `{"links":"`)
		e.started = true
	}
	for i := n; i < len(p); i++ {
		p[i] = 'A'
	}
	e.sent.Add(int64(len(p)))
	return len(p), nil
}

// A body sent with a token that can add links or change downloads stops at
// the same ceiling as one sent without any credential.
func TestABodySentWithATokenStopsAtTheCeiling(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	_, secret, err := a.APITokens.CreateScoped("extension", []apitoken.Scope{apitoken.ScopeAdd, apitoken.ScopeControl})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/api/links", "/api/tasks/options"} {
		body := &endlessLinks{}
		req, _ := http.NewRequest(http.MethodPost, srv.URL+route, io.LimitReader(body, 8*maxJSONBody))
		req.Header.Set("Authorization", "Bearer "+secret)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusRequestEntityTooLarge {
				t.Errorf("%s answered %d, want 413", route, resp.StatusCode)
			}
		}
		if sent := body.sent.Load(); sent > 2*maxJSONBody {
			t.Errorf("%s took %d MiB of the body, the ceiling is %d MiB", route, sent>>20, maxJSONBody>>20)
		}
	}
}

// TestAClosedBucketCostsNoPasswordCheck: with an app key every check is an
// Argon2id run of 19 MiB, so a caller the gate has stopped must not get one.
// A bcrypt hash from before the key shows it: the first check of the right
// password replaces it in auth.json.
func TestAClosedBucketCostsNoPasswordCheck(t *testing.T) {
	dir := t.TempDir()
	plain, err := app.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := plain.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	plain.Close()
	a, err := app.NewWithKey(dir, newAppKey(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	srv := httptest.NewServer(Handler(a))
	t.Cleanup(srv.Close)
	trustLoopback(t, a)
	file := func() []byte {
		b, err := os.ReadFile(filepath.Join(dir, auth.FileName))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	for range loginFailLimit {
		loginFrom(t, srv.URL, "not-the-password", "203.0.113.5")
	}
	before := file()
	if got := loginFrom(t, srv.URL, "a-good-password", "203.0.113.5"); got != http.StatusTooManyRequests {
		t.Fatalf("the right password from a closed bucket answered %d, want 429", got)
	}
	if !bytes.Equal(file(), before) {
		t.Error("the right password from a closed bucket was checked")
	}
	if got := loginFrom(t, srv.URL, "a-good-password", "203.0.113.6"); got != http.StatusOK {
		t.Fatalf("the right password from another caller answered %d", got)
	}
	if bytes.Equal(file(), before) {
		t.Fatal("a check of the right password left auth.json as it was, so this test proves nothing")
	}
}

// TestAnIPv6NetworkCannotMintFreshAllowances: each /64 has an allowance of its
// own, and whoever holds a /48 holds 65536 of them. The /48 as a whole stops
// at loginNetworkLimit, while another network is not held up by it.
func TestAnIPv6NetworkCannotMintFreshAllowances(t *testing.T) {
	t.Parallel()
	g := newLoginGate(nil)
	from := func(addr string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
		r.RemoteAddr = "[" + addr + "]:443"
		return r
	}
	let := 0
	for sub := range 1000 {
		r := from(fmt.Sprintf("2001:db8:7:%x::1", sub))
		for range loginFailLimit {
			if g.try(r) {
				let++
			}
		}
	}
	if let > loginNetworkLimit {
		t.Errorf("one /48 had %d guesses within the window, want at most %d", let, loginNetworkLimit)
	}
	if !g.try(from("2001:db8:8::1")) {
		t.Error("a caller from another /48 was refused")
	}
	if !g.try(httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)) {
		t.Error("an IPv4 caller was refused")
	}
}

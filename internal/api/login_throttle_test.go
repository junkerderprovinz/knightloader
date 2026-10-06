package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
)

// TestParallelGuessesStillMeetTheThrottle: an attacker who has the password
// sends a burst of code guesses at once. Every one of them must count before
// the first answer comes back, or the burst gets as many guesses as bcrypt
// can check rather than loginFailBurst.
func TestParallelGuessesStillMeetTheThrottle(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	armTwoFactor(t, a, "a-good-password")

	const burst = 4 * loginFailBurst
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
	if checked > loginFailBurst {
		t.Errorf("%d of %d parallel guesses were checked, want at most %d", checked, burst, loginFailBurst)
	}
}

// TestRevealSharesTheLoginThrottle: the phrase route asks for the password
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
	for range loginFailBurst + 1 {
		last = reveal("not-the-password")
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("after %d wrong passwords the phrase route answered %d, want 429", loginFailBurst+1, last)
	}
	if code := login(t, http.DefaultClient, srv.URL, "a-good-password"); code != http.StatusTooManyRequests {
		t.Errorf("the login answered %d right after the phrase route ran out of attempts, want 429", code)
	}
}

// TestTheSignInRoutesRefuseAnOversizedBody: these answer anybody, so a body
// is read only up to what a password or a passkey answer can need.
func TestTheSignInRoutesRefuseAnOversizedBody(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	if err := a.Auth.SetPassword("", "a-good-password"); err != nil {
		t.Fatal(err)
	}
	huge := append(append([]byte(`{"password":"`), bytes.Repeat([]byte("A"), 2*maxAuthBody)...), `"}`...)
	for _, path := range []string{"/api/auth/login", "/api/auth/passkey/login/finish"} {
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
// server has taken of it.
type endlessLinks struct {
	sent    int64
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
	e.sent += int64(len(p))
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
		if body.sent > 2*maxJSONBody {
			t.Errorf("%s took %d MiB of the body, the ceiling is %d MiB", route, body.sent>>20, maxJSONBody>>20)
		}
	}
}

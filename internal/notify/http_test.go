package notify

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/script"
)

const secretToken = "Bearer s3cr3t-token-value"

// deadAddress is a test server that has already been shut down, which produces
// a real transport failure without waiting for a routing black hole.
func deadAddress(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	addr := srv.URL
	srv.Close()
	return addr
}

func TestSendReportsWhatWasSentWithTheSecretsMaskedAgain(t *testing.T) {
	var gotAuth, gotBody, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	target := Target{
		URL: srv.URL + "/topic", Method: MethodPOST,
		Headers: map[string]string{"Authorization": secretToken, "Content-Type": "text/plain"},
		Body:    "%%task.name%% finished",
	}
	att := Send(context.Background(), target, firingWithTask("film.mkv"), "box")

	if !att.OK() {
		t.Fatalf("the send failed: %+v", att)
	}
	if gotMethod != http.MethodPost || gotBody != "film.mkv finished" || gotAuth != secretToken {
		t.Fatalf("the far end saw %s %q with auth %q", gotMethod, gotBody, gotAuth)
	}
	if att.Sent.Headers["Authorization"] != RedactedValue {
		t.Errorf("the reported request carries the real token (%q), and it goes straight to a browser",
			att.Sent.Headers["Authorization"])
	}
	if att.Sent.Body != "film.mkv finished" {
		t.Errorf("the reported body is %q, want the expanded one", att.Sent.Body)
	}
}

func TestSendKeepsTheTokenOutOfTheErrorString(t *testing.T) {
	// The whole address, query included, is what *url.Error prints back.
	addr := deadAddress(t) + "/topic?token=SUPERSECRET"
	att := Send(context.Background(), Target{URL: addr, Headers: map[string]string{"Authorization": secretToken}}, firingWithTask("x"), "")

	if att.Err == "" {
		t.Fatalf("a dead address answered without an error: %+v", att)
	}
	if strings.Contains(att.Err, "SUPERSECRET") {
		t.Errorf("the query token is in the error, which reaches the health row, the test panel and the log: %s", att.Err)
	}
	if strings.Contains(att.Err, "s3cr3t-token-value") {
		t.Errorf("the header token is in the error: %s", att.Err)
	}
	if !att.Retryable {
		t.Error("a transport failure was marked not retryable")
	}
	if att.Code != ProblemRefused {
		t.Errorf("a closed port was classified as %q (%s), want %q", att.Code, att.Err, ProblemRefused)
	}
}

func TestSendRedactsWhatTheFarEndEchoesBack(t *testing.T) {
	// Several push servers quote the request back in their own error document.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "we did not like: "+r.Header.Get("Authorization"))
	}))
	defer srv.Close()

	att := Send(context.Background(), Target{URL: srv.URL, Headers: map[string]string{"Authorization": secretToken}}, firingWithTask("x"), "")
	if strings.Contains(att.Body, "s3cr3t-token-value") {
		t.Errorf("the answer put the token on screen: %s", att.Body)
	}
	if !strings.Contains(att.Body, RedactedValue) {
		t.Errorf("the answer was not redacted at all: %s", att.Body)
	}
}

func TestSendCapsTheAnswerAndSaysThatItDid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", MaxResponseBody*2))
	}))
	defer srv.Close()

	att := Send(context.Background(), Target{URL: srv.URL}, firingWithTask("x"), "")
	if len(att.Body) != MaxResponseBody {
		t.Errorf("kept %d bytes, want the cap of %d", len(att.Body), MaxResponseBody)
	}
	if !att.Truncated {
		t.Error("the answer was cut and the panel was not told")
	}
}

func TestSendSkipsAHeaderWithNoValue(t *testing.T) {
	// The state Merge leaves behind when a stored secret could not follow a
	// changed address. A bare "Authorization:" is a 401 nobody can diagnose.
	seen := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, seen = r.Header["Authorization"]
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	Send(context.Background(), Target{URL: srv.URL, Headers: map[string]string{"Authorization": ""}}, firingWithTask("x"), "")
	if seen {
		t.Error("an empty header value was put on the wire")
	}
}

func TestClassifyStatusSplitsTheFourHundreds(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
		retry  bool
	}{
		{200, "", false},
		{204, "", false},
		// clientFor hands a 3xx back rather than carrying a custom header to
		// another origin, so it reaches classifyStatus and has to be named.
		// Not retried: the same address answers the same way next time, and
		// the thing to do is type the address it points at.
		{301, ProblemRedirect, false},
		{302, ProblemRedirect, false},
		{308, ProblemRedirect, false},
		{401, ProblemAuth, false},
		{403, ProblemAuth, false},
		{404, ProblemNotFound, false},
		{408, ProblemTimeout, true},
		{422, ProblemRejected, false},
		{429, ProblemRateLimited, true},
		{500, ProblemServer, true},
		{503, ProblemServer, true},
	} {
		if got := classifyStatus(tc.status); got != tc.code {
			t.Errorf("classifyStatus(%d) = %q, want %q", tc.status, got, tc.code)
		}
		if got := retryableStatus(tc.status); got != tc.retry {
			t.Errorf("retryableStatus(%d) = %v, want %v", tc.status, got, tc.retry)
		}
	}
}

// The errors are built the way net/http hands them back. A real lookup of an
// .invalid name tests the resolver instead: a slow one lets the target's time
// limit run out first, and that is rightly a timeout.
func TestClassifyErrorNamesWhatItCan(t *testing.T) {
	lookup := &url.Error{Op: "Post", URL: "https://x.invalid/x", Err: &net.OpError{Op: "dial", Net: "tcp",
		Err: &net.DNSError{Err: "no such host", Name: "x.invalid", IsNotFound: true}}}
	if got := classifyError(lookup); got != ProblemDNS {
		t.Errorf("a failed lookup was classified as %q, want %q", got, ProblemDNS)
	}
	late := &url.Error{Op: "Post", URL: "https://x.invalid/x", Err: context.DeadlineExceeded}
	if got := classifyError(late); got != ProblemTimeout {
		t.Errorf("a lookup cut short by the time limit was classified as %q, want %q", got, ProblemTimeout)
	}
}

func TestSendHonoursTheTargetsOwnTimeLimit(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(block)

	started := time.Now()
	att := Send(context.Background(), Target{URL: srv.URL, TimeoutSeconds: 1}, firingWithTask("x"), "")
	if took := time.Since(started); took > 5*time.Second {
		t.Fatalf("a one second limit took %s", took)
	}
	if att.Code != ProblemTimeout {
		t.Errorf("a server that never answers was classified as %q (%s), want %q", att.Code, att.Err, ProblemTimeout)
	}
}

func TestSendUsesTheTriggersOwnPayload(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
	}))
	defer srv.Close()

	f := script.Firing{Trigger: script.TriggerPackageDone, At: time.Now(), Package: &script.PackageView{Name: "Season 1", Files: 8, Failed: 1}}
	Send(context.Background(), Target{URL: srv.URL, Body: "%%package.name%%: %%package.failed%% missing of %%package.files%%"}, f, "")
	if body != "Season 1: 1 missing of 8" {
		t.Errorf("the body arrived as %q", body)
	}
}

// A custom header is a secret this package cannot recognise, and a redirect is
// where it would be handed away. httpx strips Authorization,
// Proxy-Authorization, Cookie and Cookie2 on a hop to another origin and cannot
// strip more, so X-Gotify-Key, X-Api-Key and ntfy's token header would ride
// along to whoever owns the hop.
//
// The far end here is another origin rather than another path: the two httptest
// servers on 127.0.0.1 differ only by port, which is the case httpx.sameOrigin
// was written for.
func TestSendDoesNotCarryACustomHeaderAcrossARedirect(t *testing.T) {
	const key = "X-Gotify-Key"
	const value = "gotify-secret-value"

	var leaked string
	var reached bool
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		leaked = r.Header.Get(key)
		w.WriteHeader(http.StatusOK)
	}))
	defer elsewhere.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/login", http.StatusFound)
	}))
	defer redirector.Close()

	target := Target{
		URL: redirector.URL + "/message", Method: MethodPOST,
		Headers: map[string]string{key: value, "Content-Type": "text/plain"},
		Body:    "hello",
	}
	att := Send(context.Background(), target, firingWithTask("film.mkv"), "box")

	if reached {
		t.Errorf("the redirect was followed to another origin, which saw %q in %s", leaked, key)
	}
	if leaked == value {
		t.Errorf("%s reached a host the operator never named", key)
	}
	// The 3xx reaches the caller as itself, so the test button can say the
	// address redirects instead of reporting whatever the far end answered.
	if att.Status < 300 || att.Status >= 400 {
		t.Errorf("the attempt reports status %d, want the 3xx handed back", att.Status)
	}
}

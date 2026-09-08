package notify

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/script"
)

const secretToken = "Bearer s3cr3t-token-value"

// deadAddress is an address nothing is listening on: a test server that has
// already been shut down. It is how a real transport failure is produced
// without waiting for a routing black hole.
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
		t.Errorf("the reported request carries the real token (%q); this struct is serialised straight to a browser "+
			"that was never shown it", att.Sent.Headers["Authorization"])
	}
	if att.Sent.Body != "film.mkv finished" {
		t.Errorf("the reported body is %q, want the expanded one so a refusal can be read rather than guessed at", att.Sent.Body)
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
		t.Error("a transport failure was marked not retryable; nothing about the request has been shown to be wrong")
	}
	if att.Code != ProblemRefused {
		t.Errorf("a closed port was classified as %q (%s), want %q - and note that the message is LOCALISED, "+
			"so this cannot be decided by looking for the words \"connection refused\" in it", att.Code, att.Err, ProblemRefused)
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
		t.Error("the answer was cut and the panel was not told, so it would show a sentence stopping mid-word with no explanation")
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
		{301, "", false},
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
			t.Errorf("retryableStatus(%d) = %v, want %v; a refusal repeated is one mistake turned into three requests "+
				"against somebody's public instance", tc.status, got, tc.retry)
		}
	}
}

func TestClassifyErrorNamesWhatItCan(t *testing.T) {
	// A host nobody can resolve. .invalid is reserved by RFC 2606 precisely so
	// that this cannot accidentally reach a real server.
	att := Send(context.Background(), Target{URL: "https://this-host-does-not-exist.invalid/x", TimeoutSeconds: 5}, firingWithTask("x"), "")
	if att.Code != ProblemDNS {
		t.Errorf("an unresolvable host was classified as %q (%s), want %q - a container has its own resolver and that is the commonest cause",
			att.Code, att.Err, ProblemDNS)
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

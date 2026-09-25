package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// wireRun is the Run as it arrives at a browser. It does not reuse
// selftest.Run, so a renamed field fails this test instead of decoding to zero.
type wireRun struct {
	ID         string    `json:"id"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
	Planned    []string  `json:"planned"`
	Results    []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Code   string `json:"code"`
	} `json:"results"`
}

// selfTestServer is the three routes on a throwaway app, with yt-dlp pointed at
// a missing path so a sweep launches no real process.
func selfTestServer(t *testing.T) (*httptest.Server, *Registry) {
	t.Helper()
	t.Setenv("KL_YTDLP", filepath.Join(t.TempDir(), "no-such-yt-dlp"))
	t.Setenv("KL_JD", "")
	reg := newRegistry()
	registerSelfTest(reg, testApp(t))
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, reg
}

// TestTheSelfTestRoutesAreNeverOpen checks that the routes, which send folder
// paths, account labels and received headers, all need a session.
func TestTheSelfTestRoutesAreNeverOpen(t *testing.T) {
	_, reg := selfTestServer(t)
	for _, path := range []string{"/api/selftest", "/api/selftest/request"} {
		if reg.open(path) {
			t.Errorf("%s answers without a session; it carries no credential of its own and must ride the "+
				"session guard like everything else under /api/", path)
		}
	}

	want := map[string]bool{
		"POST /api/selftest":        true,
		"GET /api/selftest":         true,
		"GET /api/selftest/request": true,
	}
	got := map[string]bool{}
	for _, r := range reg.Routes() {
		got[r.Method+" "+r.Path] = true
		if strings.TrimSpace(r.Summary) == "" {
			t.Errorf("%s %s has no summary; it renders as a blank line in the API index", r.Method, r.Path)
		}
		if r.Method == AnyMethod {
			t.Errorf("%s answers whatever it is sent; a route that acts on this instance names its method so "+
				"a GET can never be made to do a POST's job", r.Path)
		}
	}
	for m := range want {
		if !got[m] {
			t.Errorf("%s was not registered", m)
		}
	}
	for m := range got {
		if !want[m] {
			t.Errorf("%s was registered and is not one of the three this feature declares", m)
		}
	}
}

// TestASweepStartedOverTheWireIsTheSweepTheNextGetReports checks that POST
// answers 202 with an id and the planned list at once, and that GET then
// reports that same run until it finishes.
func TestASweepStartedOverTheWireIsTheSweepTheNextGetReports(t *testing.T) {
	srv, _ := selfTestServer(t)

	resp, err := http.Post(srv.URL+"/api/selftest", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /api/selftest answered %d, want 202", resp.StatusCode)
	}
	var started wireRun
	if err := json.NewDecoder(resp.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	if started.ID == "" {
		t.Fatal("the sweep answered with no id; the page has nothing to poll for")
	}
	if len(started.Planned) == 0 {
		t.Fatal("the 202 named no planned checks; the page draws its pending rows from that list and would " +
			"show nothing at all until the first result landed")
	}
	if started.StartedAt.IsZero() {
		t.Error("the sweep answered with no start time")
	}
	if !started.FinishedAt.IsZero() {
		t.Error("the 202 already carries a finish time; the page polls until that appears and would stop " +
			"before a single check had reported")
	}

	deadline := time.Now().Add(30 * time.Second)
	var final wireRun
	for {
		code, raw := getRaw(t, srv.URL+"/api/selftest")
		if code != http.StatusOK {
			t.Fatalf("GET /api/selftest answered %d: %s", code, raw)
		}
		if err := json.Unmarshal(raw, &final); err != nil {
			t.Fatal(err)
		}
		if final.ID != started.ID {
			t.Fatalf("GET reports run %q while %q was started; the page would draw somebody else's sweep", final.ID, started.ID)
		}
		if !final.FinishedAt.IsZero() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the sweep did not finish inside 30s; %d of %d checks reported", len(final.Results), len(final.Planned))
		}
		time.Sleep(10 * time.Millisecond)
	}

	if len(final.Results) != len(final.Planned) {
		t.Fatalf("%d results for %d planned checks; a planned check that never reports is a row that says "+
			"\"waiting\" for ever", len(final.Results), len(final.Planned))
	}
	for _, r := range final.Results {
		if r.Status == "" || r.Code == "" {
			t.Errorf("%s came back as %+v; without a code the page has no sentence to look up and draws a blank row", r.ID, r)
		}
	}
}

// TestAnInstanceThatHasNeverBeenSweptAnswersTwoHundred pins the state of a
// fresh install, where the page decodes the answer as JSON.
func TestAnInstanceThatHasNeverBeenSweptAnswersTwoHundred(t *testing.T) {
	srv, _ := selfTestServer(t)
	code, raw := getRaw(t, srv.URL+"/api/selftest")
	if code != http.StatusOK {
		t.Fatalf("GET /api/selftest answered %d before anything was swept: %s", code, raw)
	}
	var got struct {
		ID      string            `json:"id"`
		Planned []string          `json:"planned"`
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "" {
		t.Errorf("id = %q before anything was swept", got.ID)
	}
	if got.Planned == nil || got.Results == nil {
		t.Fatalf("planned or results came back as JSON null: %s; the page walks both and throws on null", raw)
	}
}

// TestTheRequestEchoReportsWhatArrivedAndCountsTheHopsWithoutNamingThem checks
// that X-Forwarded-For travels as a count and never as addresses.
func TestTheRequestEchoReportsWhatArrivedAndCountsTheHopsWithoutNamingThem(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/api/selftest/request", nil)
	r.Host = "kl.example.com"
	r.Header.Set("X-Forwarded-Host", "knightloader.lan")
	r.Header.Set("X-Forwarded-Proto", "HTTPS")
	r.Header.Add("X-Forwarded-For", "203.0.113.9, 10.0.0.4")
	r.Header.Add("X-Forwarded-For", "192.168.20.11")

	view := requestViewOf(r)
	if view.Host != "kl.example.com" {
		t.Errorf("host = %q, want what this instance actually received", view.Host)
	}
	if view.ForwardedHost != "knightloader.lan" {
		t.Errorf("forwardedHost = %q, want knightloader.lan", view.ForwardedHost)
	}
	if view.ForwardedProto != "https" {
		t.Errorf("forwardedProto = %q, want it lowercased", view.ForwardedProto)
	}
	if view.ForwardedForHops != 3 {
		t.Errorf("forwardedForHops = %d, want 3: two in the first header value and one in the second", view.ForwardedForHops)
	}
	if view.Now.IsZero() {
		t.Error("the echo carries no clock; the browser subtracts it from its own to find the drift")
	}

	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, addr := range []string{"203.0.113.9", "10.0.0.4", "192.168.20.11"} {
		if strings.Contains(string(raw), addr) {
			t.Fatalf("the request echo carries %s; the forwarded chain travels as a count: %s", addr, raw)
		}
	}
}

// TestABareForwardedPrefixIsNotAPathPrefix checks that "/" and similar values,
// which several proxies send for the root, are not reported as a prefix.
func TestABareForwardedPrefixIsNotAPathPrefix(t *testing.T) {
	t.Parallel()
	for _, header := range []string{"", "/", "  /  ", "//"} {
		r := httptest.NewRequest(http.MethodGet, "/api/selftest/request", nil)
		if header != "" {
			r.Header.Set("X-Forwarded-Prefix", header)
		}
		if got := requestViewOf(r).ForwardedPrefix; got != "" {
			t.Errorf("X-Forwarded-Prefix %q became %q, want empty", header, got)
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/api/selftest/request", nil)
	r.Header.Set("X-Forwarded-Prefix", "/kl/")
	if got := requestViewOf(r).ForwardedPrefix; got != "/kl" {
		t.Errorf("X-Forwarded-Prefix %q became %q, want %q", "/kl/", got, "/kl")
	}
}

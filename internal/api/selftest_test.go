package api

// The self-test's three routes: that they are locked, that a sweep started over
// the wire is the sweep the next GET reports, and that the request echo says
// what this instance saw without saying anything about somebody's internal
// network.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// wireRun is the Run as it arrives at a browser. Declared here rather than
// reusing selftest.Run so that a field quietly renamed on the Go side fails
// this test instead of decoding into a zero value nobody notices.
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
// a path that cannot exist so a sweep started here launches no real process and
// finishes in milliseconds.
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

// TestTheSelfTestRoutesAreNeverOpen is the one that matters if somebody ever
// finds this file convenient. The sweep sends folder paths, a JD address,
// account labels and provider error sentences; the request echo sends this
// instance's own headers. None of that carries a credential of its own in the
// request, so none of it may answer without a session.
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

// TestASweepStartedOverTheWireIsTheSweepTheNextGetReports is the delivery
// contract the page rests on: POST answers 202 with an id and the planned list
// straight away, and GET reports THAT run rather than a different one. The
// results are not in the POST's own response on purpose - the sweep outlives
// the request, and a reverse proxy's sixty-second read timeout is a failure
// this repo has already hit once on POST /api/links.
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

// TestAnInstanceThatHasNeverBeenSweptAnswersTwoHundred pins the state every
// install is in on the first load of the page. A 404 there would make the
// browser's own json() decoder throw on the ordinary case.
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
		t.Fatalf("planned or results came back as JSON null: %s - the page walks both and throws on null", raw)
	}
}

// TestTheRequestEchoReportsWhatArrivedAndCountsTheHopsWithoutNamingThem is the
// privacy line on this feature. X-Forwarded-For names a network's internal
// proxies; the count answers the only question the proxy card asks of it, and
// the addresses answer nothing anybody needed.
func TestTheRequestEchoReportsWhatArrivedAndCountsTheHopsWithoutNamingThem(t *testing.T) {
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
		t.Errorf("forwardedHost = %q; its mismatch with Host is the whole diagnosis of a 403 storm behind a "+
			"proxy that rewrites the Host header", view.ForwardedHost)
	}
	if view.ForwardedProto != "https" {
		t.Errorf("forwardedProto = %q, want it lowercased - proxies send both cases and the page compares "+
			"against a literal", view.ForwardedProto)
	}
	if view.ForwardedForHops != 3 {
		t.Errorf("forwardedForHops = %d, want 3 - two in the first header value and one in the second", view.ForwardedForHops)
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
			t.Fatalf("the request echo carries %s; the forwarded chain is a map of somebody's internal "+
				"network and travels as a count, never as addresses: %s", addr, raw)
		}
	}
}

// TestABareForwardedPrefixIsNotAPathPrefix keeps the prefix row quiet on a
// correctly configured install. Several proxies send "/" for "the root", and
// reporting that as a path prefix would put a red row - one whose advice is
// "give this app a subdomain of its own" - in front of somebody who already has
// exactly that.
func TestABareForwardedPrefixIsNotAPathPrefix(t *testing.T) {
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
		t.Errorf("X-Forwarded-Prefix %q became %q, want %q - this is the one signature of a stripped path "+
			"prefix that survives the stripping, and the only way this instance can see one at all",
			"/kl/", got, "/kl")
	}
}

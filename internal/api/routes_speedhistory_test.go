package api

// GET /api/stats/speed over a real app, where the shape of the answer is the
// contract. The client takes .length off both arrays without checking, reads
// the step and the cap out of the document rather than carrying a second copy,
// and decides whether to draw a curve from whether recordingSince is there.
//
// What the ring does, dropping its oldest entry, filling a suspend with zeros,
// averaging ten fine readings into one coarse bucket, is pinned in
// internal/app/speedhistory_test.go, where the sampler can be handed
// fabricated instants and a four hour suspend costs microseconds. Driving that
// from here would mean sleeping through real seconds or exporting a push-a-
// sample seam no other caller needs. What is left here is what the wire adds:
// the encoding, the registration, and that no query parameter changes either.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

func speedServer(t *testing.T) (*app.App, *httptest.Server) {
	t.Helper()
	a := testApp(t)
	reg := newRegistry()
	registerSpeedHistory(reg, a)
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return a, srv
}

// TestTheSpeedRecordIsNeverNullAndDeclaresItsOwnBounds: a ring that has never
// been written answers "recent": null if it was built with var out []int64,
// and the client's .length throws on the first load after a restart, which is
// the moment this feature is for. Decoding into the struct would hide it,
// since JSON null and an empty array both decode to a usable Go slice, so the
// raw bytes are checked as well.
func TestTheSpeedRecordIsNeverNullAndDeclaresItsOwnBounds(t *testing.T) {
	_, srv := speedServer(t)

	resp, err := http.Get(srv.URL + "/api/stats/speed")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/stats/speed: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unparseable answer: %v (%s)", err, body)
	}
	for _, field := range []string{"recent", "hour"} {
		v, ok := raw[field]
		if !ok {
			t.Fatalf("no %q in the answer: %v", field, raw)
		}
		if !strings.HasPrefix(string(v), "[") {
			t.Errorf("%q is %s; an empty record has to cross the wire as [] or the client's "+
				".length throws on the first load after a restart", field, v)
		}
	}

	var got app.SpeedHistory
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	// The caps and the steps are read out of the document rather than kept in
	// the client, so they have to be there and they have to be the real ones.
	if got.RecentStep != 1 || got.RecentCap != 120 {
		t.Errorf("the fine record describes itself as %d s x %d, want 1 s x 120", got.RecentStep, got.RecentCap)
	}
	if got.HourStep != 10 || got.HourCap != 360 {
		t.Errorf("the hour record describes itself as %d s x %d, want 10 s x 360", got.HourStep, got.HourCap)
	}
	// Bounded by construction, observed at the wire: the promise that lets the
	// route do without a ?limit.
	if len(got.Recent) > got.RecentCap || len(got.Hour) > got.HourCap {
		t.Errorf("%d of %d recent and %d of %d hourly: an answer longer than the cap it declares",
			len(got.Recent), got.RecentCap, len(got.Hour), got.HourCap)
	}
	// recordingSince is present exactly when there is something to be
	// recording since, which tells "this instance was quiet" apart from "this
	// instance had not started yet".
	_, hasSince := raw["recordingSince"]
	if recorded := len(got.Recent) > 0 || len(got.Hour) > 0; recorded != hasSince {
		t.Errorf("%d recent samples with recordingSince present = %v", len(got.Recent), hasSince)
	}
	if got.SampledAt.IsZero() {
		t.Error("no sampledAt, so nothing can say how old the answer is")
	}
}

// TestTheSpeedRouteTakesNoParameters is written against the source rather than
// against a response, like TestNothingRegistersOutsideTheTable: the failure it
// prevents is somebody writing the line, and once a ?limit= is honoured an
// answer truncated to what the caller asked for looks like an answer that was
// that long.
//
// routes_stats.go states the rule for the volume curves, and it holds more
// sharply here because this record seeds a graph: a client that could ask for
// fewer samples than the ring holds would draw a window shorter than its own
// abscissa claims.
func TestTheSpeedRouteTakesNoParameters(t *testing.T) {
	src, err := os.ReadFile("routes_speedhistory.go")
	if err != nil {
		t.Fatal(err)
	}
	// Split so that this test file does not match itself through the string.
	for _, forbidden := range []string{"URL." + "Query", "r." + "FormValue", "ParseForm"} {
		if strings.Contains(string(src), forbidden) {
			t.Errorf("routes_speedhistory.go reads %s; the record is bounded by construction and "+
				"a parameter would quietly take that promise away", forbidden)
		}
	}
}

// TestTheSpeedRouteSaysItIsMemoryOnly: "why is my curve flat" has two answers
// that look identical on the page, the sampler is dead or the box was quiet,
// and a third that is neither, the process restarted a minute ago and the
// record starts empty every time. It is answered in three places, and this
// summary is the one an operator meets first, because the self-describing
// index at GET /api/ prints these and nothing else.
func TestTheSpeedRouteSaysItIsMemoryOnly(t *testing.T) {
	a := testApp(t)
	reg := newRegistry()
	registerSpeedHistory(reg, a)

	var summary string
	for _, r := range reg.Routes() {
		if r.Path == "/api/stats/speed" {
			summary = r.Summary
		}
	}
	if summary == "" {
		t.Fatal("/api/stats/speed is not in the registry at all")
	}
	for _, word := range []string{"memory", "restart"} {
		if !strings.Contains(summary, word) {
			t.Errorf("the summary never says %q: %q\n"+
				"this line is where somebody reading a flat curve finds out that an empty record "+
				"after a restart is the design and not a fault", word, summary)
		}
	}
}

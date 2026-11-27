package feed

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// testdata/mediathek-tagesschau.json is MediathekViewWeb's answer to a
// search for topic "tagesschau" on channel "ard", trimmed to three results
// with shortened descriptions.

func TestAMediathekSearchAddressReadsBackAsARow(t *testing.T) {
	for _, tc := range []struct {
		url  string
		want Mediathek
		min  int
		max  int
	}{
		{"https://mediathekviewweb.de/#query=%21ard%20%23tatort%20%3E80", Mediathek{Channel: "ard", Topic: "tatort"}, 80, 0},
		{"https://mediathekviewweb.de/#query=!zdf%20%2Bheute%20%3C20&everywhere=true", Mediathek{Channel: "zdf", Title: "heute"}, 0, 20},
		{"https://mediathekviewweb.de/#query=%23sturm%20%23der%20%23liebe", Mediathek{Topic: "sturm der liebe"}, 0, 0},
		// Plain words search the show, as most people type its name.
		{"https://mediathekviewweb.de/#query=tatort%20m%C3%BCnster", Mediathek{Topic: "tatort münster"}, 0, 0},
		{"https://www.mediathekviewweb.de/#query=%23tatort+%2Bkommissar", Mediathek{Topic: "tatort", Title: "kommissar"}, 0, 0},
	} {
		got, ok := ParseMediathekURL(tc.url)
		if !ok {
			t.Errorf("%s was not read as a search", tc.url)
			continue
		}
		tc.want.Quality = QualityHD
		if got.Kind != KindMediathek || *got.Mediathek != tc.want || got.MinMinutes != tc.min || got.MaxMinutes != tc.max {
			t.Errorf("%s: got %+v %+v, want %+v min %d max %d", tc.url, got, got.Mediathek, tc.want, tc.min, tc.max)
		}
		if got.URL == "" || got.Validate() != nil {
			t.Errorf("%s: the row is not ready to save: %+v (%v)", tc.url, got, got.Validate())
		}
	}
	for _, not := range []string{
		"https://mediathekviewweb.de/",
		"https://mediathekviewweb.de/#query=",
		"https://example.invalid/#query=tatort",
		"https://mediathekviewweb.de/#query=%3E80",
	} {
		if _, ok := ParseMediathekURL(not); ok {
			t.Errorf("%s was taken for a search", not)
		}
	}
}

func TestTheSameSearchAlwaysGetsTheSameAddress(t *testing.T) {
	a := MediathekURL(Mediathek{Channel: "ARD", Topic: "Tatort"}, 80, 0)
	b, _ := ParseMediathekURL(a)
	if b.URL != a {
		t.Fatalf("%s came back as %s, so pasting it again would make a second subscription", a, b.URL)
	}
}

// mediathekAPI serves the fixture and keeps the last query it was sent.
func mediathekAPI(t *testing.T) (*httptest.Server, *mediathekQuery) {
	t.Helper()
	body, err := os.ReadFile("testdata/mediathek-tagesschau.json")
	if err != nil {
		t.Fatal(err)
	}
	var got mediathekQuery
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "text/plain" {
			http.Error(w, "the API takes a text/plain POST", http.StatusBadRequest)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &got); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestAMediathekSearchStagesTheChosenFile(t *testing.T) {
	srv, sent := mediathekAPI(t)
	sub := Sanitize([]Subscription{{
		Kind: KindMediathek, MinMinutes: 10,
		Mediathek: &Mediathek{Channel: "ARD", Topic: "tagesschau", Subtitles: true},
	}})[0]
	f, err := queryMediathek(context.Background(), srv.Client(), srv.URL, sub, 50)
	if err != nil {
		t.Fatal(err)
	}

	if sent.SortBy != "timestamp" || sent.SortOrder != "desc" || sent.DurationMin != 600 || sent.Size != 50 || sent.Future {
		t.Errorf("the query was %+v", sent)
	}
	fields := map[string]string{}
	for _, q := range sent.Queries {
		fields[strings.Join(q.Fields, ",")] = q.Query
	}
	if fields["channel"] != "ARD" || fields["topic"] != "tagesschau" || len(fields) != 2 {
		t.Errorf("the query searched %v", fields)
	}

	if len(f.Items) != 3 {
		t.Fatalf("got %d entries", len(f.Items))
	}
	it := f.Items[0]
	if it.Link != "https://tagesschau-progressive.ard-mcdn.de/video/2026/1126/TV-20261126-2023-0200.webxxl.h264.mp4" {
		t.Errorf("link is %s, want the HD file", it.Link)
	}
	if it.GUID != "Vnl44Ut069YIo+pThvBomg641WNgGUTqz7ciyV2hrsc=" || it.Title != "tagesschau 20:00 Uhr, 26.11.2026" {
		t.Errorf("entry: %+v", it)
	}
	if it.Duration != 949*time.Second || !it.Published.Equal(time.Unix(1795719600, 0)) {
		t.Errorf("length %v, date %v", it.Duration, it.Published)
	}
	if it.FileName != "tagesschau 20:00 Uhr, 26.11.2026.mp4" {
		t.Errorf("file name is %q, want the title rather than the CDN's name", it.FileName)
	}
	if !strings.Contains(it.Subtitle, "subtitle") {
		t.Errorf("subtitle is %q", it.Subtitle)
	}
	if f.Items[1].Subtitle != "" {
		t.Errorf("a result without subtitles got %q", f.Items[1].Subtitle)
	}
	if f.Title != "tagesschau" {
		t.Errorf("the feed is called %q, want the show", f.Title)
	}
}

func TestAMediathekFileFallsBackToTheNextQualityDown(t *testing.T) {
	r := mediathekResult{URLVideo: "n", URLVideoLow: "l"}
	if got := r.file(QualityHD); got != "n" {
		t.Errorf("hd without an HD file took %q", got)
	}
	if got := r.file(QualityLow); got != "l" {
		t.Errorf("low took %q", got)
	}
	if got := (mediathekResult{URLVideoHD: "h"}).file(QualityLow); got != "h" {
		t.Errorf("a result with only an HD file staged %q for low", got)
	}
	if got := (mediathekResult{URLVideoHD: "h", URLVideo: "n"}).file(QualityNormal); got != "n" {
		t.Errorf("normal took %q", got)
	}
}

func TestAMediathekErrorIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"err":["the search engine is busy"],"result":null}`))
	}))
	defer srv.Close()
	sub := Sanitize([]Subscription{{Kind: KindMediathek, Mediathek: &Mediathek{Topic: "x"}}})[0]
	_, err := queryMediathek(context.Background(), srv.Client(), srv.URL, sub, 50)
	if err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("got %v", err)
	}
}

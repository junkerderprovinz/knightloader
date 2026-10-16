package playback

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/mediahook"
)

const plexAnswer = `{"MediaContainer":{"size":2,"Metadata":[
 {"title":"Pilot","grandparentTitle":"Some Show","User":{"title":"anna"},
  "Player":{"title":"Living room","state":"playing","address":"192.168.1.20","local":true},
  "Session":{"id":"a1","location":"lan"}},
 {"title":"A Film","User":{"title":"ben"},
  "Player":{"product":"Plex Web","state":"paused","address":"203.0.113.9","local":false},
  "Session":{"id":"b2","location":"wan"}}]}}`

const jellyfinAnswer = `[
 {"Id":"s1","UserName":"anna","DeviceName":"TV","RemoteEndPoint":"10.0.0.7",
  "NowPlayingItem":{"Name":"Pilot","SeriesName":"Some Show"},"PlayState":{"IsPaused":false}},
 {"Id":"s2","UserName":"ben","Client":"Jellyfin Web","RemoteEndPoint":"198.51.100.4"},
 {"Id":"s3","UserName":"cleo","RemoteEndPoint":"198.51.100.5",
  "NowPlayingItem":{"Name":"A Film"},"PlayState":{"IsPaused":true}}]`

func serve(t *testing.T, path, body string, check func(*http.Request)) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		check(r)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestPlexSessionsAreReadWithTheTokenInItsHeader(t *testing.T) {
	url := serve(t, "/status/sessions", plexAnswer, func(r *http.Request) {
		if got := r.Header.Get("X-Plex-Token"); got != "tok" {
			t.Errorf("X-Plex-Token = %q", got)
		}
	})
	got, err := Ask(context.Background(), NewClient(), Server{ID: "plex", Kind: KindPlex, URL: url}, "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d streams, want 2", len(got))
	}
	if s := got[0]; s.Title != "Pilot" || s.Series != "Some Show" || s.User != "anna" || !s.Local || s.Paused || s.Server != "plex" {
		t.Errorf("first stream = %+v", s)
	}
	if s := got[1]; s.Player != "Plex Web" || s.Local || !s.Paused {
		t.Errorf("second stream = %+v", s)
	}
}

func TestJellyfinSkipsSessionsWithNothingLoaded(t *testing.T) {
	url := serve(t, "/jf/Sessions", jellyfinAnswer, func(r *http.Request) {
		if got := r.Header.Get("Authorization"); got != `MediaBrowser Token="tok"` {
			t.Errorf("Authorization = %q", got)
		}
	})
	got, err := Ask(context.Background(), NewClient(), Server{ID: "jf", Kind: KindJellyfin, URL: url + "/jf/"}, "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d streams, want 2: %+v", len(got), got)
	}
	if !got[0].Local || got[0].Player != "TV" {
		t.Errorf("first stream = %+v", got[0])
	}
	if got[1].Local || !got[1].Paused {
		t.Errorf("second stream = %+v", got[1])
	}
}

func TestEmbyIsAskedUnderItsPrefix(t *testing.T) {
	url := serve(t, "/emby/Sessions", "[]", func(r *http.Request) {
		if got := r.Header.Get("X-Emby-Token"); got != "tok" {
			t.Errorf("X-Emby-Token = %q", got)
		}
	})
	got, err := Ask(context.Background(), NewClient(), Server{ID: "emby", Kind: KindEmby, URL: url}, "tok")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestARefusedTokenIsAnAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	_, err := Ask(context.Background(), NewClient(), Server{ID: "plex", Kind: KindPlex, URL: srv.URL}, "")
	var f *Failure
	if !errors.As(err, &f) || f.Code != mediahook.CodeAuth {
		t.Fatalf("err = %v, want an auth failure", err)
	}
}

func TestTheWrongKindIsAFormatFailure(t *testing.T) {
	url := serve(t, "/status/sessions", jellyfinAnswer, func(*http.Request) {})
	_, err := Ask(context.Background(), NewClient(), Server{ID: "x", Kind: KindPlex, URL: url}, "")
	var f *Failure
	if !errors.As(err, &f) || f.Code != codeFormat {
		t.Fatalf("err = %v, want a format failure", err)
	}
}

func TestValidateRefusesATokenInTheAddress(t *testing.T) {
	s := Server{ID: "plex", Kind: KindPlex, URL: "http://10.0.0.2:32400/?X-Plex-Token=abc"}
	if err := s.Validate(); err == nil {
		t.Fatal("an address with a query was accepted")
	}
}

func TestFreeIDCountsUpFromTheKind(t *testing.T) {
	taken := []Server{{ID: "plex"}, {ID: "plex-2"}}
	if got := FreeID(KindPlex, taken); got != "plex-3" {
		t.Errorf("FreeID = %q", got)
	}
	if got := FreeID(KindEmby, taken); got != "emby" {
		t.Errorf("FreeID = %q", got)
	}
}

var t0 = time.Date(2026, 10, 16, 20, 0, 0, 0, time.UTC)

var plexOnly = []Server{{ID: "plex", Kind: KindPlex, URL: "http://x"}}

func enabled() Settings {
	s := Defaults()
	s.Enabled = true
	return s
}

func one(s Stream) map[string]answer {
	s.Server = "plex"
	return map[string]answer{"plex": {streams: []Stream{s}}}
}

func idle() map[string]answer { return map[string]answer{"plex": {}} }

func TestAStreamTurnsQuietOnAndItsEndReleasesItLater(t *testing.T) {
	tr := newTracker()
	cfg := enabled()
	if _, on := tr.observe(t0, cfg, plexOnly, one(Stream{Session: "a"})); !on {
		t.Fatal("a playing stream did not count")
	}
	if _, on := tr.observe(t0.Add(10*time.Second), cfg, plexOnly, idle()); !on {
		t.Fatal("quiet mode ended in the gap between two episodes")
	}
	if _, on := tr.observe(t0.Add(10*time.Second+releaseAfter), cfg, plexOnly, idle()); on {
		t.Fatal("quiet mode outlived the release delay")
	}
}

func TestAPausedStreamCountsUntilItsTimeIsUp(t *testing.T) {
	tr := newTracker()
	cfg := enabled()
	cfg.PausedMinutes = 30
	paused := Stream{Session: "a", Paused: true}
	if _, on := tr.observe(t0, cfg, plexOnly, one(paused)); !on {
		t.Fatal("a fresh pause did not count")
	}
	streams, _ := tr.observe(t0.Add(30*time.Minute), cfg, plexOnly, one(paused))
	if streams[0].Counts || streams[0].Reason != "paused" {
		t.Fatalf("a pause of half an hour still counts: %+v", streams[0])
	}
	if streams, _ := tr.observe(t0.Add(31*time.Minute), cfg, plexOnly, one(Stream{Session: "a"})); !streams[0].Counts {
		t.Fatal("a resumed stream did not count again")
	}
}

func TestLocalStreamsCanBeLeftOut(t *testing.T) {
	tr := newTracker()
	cfg := enabled()
	cfg.IgnoreLocal = true
	streams, on := tr.observe(t0, cfg, plexOnly, one(Stream{Session: "a", Local: true}))
	if on || streams[0].Reason != "local" {
		t.Fatalf("a local stream counted: on=%v %+v", on, streams[0])
	}
}

func TestAServerThatStopsAnsweringKeepsItsLastAnswerBriefly(t *testing.T) {
	tr := newTracker()
	cfg := enabled()
	tr.observe(t0, cfg, plexOnly, one(Stream{Session: "a"}))
	down := map[string]answer{"plex": {err: &Failure{Code: mediahook.CodeRefused}}}
	if streams, _ := tr.observe(t0.Add(20*time.Second), cfg, plexOnly, down); len(streams) != 1 {
		t.Fatal("the last answer was dropped at the first failure")
	}
	if streams, _ := tr.observe(t0.Add(errorGrace+time.Second), cfg, plexOnly, down); len(streams) != 0 {
		t.Fatal("the last answer outlived the grace")
	}
}

func TestSwitchingOffReleasesAtOnce(t *testing.T) {
	tr := newTracker()
	cfg := enabled()
	tr.observe(t0, cfg, plexOnly, one(Stream{Session: "a"}))
	cfg.Enabled = false
	if _, on := tr.observe(t0.Add(time.Second), cfg, plexOnly, nil); on {
		t.Fatal("quiet mode held after the feature was switched off")
	}
}

func TestTheWatcherReportsEachFlipOnce(t *testing.T) {
	playing := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if playing {
			_, _ = w.Write([]byte(plexAnswer))
			return
		}
		_, _ = w.Write([]byte(`{"MediaContainer":{"size":0}}`))
	}))
	defer srv.Close()
	cfg := enabled()
	servers := []Server{{ID: "plex", Kind: KindPlex, URL: srv.URL}}
	now := t0
	var flips []bool
	w := NewWatcher(Options{
		Settings: func() Settings { return cfg },
		Servers:  func() []Server { return servers },
		Token:    func(string) string { return "" },
		OnChange: func(on bool) { flips = append(flips, on) },
		Now:      func() time.Time { return now },
	})
	ctx := context.Background()
	w.pass(ctx)
	w.pass(ctx)
	playing = false
	now = now.Add(releaseAfter + time.Second)
	w.pass(ctx)
	if len(flips) != 2 || !flips[0] || flips[1] {
		t.Fatalf("flips = %v, want [true false]", flips)
	}
	if st := w.Status(); st.Active || st.Since != nil || len(st.Servers) != 1 || !st.Servers[0].OK {
		t.Fatalf("status = %+v", st)
	}
}

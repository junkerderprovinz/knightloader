package hostheaders

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The header names the redirect tests watch. "Authorization" is the one
// internal/httpx already strips across an origin change; "X-Forum-Token" is
// the one only this package can strip, because httpx's list is a fixed four
// names and the whole point of a header profile is that the name is whatever
// the user's forum, seedbox or Nextcloud asks for.
const (
	secretToken = "forum-session-2c9f1b7a-do-not-leak"
	secretBasic = "Basic ZGVtbzpzM2NyZXQ="
)

// site is one test server whose URL is known before it starts serving.
//
// httptest.NewServer only publishes its URL after the handler is installed,
// so a handler that has to redirect to its own address (or to the other
// server's) can only be written by assigning a variable the serving goroutine
// then reads - a data race the detector is right to report, even though the
// first request happens long afterwards. NewUnstartedServer allocates the
// listener up front, so the address exists before anything is running and
// every handler here is closed over a value that never changes.
type site struct {
	srv  *httptest.Server
	URL  string
	seen chan http.Header
}

func newSite(t *testing.T) *site {
	t.Helper()
	srv := httptest.NewUnstartedServer(nil)
	s := &site{srv: srv, URL: "http://" + srv.Listener.Addr().String(), seen: make(chan http.Header, 8)}
	t.Cleanup(srv.Close)
	return s
}

// serve installs the handler and starts the server. Every request's headers
// are recorded first, which is what the assertions read.
func (s *site) serve(h http.HandlerFunc) {
	s.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.seen <- r.Header.Clone()
		h(w, r)
	})
	s.srv.Start()
}

// saw returns the headers of the next recorded request, or nil when the server
// was never reached.
func (s *site) saw() http.Header {
	select {
	case h := <-s.seen:
		return h
	default:
		return nil
	}
}

func profileFor(t *testing.T, origin string) Set {
	t.Helper()
	set, err := Normalize(Set{
		Origin: origin,
		Headers: []Header{
			{Name: "X-Forum-Token", Value: secretToken},
			{Name: "Authorization", Value: secretBasic},
		},
	})
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	return set
}

// TestHeadersDoNotFollowARedirectAcrossAnOrigin is the test the whole package
// hangs on.
//
// A forum that redirects its attachment links to a CDN is ordinary, and so is
// an open redirect on a host somebody configured a login for. Either one hands
// the stored credential to a server the user never named, unless the headers
// are deleted on the hop that leaves the origin - Set.checkRedirect in
// redirect.go.
//
// The X-Forum-Token assertion is the one that only this package can satisfy:
// net/http strips nothing here (two httptest servers are both 127.0.0.1, which
// its registered-domain rule reads as the same place) and internal/httpx
// strips Authorization and three other fixed names, none of which is a header
// a user chose.
func TestHeadersDoNotFollowARedirectAcrossAnOrigin(t *testing.T) {
	cdn := newSite(t)
	cdn.serve(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("bytes")) })

	forum := newSite(t)
	forum.serve(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, cdn.URL+"/file.zip", http.StatusFound)
	})

	set := profileFor(t, forum.URL)
	probe, err := set.Preflight(context.Background(), nil, forum.URL+"/attachment/12")
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}

	sent := cdn.saw()
	if sent == nil {
		t.Fatal("the redirect target was never reached, so this test proves nothing")
	}
	if got := sent.Get("X-Forum-Token"); got != "" {
		t.Errorf("the CDN received X-Forum-Token %q; a stored header followed a redirect off its own origin", got)
	}
	if got := sent.Get("Authorization"); got != "" {
		t.Errorf("the CDN received Authorization %q; a stored header followed a redirect off its own origin", got)
	}
	// And the answer handed to the download backend carries nothing either:
	// the chain ended somewhere the profile does not cover.
	if len(probe.Headers) != 0 {
		t.Errorf("Preflight returned %d headers for a URL off the profile's origin, want none", len(probe.Headers))
	}
	if OriginOf(probe.URL) != OriginOf(cdn.URL) {
		t.Errorf("final = %q, want a URL on the redirect target", probe.URL)
	}
}

// TestHeadersSurviveARedirectInsideTheOrigin is the other half. A guard that
// dropped the headers on every hop would be safe and useless: a Nextcloud that
// redirects /s/<token>/download to /remote.php/... is one origin the whole way
// and the credential has to survive it, or the feature never works at all.
func TestHeadersSurviveARedirectInsideTheOrigin(t *testing.T) {
	nc := newSite(t)
	nc.serve(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/s/token/download" {
			http.Redirect(w, r, nc.URL+"/remote.php/file.zip", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("bytes"))
	})

	set := profileFor(t, nc.URL)
	probe, err := set.Preflight(context.Background(), nil, nc.URL+"/s/token/download")
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	_ = nc.saw() // the first hop
	second := nc.saw()
	if second == nil {
		t.Fatal("the second hop was never made, so this test proves nothing")
	}
	if got := second.Get("X-Forum-Token"); got != secretToken {
		t.Errorf("the second hop on the same origin got X-Forum-Token %q, want the stored value", got)
	}
	if probe.Headers["X-Forum-Token"] != secretToken {
		t.Errorf("Preflight returned %d headers, want the stored ones for a final URL on the profile's own origin", len(probe.Headers))
	}
	if probe.URL != nc.URL+"/remote.php/file.zip" {
		t.Errorf("final = %q, want the URL the chain ended at", probe.URL)
	}
}

// TestHeadersComeBackAfterALoopThroughAStranger is the case Set.Attach alone
// cannot catch, and the reason checkRedirect compares against the profile's
// origin rather than against where the chain started or where it ended.
//
// home -> stranger -> home ends on the profile's own origin, so the answer
// handed to the backend is correct however the middle hop was handled. The
// leak is in the middle: the stranger is sent a request, and without the strip
// that request carries the credential.
func TestHeadersComeBackAfterALoopThroughAStranger(t *testing.T) {
	home, bounce := newSite(t), newSite(t)
	home.serve(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, bounce.URL+"/hop", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("bytes"))
	})
	bounce.serve(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, home.URL+"/file.zip", http.StatusFound)
	})

	set := profileFor(t, home.URL)
	probe, err := set.Preflight(context.Background(), nil, home.URL+"/start")
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	middle := bounce.saw()
	if middle == nil {
		t.Fatal("the middle hop was never reached, so this test proves nothing")
	}
	if middle.Get("X-Forum-Token") != "" || middle.Get("Authorization") != "" {
		t.Error("the stranger in the middle of the chain was sent a stored header")
	}
	if probe.Headers["X-Forum-Token"] != secretToken {
		t.Error("the chain came back to the profile's own origin and the headers did not")
	}
	if probe.URL != home.URL+"/file.zip" {
		t.Errorf("final = %q, want the URL the chain ended at", probe.URL)
	}
}

// TestPreflightRefusesAProfileWithNoOrigin pins the one state Attach cannot
// make safe: a set with no origin matches nothing, and a preflight for it
// would be a request with no scope at all.
func TestPreflightRefusesAProfileWithNoOrigin(t *testing.T) {
	var s Set
	if _, err := s.Preflight(context.Background(), nil, "https://example.org/x"); err == nil {
		t.Fatal("Preflight accepted a profile with no origin")
	}
}

// TestTheProbeReadsTheStatusLine, because a 404 that reports "online" puts a
// green dot on a link that is gone, and a 403 that reports "offline" puts a
// dead marker on a link whose only problem is an expired cookie.
func TestTheProbeReadsTheStatusLine(t *testing.T) {
	site := newSite(t)
	site.serve(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gone":
			http.NotFound(w, r)
		case "/refused":
			http.Error(w, "no", http.StatusForbidden)
		default:
			_, _ = w.Write([]byte("bytes"))
		}
	})
	set := profileFor(t, site.URL)
	for path, want := range map[string]int{
		"/file.zip": http.StatusOK,
		"/gone":     http.StatusNotFound,
		"/refused":  http.StatusForbidden,
	} {
		probe, err := set.Preflight(context.Background(), nil, site.URL+path)
		if err != nil {
			t.Fatalf("Preflight(%s): %v", path, err)
		}
		if probe.Status != want {
			t.Errorf("Preflight(%s).Status = %d, want %d", path, probe.Status, want)
		}
	}
}

package hostheaders

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Authorization is also stripped by internal/httpx; X-Forum-Token stands for a
// user-chosen name that only this package knows to strip.
const (
	secretToken = "forum-session-2c9f1b7a-do-not-leak"
	secretBasic = "Basic ZGVtbzpzM2NyZXQ="
)

// site is one test server whose URL is known before it starts serving, so a
// handler can redirect to it without a data race.
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

// serve installs the handler and starts the server, recording every request's
// headers.
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

// Both test servers are on 127.0.0.1, which net/http treats as one domain, so
// only checkRedirect can strip the headers here.
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
	if len(probe.Headers) != 0 {
		t.Errorf("Preflight returned %d headers for a URL off the profile's origin, want none", len(probe.Headers))
	}
	if OriginOf(probe.URL) != OriginOf(cdn.URL) {
		t.Errorf("final = %q, want a URL on the redirect target", probe.URL)
	}
}

// A Nextcloud redirecting /s/<token>/download to /remote.php/... stays on one
// origin, and the credential has to survive that.
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

// A chain home -> stranger -> home ends on the profile's origin, so Attach
// alone would not notice the stranger being sent the credential.
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

func TestPreflightRefusesAProfileWithNoOrigin(t *testing.T) {
	var s Set
	if _, err := s.Preflight(context.Background(), nil, "https://example.org/x"); err == nil {
		t.Fatal("Preflight accepted a profile with no origin")
	}
}

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

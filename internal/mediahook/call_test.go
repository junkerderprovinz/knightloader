package mediahook

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

// answering is a server that hands back one status and remembers what it was
// sent, which is all any test in this file needs to know about the far end.
type answering struct {
	srv     *httptest.Server
	hits    atomic.Int32
	method  atomic.Value // string
	token   atomic.Value // string
	tokenIn atomic.Bool
}

func serverAnswering(t *testing.T, status int) *answering {
	t.Helper()
	a := &answering{}
	a.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.hits.Add(1)
		a.method.Store(r.Method)
		v := r.Header.Get("X-Emby-Token")
		a.token.Store(v)
		a.tokenIn.Store(v != "")
		w.WriteHeader(status)
	}))
	t.Cleanup(a.srv.Close)
	return a
}

func (a *answering) header() string {
	v, _ := a.token.Load().(string)
	return v
}

func hookAt(url, method string) Hook {
	return Hook{ID: "jellyfin", URL: url, Method: method, HeaderName: "X-Emby-Token"}
}

func TestCallReportsWhatCameBack(t *testing.T) {
	for _, c := range []struct {
		status int
		ok     bool
		code   string
	}{
		{http.StatusOK, true, ""},
		{http.StatusNoContent, true, ""},
		{http.StatusUnauthorized, false, CodeAuth},
		{http.StatusForbidden, false, CodeAuth},
		{http.StatusNotFound, false, CodeNotFound},
		{http.StatusMethodNotAllowed, false, CodeMethod},
		{http.StatusInternalServerError, false, CodeServer},
		{http.StatusBadGateway, false, CodeServer},
		// Every other 4xx: no sentence of its own, the status in the raw error.
		{http.StatusTooManyRequests, false, CodeUnknown},
	} {
		srv := serverAnswering(t, c.status)
		res := Call(context.Background(), srv.srv.Client(), hookAt(srv.srv.URL, MethodPost), planted)
		if res.OK != c.ok || res.Code != c.code {
			t.Errorf("status %d gave ok=%v code=%q, want ok=%v code=%q", c.status, res.OK, res.Code, c.ok, c.code)
		}
		if res.Status != c.status {
			t.Errorf("status %d was reported as %d", c.status, res.Status)
		}
		if c.status == http.StatusMethodNotAllowed && res.Params["method"] != MethodPost {
			t.Errorf("the 405 sentence does not name the method that was sent: %v", res.Params)
		}
		if c.status == http.StatusUnauthorized && res.Params["status"] != http.StatusUnauthorized {
			t.Errorf("the 401 sentence does not carry the status: %v", res.Params)
		}
	}
}

func TestCallSendsTheMethodAndTheHeaderItWasGiven(t *testing.T) {
	srv := serverAnswering(t, http.StatusOK)
	if res := Call(context.Background(), srv.srv.Client(), hookAt(srv.srv.URL, MethodPost), planted); !res.OK {
		t.Fatalf("the call failed: %+v", res)
	}
	if got, _ := srv.method.Load().(string); got != MethodPost {
		t.Errorf("the server saw %q, want %q", got, MethodPost)
	}
	if srv.header() != planted {
		t.Errorf("the server saw the header %q, want the stored value", srv.header())
	}
}

// TestNoHeaderIsSentWithoutAValue covers the ordinary home case: a media server
// on a trusted LAN that asks for nothing. An empty header would be a header, and
// some servers refuse one.
func TestNoHeaderIsSentWithoutAValue(t *testing.T) {
	srv := serverAnswering(t, http.StatusOK)
	Call(context.Background(), srv.srv.Client(), hookAt(srv.srv.URL, MethodGet), "")
	if srv.tokenIn.Load() {
		t.Error("a header was sent although nothing is stored for this address")
	}
}

// TestARedirectIsNeverFollowedAndTheTokenNeverTravels is the security test this
// whole file exists for.
//
// httpx's own credential list is Authorization, Proxy-Authorization, Cookie and
// Cookie2, and it strips those across an origin change. X-Emby-Token,
// X-Plex-Token and X-Api-Key are on nobody's list, so a client that followed
// redirects would hand the token to whatever the 302 pointed at - a login page
// on another host in front of a reverse proxy is enough, and httpx follows ten
// hops by default. NewClient's MaxRedirects: -1 is what stops it, and the 3xx is
// reported as the configuration answer it is.
func TestARedirectIsNeverFollowedAndTheTokenNeverTravels(t *testing.T) {
	elsewhere := serverAnswering(t, http.StatusOK)
	var redirects atomic.Int32
	from := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirects.Add(1)
		http.Redirect(w, r, elsewhere.srv.URL+"/login", http.StatusFound)
	}))
	t.Cleanup(from.Close)

	res := Call(context.Background(), NewClient(), hookAt(from.URL, MethodPost), planted)

	if res.Code != CodeRedirect || res.Status != http.StatusFound {
		t.Fatalf("the redirect was not reported as one: %+v", res)
	}
	if res.Params["status"] != http.StatusFound {
		t.Errorf("the redirect sentence does not carry the status: %v", res.Params)
	}
	if redirects.Load() != 1 {
		t.Errorf("the first address was called %d times", redirects.Load())
	}
	if elsewhere.hits.Load() != 0 {
		t.Fatalf("the redirect was followed: the address it pointed at was called %d times", elsewhere.hits.Load())
	}
	if elsewhere.header() != "" {
		t.Fatalf("the token travelled to the address the redirect pointed at")
	}
}

// TestNothingListeningIsToldApartFromEverythingElse: the port typo. It is worth
// its own code because the fix is a port and the fix for the code it would
// otherwise be folded onto (dns) is a name.
func TestNothingListeningIsToldApartFromEverythingElse(t *testing.T) {
	// A listener taken straight back down, so the address is one nothing can be
	// listening on rather than a number picked and hoped for.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	res := Call(context.Background(), NewClient(), hookAt("http://"+addr+"/Library/Refresh", MethodGet), planted)
	if res.OK {
		t.Fatal("a call to a closed port reported success")
	}
	if res.Code != CodeRefused {
		t.Errorf("code = %q (%s), want %q", res.Code, res.Error, CodeRefused)
	}
}

// TestAHostThatDoesNotResolveSaysSo. The address is under .invalid, which RFC
// 2606 reserves precisely so that a lookup for it cannot succeed anywhere.
func TestAHostThatDoesNotResolveSaysSo(t *testing.T) {
	res := Call(context.Background(), NewClient(), hookAt("http://jellyfin.this-name-cannot-exist.invalid:8096/x", MethodGet), planted)
	if res.OK {
		t.Fatal("a call to a name that cannot resolve reported success")
	}
	if res.Code != CodeDNS {
		t.Errorf("code = %q (%s), want %q", res.Code, res.Error, CodeDNS)
	}
}

// TestAServerThatAcceptsAndSaysNothingIsATimeout covers the nastiest far end:
// one that takes the connection and never answers. The client is built with a
// short ceiling so the test does not sit for twenty seconds; what is asserted is
// the classification, which is the same at either ceiling.
func TestAServerThatAcceptsAndSaysNothingIsATimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})
	client := httpx.New(httpx.Options{Timeout: 150 * time.Millisecond, MaxRedirects: -1})
	res := Call(context.Background(), client, hookAt(srv.URL, MethodGet), planted)
	if res.OK {
		t.Fatal("a server that never answered reported success")
	}
	if res.Code != CodeTimeout {
		t.Errorf("code = %q (%s), want %q", res.Code, res.Error, CodeTimeout)
	}
	if res.Params["seconds"] != int(CallTimeout/time.Second) {
		t.Errorf("the timeout sentence does not name the build's own ceiling: %v", res.Params)
	}
}

// TestAnAddressThatIsNotOneIsAnswered rather than crashing: a settings.json
// edited by hand reaches the runner without going past Validate.
func TestAnAddressThatIsNotOneIsAnswered(t *testing.T) {
	res := Call(context.Background(), NewClient(), Hook{ID: "x", URL: "://nonsense", Method: MethodGet}, planted)
	if res.OK || res.Code != CodeUnknown {
		t.Fatalf("an unusable address gave %+v", res)
	}
	// And the address itself is not quoted back: a pasted Plex address carries
	// its token in the query, and this sentence reaches the log ring.
	if res.Error == "" {
		t.Error("nothing at all was reported")
	}
}

// TestTheDurationIsAlwaysReported. It is the one number on the card that says
// "the far end is slow" rather than "the far end is broken", and a zero there
// would read as an instant answer.
func TestTheDurationIsAlwaysReported(t *testing.T) {
	srv := serverAnswering(t, http.StatusOK)
	res := Call(context.Background(), srv.srv.Client(), hookAt(srv.srv.URL, MethodGet), "")
	if res.DurationMS < 0 {
		t.Errorf("durationMs = %d", res.DurationMS)
	}
	if res.At.IsZero() {
		t.Error("the result carries no time")
	}
}

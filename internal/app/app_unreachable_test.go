package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	jdresolver "github.com/junkerderprovinz/knightloader/internal/resolver/jd"
)

// unreachableSidecar stands in for a JD that has a plugin for ddownload.com
// and for gone.example.com, whose site no longer answers.
func unreachableSidecar(t *testing.T) *App {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/accounts/listPremiumHoster" {
			_, _ = w.Write([]byte(`{"data":["ddownload.com","gone.example.com"]}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("KL_JD", srv.URL)
	prev := unreachableHosts
	unreachableHosts = map[string]bool{"gone.example.com": true}
	t.Cleanup(func() { unreachableHosts = prev })
	return newQueueApp(t)
}

func offered(a *App) map[string]bool {
	out := map[string]bool{}
	for _, h := range a.HosterHosts(context.Background()) {
		out[h.ID] = true
	}
	return out
}

func TestThePickerHidesAnUnreachableHoster(t *testing.T) {
	a := unreachableSidecar(t)
	got := offered(a)
	if got["gone.example.com"] {
		t.Error("the picker offers a hoster whose site does not answer")
	}
	if !got["ddownload.com"] {
		t.Error("the picker lost a hoster that answers")
	}
}

func TestALinkToAnUnreachableHosterStillGoesToJD(t *testing.T) {
	a := unreachableSidecar(t)
	if _, err := a.hosterAuth().Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !jdresolver.FileHoster("gone.example.com") {
		t.Error("a link to an unreachable hoster is no longer JD's to fetch")
	}
}

func TestAnUnreachableHosterThatAnswersAgainReappears(t *testing.T) {
	a := unreachableSidecar(t)
	prev := hostReachability
	hostReachability = func(context.Context, string) string { return "" }
	t.Cleanup(func() { hostReachability = prev })

	a.recheckUnreachable(context.Background())
	if !offered(a)["gone.example.com"] {
		t.Error("a hoster that answers again is still hidden")
	}
}

func TestAParkingOrForSalePageIsNotTheHoster(t *testing.T) {
	for _, c := range []struct {
		final string
		body  string
		want  bool
	}{
		{"https://www.hugedomains.com/domain_profile.cfm?d=example", "", true},
		{"https://example.org/", "<h1>This domain is for sale!</h1>", true},
		{"https://example.org/", `<script src="//www.parkingcrew.net/assets/park.js"></script>`, true},
		{"https://example.org/", "<h1>Upload your files</h1><p>Premium accounts for sale</p>", false},
	} {
		final, _ := url.Parse(c.final)
		if got := parkedPage(final, []byte(c.body)); got != c.want {
			t.Errorf("parkedPage(%s, %q) = %v, want %v", c.final, c.body, got, c.want)
		}
	}
}

func TestReachabilityNamesWhyASiteIsUnreachable(t *testing.T) {
	allowLocalIcons(t)
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer live.Close()
	forSale := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<title>Buy this domain</title>"))
	}))
	defer forSale.Close()
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()

	ctx := context.Background()
	if got := answers(ctx, gone.URL, live.URL); got != "" {
		t.Errorf("a site that answers, even with a 403, counts as unreachable: %q", got)
	}
	if got := answers(ctx, forSale.URL); !strings.HasPrefix(got, UnreachableParked) {
		t.Errorf("a page that sells the domain = %q, want %q", got, UnreachableParked)
	}
	if got := answers(ctx, gone.URL); got != UnreachableNoAnswer {
		t.Errorf("a site that refuses every connection = %q, want %q", got, UnreachableNoAnswer)
	}
	if got := Reachability(ctx, "no-such-host.invalid"); got != UnreachableNoDNS {
		t.Errorf("a name without an address = %q, want %q", got, UnreachableNoDNS)
	}
}

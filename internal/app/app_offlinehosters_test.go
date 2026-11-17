package app

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	jdresolver "github.com/junkerderprovinz/knightloader/internal/resolver/jd"
)

// offlineSidecar stands in for a JD that has a plugin for ddownload.com
// and for gone.example.com, whose site does not answer.
func offlineSidecar(t *testing.T) *App {
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
	prev := offlineHosters
	offlineHosters = map[string]bool{"gone.example.com": true}
	t.Cleanup(func() { offlineHosters = prev })
	return newQueueApp(t)
}

func offered(a *App) map[string]bool {
	out := map[string]bool{}
	for _, h := range a.HosterHosts(context.Background()) {
		out[h.ID] = true
	}
	return out
}

func TestThePickerHidesAnOfflineHoster(t *testing.T) {
	a := offlineSidecar(t)
	got := offered(a)
	if got["gone.example.com"] {
		t.Error("the picker offers a hoster whose site does not answer")
	}
	if !got["ddownload.com"] {
		t.Error("the picker lost a hoster that answers")
	}
}

func TestALinkToAnOfflineHosterStillGoesToJD(t *testing.T) {
	a := offlineSidecar(t)
	if _, err := a.hosterAuth().Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !jdresolver.FileHoster("gone.example.com") {
		t.Error("a link to an offline hoster is no longer JD's to fetch")
	}
}

func TestAnOfflineHosterThatAnswersAgainReappears(t *testing.T) {
	a := offlineSidecar(t)
	prev := hostOfflineReason
	hostOfflineReason = func(context.Context, string) string { return "" }
	t.Cleanup(func() { hostOfflineReason = prev })

	a.recheckOfflineHosters(context.Background())
	if !offered(a)["gone.example.com"] {
		t.Error("a hoster that answers again is still hidden")
	}
}

func TestAnOfflineHosterWhoseDomainIsParkedStaysHidden(t *testing.T) {
	a := offlineSidecar(t)
	fakeSites(t, map[string]http.HandlerFunc{
		"gone.example.com": page(200, "<h1>This domain is for sale!</h1>"),
	})
	prev := hostOfflineReason
	hostOfflineReason = func(ctx context.Context, host string) string { return answers(ctx, "http://"+host) }
	t.Cleanup(func() { hostOfflineReason = prev })

	a.recheckOfflineHosters(context.Background())
	if offered(a)["gone.example.com"] {
		t.Error("a hoster whose domain is now for sale is back in the picker")
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

// fakeSites serves each named site from one httptest server: the icon client
// dials it for every name, and the Host header picks the handler.
func fakeSites(t *testing.T, sites map[string]http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := strings.Cut(r.Host, ":")
		if h, ok := sites[host]; ok {
			h(w, r)
			return
		}
		http.Error(w, "no such site", http.StatusMisdirectedRequest)
	}))
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().String()
	var d net.Dialer
	prev := iconClient
	iconClient = &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return d.DialContext(ctx, network, addr)
		},
	}}
	t.Cleanup(func() { iconClient = prev })
}

func page(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func redirect(to string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, to, http.StatusFound)
	}
}

// hosterFront stands for a real hoster's front page, long enough not to look
// empty.
const hosterFront = `<!doctype html><html><head><title>Upload your files</title></head>
<body><h1>Fast file hosting</h1><p>Share files up to 10 GB, premium accounts for faster downloads.</p></body></html>`

func TestOfflineReasonTellsAHosterFromWhatAnswersInItsPlace(t *testing.T) {
	fakeSites(t, map[string]http.HandlerFunc{
		"live.example":         page(200, hosterFront),
		"challenge.example":    page(403, `<!DOCTYPE html><html><head><title>Just a moment...</title></head><body><script src="/cdn-cgi/challenge-platform/h/b/orchestrate/chl_page/v1"></script></body></html>`),
		"underattack.example":  page(503, `<html><head><title>Just a moment...</title></head><body><div id="cf-chl-widget"></div></body></html>`),
		"incapsula.example":    page(200, `<html><head><META NAME="robots" CONTENT="noindex,nofollow"><script src="/_Incapsula_Resource?SWJIYLWA=5074a744"></script><body></body></html>`),
		"members.example":      page(401, ""),
		"rg.to":                redirect("http://rapidgator.net/"),
		"rapidgator.net":       page(200, hosterFront),
		"vidspeed.example":     redirect("http://vidspeeds.example/"),
		"vidspeeds.example":    page(200, hosterFront),
		"www-only.example":     redirect("http://www.www-only.example/"),
		"www.www-only.example": page(200, hosterFront),
		"moved.example":        redirect("http://unrelated.test/"),
		"unrelated.test":       page(200, hosterFront),
		"bounce.example":       page(200, `<html><head><script>location.replace("https://elsewhere.test" + location.pathname);</script></head><body></body></html>`),
		"forsale.example":      redirect("http://www.hugedomains.com/domain_profile.cfm?d=forsale.example"),
		"www.hugedomains.com":  page(200, hosterFront),
		"lander.example":       page(200, `<!DOCTYPE html><html><head><script>window.onload=function(){window.location.href="/lander"}</script></head></html>`),
		"ww.example":           redirect("http://ww17.ww.example/"),
		"ww17.ww.example":      page(200, hosterFront),
		"nginx.example":        page(200, `<!DOCTYPE html><html><head><title>Welcome to nginx!</title></head><body><h1>Welcome to nginx!</h1><p>If you see this page, the nginx web server is successfully installed.</p></body></html>`),
		"suspended.example":    page(200, `<html><head><title>Account Suspended</title></head><body><h1>This Account has been suspended.</h1><p>Contact your hosting provider for more information.</p></body></html>`),
		"expired.example":      page(200, `<html><head><title>expired.example</title></head><body><h1>This domain has expired.</h1><p>If you are the owner, renew it with your registrar to bring the site back.</p></body></html>`),
		"origin.example":       page(523, `<!DOCTYPE html><html><head><title>origin.example | 523: Origin is unreachable</title></head><body>Cloudflare</body></html>`),
		"badgateway.example":   page(502, `<html><head><title>502 Bad Gateway</title></head><body><center><h1>502 Bad Gateway</h1></center></body></html>`),
		"gone.example":         page(404, `<html><head><title>404 Not Found</title></head><body><center><h1>404 Not Found</h1></center><hr><center>nginx</center></body></html>`),
		"closed.example":       page(410, `<html><head><title>Fermeture définitive</title></head><body><p>C'est la fin d'une aventure.</p></body></html>`),
		"empty.example":        page(200, "  \n"),
		"stock404.example":     page(200, `<html><head><title>404 Not Found</title></head><body><center><h1>404 Not Found</h1></center><hr><center>nginx</center></body></html>`),
		"depositfiles.com":     redirect("http://dfiles.com/"),
		"dfiles.com":           page(200, hosterFront),
	})
	ctx := context.Background()
	for _, c := range []struct {
		host, want string
	}{
		{"live.example", ""},
		{"challenge.example", ""},
		{"underattack.example", ""},
		{"incapsula.example", ""},
		{"members.example", ""},
		{"rg.to", ""},
		{"vidspeed.example", ""},
		{"www-only.example", ""},
		{"moved.example", OfflineElsewhere + " (unrelated.test)"},
		{"bounce.example", OfflineElsewhere + " (elsewhere.test)"},
		{"forsale.example", OfflineParked + " (www.hugedomains.com)"},
		{"lander.example", OfflineParked},
		{"ww.example", OfflineParked + " (ww17.ww.example)"},
		{"nginx.example", OfflineHostingPage},
		{"suspended.example", OfflineHostingPage},
		{"expired.example", OfflineExpired},
		{"origin.example", OfflineServerDown + " (HTTP 523)"},
		{"badgateway.example", OfflineServerDown + " (HTTP 502)"},
		{"gone.example", OfflineGone + " (HTTP 404)"},
		{"closed.example", OfflineGone + " (HTTP 410)"},
		{"empty.example", OfflineEmpty},
		{"stock404.example", OfflineHostingPage},
		{"depositfiles.com", ""},
	} {
		if got := answers(ctx, "http://"+c.host); got != c.want {
			t.Errorf("%s = %q, want %q", c.host, got, c.want)
		}
	}
}

func TestOfflineReasonNamesWhyASiteIsOffline(t *testing.T) {
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
		t.Errorf("a site that answers, even with a 403, counts as offline: %q", got)
	}
	if got := answers(ctx, forSale.URL); !strings.HasPrefix(got, OfflineParked) {
		t.Errorf("a page that sells the domain = %q, want %q", got, OfflineParked)
	}
	if got := answers(ctx, gone.URL); got != OfflineNoAnswer {
		t.Errorf("a site that refuses every connection = %q, want %q", got, OfflineNoAnswer)
	}
	if got := OfflineReason(ctx, "no-such-host.invalid"); got != OfflineNoDNS {
		t.Errorf("a name without an address = %q, want %q", got, OfflineNoDNS)
	}
}

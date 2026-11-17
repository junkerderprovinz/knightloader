package app

// Offline hosters are hosters JD knows whose site does not answer. The
// picker leaves them out, since a login for a hoster that is gone helps
// nobody, but links to them still go to JD. The list ships with the binary
// (scripts/hostericons writes it), and a weekly check brings a hoster back
// into the picker once its site answers again.

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"

	hostericons "github.com/junkerderprovinz/knightloader/assets/hoster-icons"
	"github.com/junkerderprovinz/knightloader/internal/hostalias"
)

// offlineHosters are the hosters the shipped list names, by bare domain.
var offlineHosters = parseOfflineHosters(hostericons.Offline)

// parseOfflineHosters reads the shipped list: one host per line, then a tab
// and the reason.
func parseOfflineHosters(list string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(list, "\n") {
		if host, _, _ := strings.Cut(strings.TrimSpace(line), "\t"); host != "" {
			out[host] = true
		}
	}
	return out
}

// The first check waits a few minutes after start, out of the way of
// everything a start does, and then runs once a week.
const (
	offlineFirstCheck = 5 * time.Minute
	offlineEvery      = 7 * 24 * time.Hour
)

// StartOfflineHosterCheck starts the weekly check of the offline hosters.
// The binaries call it rather than New, which every test calls. Close stops
// it.
func (a *App) StartOfflineHosterCheck() {
	a.spawn(func() {
		a.tickLoop(offlineFirstCheck, offlineEvery, func() { a.recheckOfflineHosters(a.ctx) })
	})
}

// hostOfflineReason is OfflineReason, a variable so tests can stand in for the
// network.
var hostOfflineReason = OfflineReason

// recheckOfflineHosters asks every offline hoster's site once, a few at a
// time, and keeps which of them answer.
func (a *App) recheckOfflineHosters(ctx context.Context) {
	slots := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for host := range offlineHosters {
		wg.Go(func() {
			slots <- struct{}{}
			back := hostOfflineReason(ctx, host) == ""
			<-slots
			a.offlineMu.Lock()
			if a.answering == nil {
				a.answering = map[string]bool{}
			}
			was := a.answering[host]
			a.answering[host] = back
			a.offlineMu.Unlock()
			if back && !was {
				log.Printf("hosters: %s answers again and is back in the account picker", host)
			}
		})
	}
	wg.Wait()
}

// hiddenOffline reports whether the picker leaves host out.
func (a *App) hiddenOffline(host string) bool {
	if !offlineHosters[host] {
		return false
	}
	a.offlineMu.Lock()
	defer a.offlineMu.Unlock()
	return !a.answering[host]
}

// offlineHosterState is embedded in App: which offline hosters the last
// check found answering.
type offlineHosterState struct {
	offlineMu sync.Mutex
	answering map[string]bool
}

// The reasons OfflineReason gives for a site that is offline.
const (
	OfflineNoDNS    = "no DNS entry"
	OfflineNoAnswer = "no answer within 10 s"
	OfflineParked   = "parked or for sale"

	OfflineElsewhere   = "sends to another site"
	OfflineHostingPage = "web host's default or suspended page"
	OfflineExpired     = "domain expired"
	OfflineServerDown  = "server behind the proxy is down"
	OfflineGone        = "front page gone"
	OfflineEmpty       = "empty page"
)

// reachTimeout is how long one address of a site gets to answer.
const reachTimeout = 10 * time.Second

// OfflineReason says why host's site is offline, or "" when it answers as the
// hoster. A challenge or login page counts as the hoster, since a browser
// meets it there too, but a page that only takes the hoster's place does not:
// a parked or expired domain, a web host's placeholder, a proxy whose server
// is gone, or a redirect to some other site.
func OfflineReason(ctx context.Context, host string) string {
	if _, err := net.DefaultResolver.LookupHost(ctx, host); err != nil {
		return OfflineNoDNS
	}
	return answers(ctx, "https://"+host, "http://"+host)
}

// answers asks each origin's front page in turn and judges the first that
// answers.
func answers(ctx context.Context, origins ...string) string {
	for _, origin := range origins {
		from, err := url.Parse(origin)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(ctx, reachTimeout)
		resp, err := iconGet(ctx, origin+"/", "text/html,application/xhtml+xml")
		if err != nil {
			cancel()
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, iconHTMLMaxBytes))
		resp.Body.Close()
		cancel()
		return frontPage(from.Hostname(), resp, body)
	}
	return OfflineNoAnswer
}

// frontPage judges the page a request for asked's front page ended at. A
// redirect to another site says more than the challenge waiting there, and a
// challenge page carries scripts that would read as a redirect, hence the
// order.
func frontPage(asked string, resp *http.Response, body []byte) string {
	final := resp.Request.URL.Hostname()
	if parkedPage(resp.Request.URL, body) {
		if final != asked {
			return OfflineParked + " (" + final + ")"
		}
		return OfflineParked
	}
	if final != asked && !sameHoster(asked, final) {
		return OfflineElsewhere + " (" + final + ")"
	}
	if resp.StatusCode == http.StatusUnauthorized || challenge.Match(body) {
		return ""
	}
	switch code := resp.StatusCode; {
	case code == http.StatusBadGateway, code == http.StatusGatewayTimeout, code >= 520 && code <= 530:
		return fmt.Sprintf("%s (HTTP %d)", OfflineServerDown, code)
	case code == http.StatusNotFound, code == http.StatusGone:
		return fmt.Sprintf("%s (HTTP %d)", OfflineGone, code)
	}
	if to := bounceTarget(body); to != "" && !sameHoster(asked, to) {
		return OfflineElsewhere + " (" + to + ")"
	}
	if slices.ContainsFunc(headings(body), hostingPage.MatchString) {
		return OfflineHostingPage
	}
	if expired.Match(body) {
		return OfflineExpired
	}
	if resp.StatusCode < 300 && len(bytes.TrimSpace(body)) < emptyBelow {
		return OfflineEmpty
	}
	return ""
}

// emptyBelow is the size under which a page has no room for a site, where an
// empty document already takes 13 bytes of tags.
const emptyBelow = 64

// forSale finds what a parking or domain sale page says or loads. GoDaddy's
// parked domains send the browser on to /lander with a script.
var forSale = regexp.MustCompile(`(?i)\b(?:this domain (?:name )?(?:is|may be) for sale|buy this domain|domain (?:name )?(?:is )?for sale|parkingcrew|sedoparking|bodis\.com|dan\.com|afternic|hugedomains)|location\.href\s*=\s*["']/lander|//parking\.[a-z0-9-]+\.[a-z]+|lander\.parity\.domains|\bparked (?:domain|free)|domain is parked`)

// parkingSubdomain is the wwNN. host that parking services move a domain's
// visitors to.
var parkingSubdomain = regexp.MustCompile(`^ww\d+\.`)

// parkedPage reports whether a front page that ended up at final is a
// parking service's or offers the domain for sale.
func parkedPage(final *url.URL, body []byte) bool {
	return parkedHost(final.Hostname()) || parkingSubdomain.MatchString(final.Hostname()) || forSale.Match(body)
}

// challenge finds the bot checks that stand in front of a live site:
// Cloudflare, Imperva, DDoS-Guard, Sucuri and Anubis.
var challenge = regexp.MustCompile(`(?i)challenge-platform|cf[-_]chl|<title>\s*just a moment\.\.\.\s*</title>|attention required! \| cloudflare|_incapsula_resource|ddos-guard|sucuri_cloudproxy|making sure you(?:'|&#39;|’)re not a bot|checking your browser`)

// hostingPage matches the title or main heading of a web server's or web
// host's page for a site that is not set up or has been switched off. Only
// those two are read, as a hoster's help pages may well mention a suspended
// account.
var hostingPage = regexp.MustCompile(`(?i)welcome to nginx|apache2? \w+ default page|test page for (?:the )?apache|^it works!?$|^iis windows server$|^welcome to (?:centos|openresty)|default web ?site page|web server's default page|domain default page|account (?:has been )?suspended|(?:site|website|domain|page) (?:has been |is )?(?:suspended|deactivated|disabled)|future home of something quite cool|^site not available$|^index of /|^coming soon!?$|^under construction!?$|^404 not found$`)

var expired = regexp.MustCompile(`(?i)\b(?:domain (?:name )?(?:has |is )?expired|expired domain)\b`)

var (
	headingTag = regexp.MustCompile(`(?is)<(?:title|h1)\b[^>]*>(.*?)</(?:title|h1)>`)
	anyTag     = regexp.MustCompile(`<[^>]*>`)
)

// headings returns the text of the page's title and h1 elements.
func headings(body []byte) []string {
	var out []string
	for _, m := range headingTag.FindAllSubmatch(body, 8) {
		text := html.UnescapeString(anyTag.ReplaceAllString(string(m[1]), " "))
		out = append(out, strings.Join(strings.Fields(text), " "))
	}
	return out
}

// bounce finds a script or meta refresh that sends the browser to an
// absolute address.
var bounce = regexp.MustCompile(`(?i)(?:location(?:\.href)?\s*=|location\.(?:replace|assign)\s*\(|http-equiv=["']?refresh["']?[^>]*url=)\s*["']?(https?://[^"'\s;)>/]+)`)

// bounceMaxBytes keeps bounceTarget to pages that are nothing but the
// redirect, as a full page may send a button's click anywhere.
const bounceMaxBytes = 4 << 10

// bounceTarget returns the host a small page sends the browser on to, or "".
func bounceTarget(body []byte) string {
	if len(body) > bounceMaxBytes {
		return ""
	}
	m := bounce.FindSubmatch(body)
	if m == nil {
		return ""
	}
	u, err := url.Parse(string(m[1]))
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// knownMoves are hosters whose front page sends to a name that neither
// hostalias nor the name itself ties to them but that is still the hoster or
// its owner, from looking at the sites.
var knownMoves = map[string]string{
	"depositfiles.com":   "dfiles.com",
	"massengeschmack.tv": "orangeflix.de",
	"upload-4ever.com":   "up-4ever.net",
	"workers.dev":        "cloudflare.com",
}

// sameHoster reports whether final is still the hoster asked for: the same
// registered domain, one of its JDownloader aliases, a known move, or the same
// name under another ending, which is how hosters tend to move (vidspeed.org
// to vidspeeds.org, dood.re to doodstream.com).
func sameHoster(asked, final string) bool {
	a, f := registrable(asked), registrable(final)
	if a == f || hostalias.Canonical(a) == hostalias.Canonical(f) || knownMoves[a] == f {
		return true
	}
	an, fn := siteName(a), siteName(f)
	if len(an) > len(fn) {
		an, fn = fn, an
	}
	return len(an) >= 4 && (strings.HasPrefix(fn, an) || strings.HasSuffix(fn, an))
}

// registrable returns the domain host was registered under, or host itself
// where that is a public suffix such as workers.dev.
func registrable(host string) string {
	host = strings.ToLower(host)
	if d, err := publicsuffix.EffectiveTLDPlusOne(host); err == nil {
		return d
	}
	return host
}

// siteName returns a registered domain without its ending, digits or
// hyphens, so up-4ever and up4ever compare alike.
func siteName(domain string) string {
	suffix, _ := publicsuffix.PublicSuffix(domain)
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r
		}
		return -1
	}, strings.TrimSuffix(domain, "."+suffix))
}

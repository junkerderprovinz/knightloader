package app

// Offline hosters are hosters JD knows whose site no longer answers. The
// picker leaves them out, since a login for a hoster that is gone helps
// nobody, but links to them still go to JD. The list ships with the binary
// (scripts/hostericons writes it), and a weekly check brings a hoster back
// into the picker once its site answers again.

import (
	"context"
	"io"
	"log"
	"net"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	hostericons "github.com/junkerderprovinz/knightloader/assets/hoster-icons"
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
)

// reachTimeout is how long one address of a site gets to answer.
const reachTimeout = 10 * time.Second

// OfflineReason says why host's site is offline, or "" when it answers.
// Any HTTP status is an answer, since a hoster behind a challenge page is
// still there, but a parking or for-sale page is not the hoster.
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
		ctx, cancel := context.WithTimeout(ctx, reachTimeout)
		resp, err := iconGet(ctx, origin+"/", "text/html,application/xhtml+xml")
		if err != nil {
			cancel()
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, iconHTMLMaxBytes))
		resp.Body.Close()
		cancel()
		if !parkedPage(resp.Request.URL, body) {
			return ""
		}
		if from, err := url.Parse(origin); err == nil && from.Hostname() != resp.Request.URL.Hostname() {
			return OfflineParked + " (" + resp.Request.URL.Hostname() + ")"
		}
		return OfflineParked
	}
	return OfflineNoAnswer
}

// forSale finds what a parking or domain sale page says or loads.
var forSale = regexp.MustCompile(`(?i)\b(?:this domain (?:name )?(?:is|may be) for sale|buy this domain|domain (?:name )?(?:is )?for sale|parkingcrew|sedoparking|bodis\.com|dan\.com|afternic|hugedomains)`)

// parkedPage reports whether a front page that ended up at final is a
// parking service's or offers the domain for sale.
func parkedPage(final *url.URL, body []byte) bool {
	return parkedHost(final.Hostname()) || forSale.Match(body)
}

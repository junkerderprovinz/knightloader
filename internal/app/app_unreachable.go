package app

// Unreachable hosters are hosters JD knows whose site no longer answers. The
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

// unreachableHosts are the hosters the shipped list names, by bare domain.
var unreachableHosts = parseUnreachable(hostericons.Unreachable)

// parseUnreachable reads the shipped list: one host per line, then a tab and
// the reason.
func parseUnreachable(list string) map[string]bool {
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
	unreachableFirstCheck = 5 * time.Minute
	unreachableEvery      = 7 * 24 * time.Hour
)

// StartUnreachableCheck starts the weekly check of the unreachable hosters.
// The binaries call it rather than New, which every test calls. Close stops
// it.
func (a *App) StartUnreachableCheck() {
	a.spawn(func() {
		a.tickLoop(unreachableFirstCheck, unreachableEvery, func() { a.recheckUnreachable(a.ctx) })
	})
}

// hostReachability is Reachability, a variable so tests can stand in for the
// network.
var hostReachability = Reachability

// recheckUnreachable asks every unreachable hoster's site once, a few at a
// time, and keeps which of them answer.
func (a *App) recheckUnreachable(ctx context.Context) {
	slots := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for host := range unreachableHosts {
		wg.Go(func() {
			slots <- struct{}{}
			back := hostReachability(ctx, host) == ""
			<-slots
			a.unreachableMu.Lock()
			if a.answering == nil {
				a.answering = map[string]bool{}
			}
			was := a.answering[host]
			a.answering[host] = back
			a.unreachableMu.Unlock()
			if back && !was {
				log.Printf("hosters: %s answers again and is back in the account picker", host)
			}
		})
	}
	wg.Wait()
}

// hiddenUnreachable reports whether the picker leaves host out.
func (a *App) hiddenUnreachable(host string) bool {
	if !unreachableHosts[host] {
		return false
	}
	a.unreachableMu.Lock()
	defer a.unreachableMu.Unlock()
	return !a.answering[host]
}

// unreachableState is embedded in App: which unreachable hosters the last
// check found answering.
type unreachableState struct {
	unreachableMu sync.Mutex
	answering     map[string]bool
}

// The reasons Reachability gives for an unreachable site.
const (
	UnreachableNoDNS    = "no DNS entry"
	UnreachableNoAnswer = "no answer within 10 s"
	UnreachableParked   = "parked or for sale"
)

// reachTimeout is how long one address of a site gets to answer.
const reachTimeout = 10 * time.Second

// Reachability says why host's site is unreachable, or "" when it answers.
// Any HTTP status is an answer, since a hoster behind a challenge page is
// still there, but a parking or for-sale page is not the hoster.
func Reachability(ctx context.Context, host string) string {
	if _, err := net.DefaultResolver.LookupHost(ctx, host); err != nil {
		return UnreachableNoDNS
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
			return UnreachableParked + " (" + resp.Request.URL.Hostname() + ")"
		}
		return UnreachableParked
	}
	return UnreachableNoAnswer
}

// forSale finds what a parking or domain sale page says or loads.
var forSale = regexp.MustCompile(`(?i)\b(?:this domain (?:name )?(?:is|may be) for sale|buy this domain|domain (?:name )?(?:is )?for sale|parkingcrew|sedoparking|bodis\.com|dan\.com|afternic|hugedomains)`)

// parkedPage reports whether a front page that ended up at final is a
// parking service's or offers the domain for sale.
func parkedPage(final *url.URL, body []byte) bool {
	return parkedHost(final.Hostname()) || forSale.Match(body)
}

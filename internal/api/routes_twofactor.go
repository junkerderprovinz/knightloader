package api

// The second factor's own routes, and the throttle that makes six digits worth
// asking for.
//
// The enrolment is three steps and they are three routes, because the middle
// one is the only one that changes anything: begin hands out a candidate secret
// and arms nothing, confirm accepts a code and arms the factor, disable takes it
// off and costs the same proof as using it. internal/auth/twofactor.go holds the
// argument behind each of those; this file is the door.

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/auth"
)

// The login throttle. A password is long and a bcrypt comparison is slow, so
// guessing one over HTTP was never the cheap attack. A six-digit code is a
// million possibilities checked by an HMAC, which a script can try as fast as
// the network allows, so an unthrottled second factor is a shorter password.
//
// It sits in front of the whole login route rather than the code half alone:
// counting only code failures would leave the password half as a way to keep
// the connection warm, and an attacker who picks which half to fail picks the
// uncounted one.
//
// The passkey sign-in keeps a counter of its own; routes_passkeys.go says why
// sharing this one would let an attack on the password take the owner's
// working passkey away with it.
const (
	// loginFailLimit failures within loginFailWindow are allowed per client.
	// Five a minute is 7200 a day against a million codes, so a code guessed
	// in time takes months, while somebody who mistypes waits at most a
	// minute. A hard lock would hand an attacker a way to keep the operator
	// out, which on this app is the same damage as getting in.
	loginFailLimit  = 5
	loginFailWindow = time.Minute
	// loginNetworkLimit failures within loginFailWindow are allowed per IPv6
	// /48 as a whole. Its /64s count one by one as well, and whoever holds a
	// /48 holds 65536 of them, each with a fresh allowance otherwise.
	loginNetworkLimit = 4 * loginFailLimit
	// loginGateMax bounds the map. This route answers without a session, so the
	// number of keys is chosen by whoever calls it.
	loginGateMax = 512
)

type loginFails struct {
	// at holds the times of the failures within the window, oldest first.
	at []time.Time
}

type loginGate struct {
	mu    sync.Mutex
	fails map[string]*loginFails
	// trusted lists the proxies whose X-Forwarded-For counts; nil trusts none.
	trusted func() []netip.Prefix
	now     func() time.Time
}

func newLoginGate(trusted func() []netip.Prefix) *loginGate {
	return &loginGate{fails: map[string]*loginFails{}, trusted: trusted, now: time.Now}
}

// trustedProxies is the setting of that name, read on every request so a
// change applies at once.
func trustedProxies(a *app.App) func() []netip.Prefix {
	return func() []netip.Prefix { return a.Settings.Get().TrustedProxyPrefixes() }
}

// key is who r counts as for the throttle.
func (g *loginGate) key(r *http.Request) string {
	var trusted []netip.Prefix
	if g.trusted != nil {
		trusted = g.trusted()
	}
	return loginClientKey(r, trusted)
}

// loginClientKey is the caller's bucket (see bucketOf).
//
// Behind a reverse proxy every caller arrives as the proxy, so the whole
// instance shares one bucket unless the proxy is in trusted. Then the caller
// is the right-most address in X-Forwarded-For that is not itself a trusted
// proxy: each proxy appends the address it was reached from, so everything
// to the right of that one was written by a proxy of ours and everything to
// the left by whoever sent the request, who could name a new bucket per
// attempt. A direct caller's header is never read, for the same reason.
func loginClientKey(r *http.Request, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	if !inPrefixes(trusted, peer) {
		return bucketOf(peer)
	}
	var hops []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(v, ",")...)
	}
	for i := len(hops) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(hops[i])
		addr, err := netip.ParseAddr(hop)
		if err != nil {
			// Some proxies add the caller's port, which changes with every
			// connection and would make every attempt a bucket of its own.
			ap, perr := netip.ParseAddrPort(hop)
			if perr != nil {
				return hop
			}
			addr = ap.Addr()
		}
		if !inPrefixes(trusted, addr) {
			return bucketOf(addr)
		}
	}
	// Proxies of ours all the way: the left-most is as far as the trail goes.
	if len(hops) > 0 {
		return strings.TrimSpace(hops[0])
	}
	return host
}

// bucketOf is the key for a caller at a: the address itself for IPv4, and its
// /64 for IPv6, the least a single connection is given, so that one guesser
// cannot take a fresh bucket from every address of their own network.
func bucketOf(a netip.Addr) string {
	a = a.Unmap()
	if a.Is4() {
		return a.String()
	}
	p, _ := a.WithZone("").Prefix(64)
	return p.String()
}

func inPrefixes(prefixes []netip.Prefix, a netip.Addr) bool {
	a = a.Unmap()
	for _, p := range prefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// networkOf is the key the IPv6 /48 around bucket key counts under, and ""
// for an IPv4 caller.
func networkOf(key string) string {
	p, err := netip.ParsePrefix(key)
	if err != nil || !p.Addr().Is6() {
		return ""
	}
	wide, _ := p.Addr().Prefix(48)
	return "net " + wide.String()
}

// try takes one attempt from the caller's allowance, and from its network's,
// and reports whether there was one left in both. The attempt counts as a failure from the start and pass wipes
// it on success: deciding and counting in one step under the lock is what
// stops a burst of parallel requests from all passing the check before the
// first of them has been counted.
//
// A caller who has used up the window is refused even with the right answer;
// letting one through would tell an attacker when a guess was right.
func (g *loginGate) try(r *http.Request) bool {
	key := g.key(r)
	now := g.now()
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sweep(now)
	limits := map[string]int{key: loginFailLimit}
	if network := networkOf(key); network != "" {
		limits[network] = loginNetworkLimit
	}
	for k, most := range limits {
		if f := g.fails[k]; f != nil {
			if f.forget(now); len(f.at) >= most {
				return false
			}
		}
	}
	for k := range limits {
		f := g.fails[k]
		if f == nil {
			f = &loginFails{}
			g.fails[k] = f
		}
		f.at = append(f.at, now)
	}
	return true
}

// forget drops the failures that have left the window.
func (f *loginFails) forget(now time.Time) {
	keep := 0
	for keep < len(f.at) && now.Sub(f.at[keep]) >= loginFailWindow {
		keep++
	}
	f.at = f.at[keep:]
}

// release gives back an attempt try took for a request that turned out not to
// be a guess, such as the right password arriving without the code it still
// needs.
func (g *loginGate) release(r *http.Request) {
	key := g.key(r)
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, k := range []string{key, networkOf(key)} {
		if f := g.fails[k]; f != nil && len(f.at) > 0 {
			f.at = f.at[:len(f.at)-1]
		}
	}
}

// pass clears the record for a caller who got in, so a day of typos does not
// meet somebody a week later.
func (g *loginGate) pass(r *http.Request) {
	key := g.key(r)
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.fails, key)
}

// sweep drops records with no failure left in the window, and the one with
// the oldest last failure if the map is still full after that. Called with
// the lock held.
func (g *loginGate) sweep(now time.Time) {
	for k, f := range g.fails {
		if f.forget(now); len(f.at) == 0 {
			delete(g.fails, k)
		}
	}
	for len(g.fails) >= loginGateMax {
		oldestKey, oldest := "", time.Time{}
		for k, f := range g.fails {
			if last := f.at[len(f.at)-1]; oldest.IsZero() || last.Before(oldest) {
				oldestKey, oldest = k, last
			}
		}
		delete(g.fails, oldestKey)
	}
}

// twoFactorLabels is what an authenticator app shows above the code. The
// account half is the instance's own name when it has one, so somebody running
// two KnightLoaders sees two distinguishable entries.
func twoFactorLabels(a *app.App) (issuer, account string) {
	account = strings.TrimSpace(a.Settings.Get().InstanceName)
	if account == "" {
		account = "this instance"
	}
	return "KnightLoader", account
}

func registerTwoFactor(reg *Registry, a *app.App) {
	reg.Add(http.MethodPost, "/api/auth/2fa/begin",
		"start a second-factor enrolment: a fresh secret and the otpauth URI for it. Arms nothing",
		func(w http.ResponseWriter, r *http.Request) {
			issuer, account := twoFactorLabels(a)
			sec, uri, err := a.Auth.BeginTwoFactor(issuer, account)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// The secret leaves the instance here, once, to the session that
			// asked for it. Everything downstream shows it and forgets it.
			//
			// The QR grid is computed here rather than in the browser (see
			// QRMatrix) so the grid a phone scans and the string beside it for
			// a phone that cannot travel together and cannot disagree.
			writeJSON(w, map[string]any{"secret": sec, "uri": uri, "qr": renderQR(uri)})
		})

	reg.Add(http.MethodPost, "/api/auth/2fa/confirm",
		"confirm the enrolment with a code from the app; answers with the recovery codes, once",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Code string `json:"code"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			var codes []string
			err := a.Auth.Mint(stillAdmitted(a, r), func() (err error) {
				codes, err = a.Auth.ConfirmTwoFactor(body.Code)
				return err
			})
			if err != nil {
				status := http.StatusBadRequest
				if errors.Is(err, auth.ErrCodeRejected) || errors.Is(err, auth.ErrStale) {
					status = http.StatusUnauthorized
				}
				http.Error(w, err.Error(), status)
				return
			}
			// They are stored hashed, so this response is the one chance to
			// read them. The screen that shows them says so beforehand and
			// asks for an acknowledgement rather than a dismissal.
			writeJSON(w, map[string]any{"recoveryCodes": codes})
		})

	reg.Add(http.MethodPost, "/api/auth/2fa/disable",
		"turn the second factor off; costs the same proof as using it - a live code or a recovery code",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Code string `json:"code"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			if err := a.Auth.DisableTwoFactor(body.Code); err != nil {
				status := http.StatusBadRequest
				if errors.Is(err, auth.ErrCodeRejected) {
					status = http.StatusUnauthorized
				}
				http.Error(w, err.Error(), status)
				return
			}
			writeJSON(w, map[string]any{"twoFactor": false})
		})
}

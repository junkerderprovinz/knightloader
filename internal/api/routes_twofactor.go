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
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/auth"
)

// The login throttle.
//
// WHY IT HAD TO ARRIVE WITH THIS FEATURE. A password is long and a bcrypt
// comparison is slow, so guessing one over HTTP was never the cheap attack. A
// six-digit code is a million possibilities and the check is an HMAC, which a
// script can try as fast as the network allows - so a second factor with no
// throttle in front of it is a shorter password, not a second factor.
//
// It sits in front of the WHOLE login route rather than only the code half. One
// door, one counter: a throttle that only counted code failures would leave the
// password half as the way to keep the connection warm, and an attacker who can
// choose which half to fail can choose the uncounted one.
//
// The passkey sign-in has a counter of its own rather than this one, and
// routes_passkeys.go says why - the short version is that sharing would let an
// attack on the password take the owner's working passkey away with it.
const (
	// loginFailBurst is how many failures are free. Enough that somebody
	// mistyping a code twice and then reaching for the recovery sheet never
	// meets this; few enough that the guessing rate below is what it is.
	loginFailBurst = 8
	// loginCoolOff is how long the door stays shut once the burst is used up.
	// Eight tries per half-minute is under a thousand an hour against a million
	// codes, which is decades - while a locked-out owner waits half a minute.
	// That asymmetry is the whole design: a hard lock would hand an attacker a
	// way to keep the operator out, which on this app is the same damage as
	// getting in.
	loginCoolOff = 30 * time.Second
	// loginGateMax bounds the map. This route answers without a session, so the
	// number of keys is chosen by whoever calls it.
	loginGateMax = 512
)

type loginFails struct {
	count int
	until time.Time
	seen  time.Time
}

type loginGate struct {
	mu    sync.Mutex
	fails map[string]*loginFails
}

func newLoginGate() *loginGate { return &loginGate{fails: map[string]*loginFails{}} }

// loginClientKey is the caller's address without its port.
//
// Behind a reverse proxy every caller arrives as the proxy, so the whole
// instance shares one bucket. That is deliberately not fixed by trusting
// X-Forwarded-For: a header the caller writes is a header the caller chooses,
// so honouring it would turn the throttle into a formality - a script rotating
// one header value per attempt would never meet it. One shared bucket is a real
// limit; a per-attacker bucket that the attacker names is none.
func loginClientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// blocked reports whether this caller is inside a cool-off. It refuses the RIGHT
// answer too, which is the only way it can be a throttle rather than an oracle
// telling an attacker when a guess was close.
func (g *loginGate) blocked(r *http.Request) bool {
	key := loginClientKey(r)
	g.mu.Lock()
	defer g.mu.Unlock()
	f := g.fails[key]
	return f != nil && time.Now().Before(f.until)
}

func (g *loginGate) fail(r *http.Request) {
	key := loginClientKey(r)
	now := time.Now()
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sweep(now)
	f := g.fails[key]
	if f == nil {
		f = &loginFails{}
		g.fails[key] = f
	}
	f.seen = now
	f.count++
	if f.count >= loginFailBurst {
		f.until = now.Add(loginCoolOff)
		// The counter is not reset here. Each further failure inside the
		// cool-off pushes the window out again, so a script that keeps hammering
		// keeps the door shut on itself.
		f.count = loginFailBurst
	}
}

// pass clears the record for a caller who got in, so a day of typos does not
// meet somebody a week later.
func (g *loginGate) pass(r *http.Request) {
	key := loginClientKey(r)
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.fails, key)
}

// sweep drops records nothing has touched for a while, and - if the map is
// still full after that - the oldest one. Called with the lock held.
func (g *loginGate) sweep(now time.Time) {
	for k, f := range g.fails {
		if now.Sub(f.seen) > 10*time.Minute && now.After(f.until) {
			delete(g.fails, k)
		}
	}
	for len(g.fails) >= loginGateMax {
		oldestKey, oldest := "", time.Time{}
		for k, f := range g.fails {
			if oldest.IsZero() || f.seen.Before(oldest) {
				oldestKey, oldest = k, f.seen
			}
		}
		delete(g.fails, oldestKey)
	}
}

// twoFactorIssuer is what an authenticator app shows above the code. The account
// half is the instance's own name when it has one, so somebody running two
// KnightLoaders sees two distinguishable entries rather than two identical ones.
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
			// The secret leaves the instance exactly here, once, to the session
			// that asked for it. Everything downstream shows it and forgets it.
			//
			// The QR grid is computed here rather than in the browser for the
			// reason QRMatrix's own doc comment gives: a second render path is
			// a second thing that can fall out of step with the response it
			// belongs to. A secret is offered in every form a device can take
			// it - the grid for a phone that can scan, the string beside it for
			// one that cannot - so both travel together and cannot disagree.
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
			codes, err := a.Auth.ConfirmTwoFactor(body.Code)
			if err != nil {
				status := http.StatusBadRequest
				if errors.Is(err, auth.ErrCodeRejected) {
					status = http.StatusUnauthorized
				}
				http.Error(w, err.Error(), status)
				return
			}
			// The only time these exist outside the operator's own notes. They
			// are stored hashed, so this response is genuinely the one chance -
			// which is why the screen that shows them says so beforehand and
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

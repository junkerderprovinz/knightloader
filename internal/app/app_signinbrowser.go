package app

// The sign-in browser (internal/signinbrowser): a headless Chromium-family
// browser that the user signs in to a site with from KnightLoader's own window,
// the image's Chromium in the container and an installed browser on the
// desktop. What it reads of the site's cookies is stored like the extension's
// hand-over, through storeSiteSession, which also takes back a Remove.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/signinbrowser"
	"github.com/junkerderprovinz/knightloader/internal/sitelogin"
)

// SourceSignInBrowser is the Source of a session the sign-in browser stored.
const SourceSignInBrowser = "kl-browser"

// ErrNoSignInBrowser is a sign-in browser asked for where there is none.
var ErrNoSignInBrowser = errors.New("there is no Chromium-family browser here to sign in with")

// signInBrowserBinary is the browser the sign-in window runs: KL_CHROMIUM
// where it is set, as the image does, and otherwise a Chromium-family browser
// installed on this computer.
func signInBrowserBinary() (signinbrowser.Browser, bool) {
	if bin := strings.TrimSpace(os.Getenv("KL_CHROMIUM")); bin != "" {
		return signinbrowser.Browser{Name: "Chromium", Path: bin}, true
	}
	return signinbrowser.Installed()
}

var (
	signInBrowserMu    sync.Mutex
	signInBrowserByApp = map[*App]*signinbrowser.Manager{}
)

func (a *App) signInBrowserDir() string { return filepath.Join(a.DataDir, "signinbrowser") }

// signInBrowserProfile is the profile folder for one browser: a profile one
// browser wrote may not open in another, or in an older one.
func (a *App) signInBrowserProfile(b signinbrowser.Browser) string {
	return filepath.Join(a.signInBrowserDir(), "profile-"+strings.ReplaceAll(strings.ToLower(b.Name), " ", "-"))
}

// SignInBrowser is the app's sign-in browser, made on first use.
func (a *App) SignInBrowser() *signinbrowser.Manager {
	signInBrowserMu.Lock()
	defer signInBrowserMu.Unlock()
	m, ok := signInBrowserByApp[a]
	if !ok {
		b, _ := signInBrowserBinary()
		m = signinbrowser.NewManager(a.ctx, signinbrowser.Config{
			Binary:    b.Path,
			Profile:   a.signInBrowserProfile(b),
			NoSandbox: buildinfo.InContainer(),
			Session:   a.signInBrowserSession,
		})
		signInBrowserByApp[a] = m
	}
	return m
}

// SignInBrowserAvailable reports whether a browser is there to sign in with.
func (a *App) SignInBrowserAvailable() bool { return a.SignInBrowser().Available() }

// SignInBrowserName names the browser the sign-in window runs.
func (a *App) SignInBrowserName() string {
	b, _ := signInBrowserBinary()
	return b.Name
}

// OpenSignInBrowser opens the sign-in browser on a site's login page.
func (a *App) OpenSignInBrowser(ctx context.Context, id string, width, height int, scale float64) (SiteLoginState, error) {
	site, ok := sitelogin.Lookup(id)
	if !ok {
		return SiteLoginState{}, ErrNoSuchSite
	}
	if !a.SignInBrowserAvailable() {
		return SiteLoginState{}, ErrNoSignInBrowser
	}
	if err := a.SignInBrowser().Open(ctx, site, width, height, scale); err != nil {
		return SiteLoginState{}, err
	}
	return a.siteLoginState(site, false), nil
}

// CloseSignInBrowser closes the window, storing the session it holds, and
// says how its site stands. ok is false when no window was open.
func (a *App) CloseSignInBrowser(ctx context.Context) (SiteLoginState, bool) {
	id := a.SignInBrowser().Close(ctx)
	site, found := sitelogin.Lookup(id)
	if !found {
		return SiteLoginState{}, false
	}
	return a.siteLoginState(site, false), true
}

// signInBrowserSession stores what the sign-in browser holds of a site's
// session, or forgets a session it stored once the user signed out in it.
func (a *App) signInBrowserSession(site sitelogin.Site, jar string) any {
	if jar == "" {
		if a.fromSignInBrowser(site.ID, a.siteStore().Session(site.ID)) {
			if err := a.forgetHandedOver(site.ID); err != nil {
				log.Printf("sign-in browser: could not forget the %s session: %v", site.Name, err)
			}
		}
		return a.siteLoginState(site, false)
	}
	st, err := a.storeSiteSession(site.ID, jar)
	if err != nil {
		log.Printf("sign-in browser: %s did not take the session: %v", site.Name, err)
		return a.siteLoginState(site, false)
	}
	a.noteSignInBrowserJar(site.ID, jar)
	st.Source = SourceSignInBrowser
	return st
}

// The sessions the sign-in browser stored are known by the hash of their
// jar, so a session stored any other way since reads as not its own.
func (a *App) signInBrowserJars() map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile(filepath.Join(a.signInBrowserDir(), "sessions.json"))
	if err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func jarHash(jar string) string {
	sum := sha256.Sum256([]byte(jar))
	return hex.EncodeToString(sum[:])
}

func (a *App) noteSignInBrowserJar(id, jar string) {
	signInBrowserMu.Lock()
	defer signInBrowserMu.Unlock()
	jars := a.signInBrowserJars()
	jars[id] = jarHash(jar)
	b, _ := json.Marshal(jars)
	err := os.MkdirAll(a.signInBrowserDir(), 0o700)
	if err == nil {
		err = os.WriteFile(filepath.Join(a.signInBrowserDir(), "sessions.json"), b, 0o600)
	}
	if err != nil {
		log.Printf("sign-in browser: could not note where the %s session came from: %v", id, err)
	}
}

// fromSignInBrowser reports whether jar is the session the sign-in browser
// stored for a site.
func (a *App) fromSignInBrowser(id, jar string) bool {
	if jar == "" {
		return false
	}
	signInBrowserMu.Lock()
	defer signInBrowserMu.Unlock()
	return a.signInBrowserJars()[id] == jarHash(jar)
}

// liveSignInBrowserJar is the session an open sign-in browser holds for a
// site whose stored session it made, which is fresher while the site keeps
// renewing its cookies in the window.
func (a *App) liveSignInBrowserJar(site sitelogin.Site, stored string) (string, bool) {
	signInBrowserMu.Lock()
	m, ok := signInBrowserByApp[a]
	signInBrowserMu.Unlock()
	if !ok || m.OpenSite() != site.ID || !a.fromSignInBrowser(site.ID, stored) {
		return "", false
	}
	ctx, cancel := context.WithTimeout(a.ctx, siteCheckTimeout)
	defer cancel()
	jar, ok := m.Jar(ctx, site.ID)
	return jar, ok && sitelogin.HoldsSignIn(site, jar, time.Now())
}

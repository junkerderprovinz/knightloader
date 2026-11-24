package api

// The kit's sign-in in front of KnightLoader's routes: who a request comes
// from, the rights a token of the kit carries here, and what of
// KnightLoader's own ends when the kit ends its ways in.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/junkerderprovinz/knightloader/internal/access"
	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	kitfortress "github.com/junkerderprovinz/parleyport/kit/fortress"
	"github.com/junkerderprovinz/parleyport/kit/signin"
)

// errStale refuses a change for a caller whose own way in ended while the
// request was under way.
var errStale = errors.New("this session has ended, sign in again")

// closed reports whether anybody has to sign in, by a password or a sign-in
// service.
func closed(a *app.App) bool { return !a.SignIn.Anyone() }

// caller works out who is asking: ok is false for nobody, and tok is set when
// an API token is what let the request in, since only a token is narrowed by
// scopes. A member of the group already holds the phrase that reaches every
// instance in it, so a second credential would protect nothing, and guard
// holds such calls to groupRoutes.
func caller(a *app.App, r *http.Request) (tok *apitoken.Token, ok bool) {
	if fromGroup(r) {
		return nil, true
	}
	if v, ok := signin.From(r.Context()); ok {
		if v.Via != signin.ViaToken {
			return nil, true
		}
		t := kitToken(v, bearerToken(r))
		return &t, true
	}
	// An MCP client's token, which KnightLoader keeps itself.
	if secret := bearerToken(r); secret != "" {
		return mcpToken(a, secret)
	}
	return nil, false
}

// kitToken is the token of the kit a request came in with, as the scope
// checks read it. Its id is a digest of the secret, which tells two tokens
// apart without keeping either.
func kitToken(v signin.Visitor, secret string) apitoken.Token {
	sum := sha256.Sum256([]byte("kit-token:" + secret))
	return apitoken.Token{ID: hex.EncodeToString(sum[:8]), Name: v.Name, Scopes: apitoken.Granted(v.Scopes)}
}

// mcpToken checks secret against the MCP clients' tokens.
func mcpToken(a *app.App, secret string) (*apitoken.Token, bool) {
	t, ok := a.APITokens.Check(secret)
	if !ok {
		return nil, false
	}
	if _, mcp := a.MCPClients.ByToken(t.ID); !mcp {
		return nil, false
	}
	return &t, true
}

// anyToken checks secret against every API token, the kit's and the MCP
// clients', whether or not the app is open.
func anyToken(a *app.App, secret string) (apitoken.Token, bool) {
	if secret == "" {
		return apitoken.Token{}, false
	}
	if v, ok := a.SignIn.TokenVisitor(context.Background(), secret); ok {
		return kitToken(v, secret), true
	}
	if t, ok := mcpToken(a, secret); ok {
		return *t, true
	}
	return apitoken.Token{}, false
}

// allTokens lists the rights of every API token, the kit's and the MCP
// clients'.
func allTokens(a *app.App) []apitoken.Token {
	var out []apitoken.Token
	if kit, err := a.Store.SignInTokenScopes(); err == nil {
		for _, s := range kit {
			out = append(out, apitoken.Token{Scopes: apitoken.Granted(s)})
		}
	}
	for _, t := range a.APITokens.List() {
		if _, mcp := a.MCPClients.ByToken(t.ID); mcp {
			out = append(out, t)
		}
	}
	return out
}

// tokenSecrets keeps, in memory only, the secret behind a token id a play
// link was made for, so the link can tell whether the token still counts.
var tokenSecrets sync.Map

func rememberTokenSecret(id, secret string) {
	if secret != "" {
		tokenSecrets.Store(id, secret)
	}
}

// tokenIDCounts reports whether the token caller handed out as id still
// lets its holder in.
func tokenIDCounts(a *app.App, id string) bool {
	if secret, ok := tokenSecrets.Load(id); ok {
		_, ok := anyToken(a, secret.(string))
		return ok
	}
	return slices.ContainsFunc(a.APITokens.List(), func(t apitoken.Token) bool {
		_, mcp := a.MCPClients.ByToken(t.ID)
		return mcp && t.ID == id
	})
}

// signInEpoch counts how often the owner's sessions all ended, for what has
// to end with them.
func signInEpoch(a *app.App) uint64 {
	acc, err := a.SignIn.Account(context.Background(), access.Owner)
	if err != nil {
		return 0
	}
	return uint64(acc.Epoch)
}

// stillAdmitted reports whether the way in that let r in still would: a
// reset or a sign-out may have ended it while the request was under way.
func stillAdmitted(a *app.App, r *http.Request) bool {
	if fromGroup(r) {
		return true
	}
	v, ok := signin.From(r.Context())
	switch {
	case !ok:
		_, ok := mcpToken(a, bearerToken(r))
		return ok
	case v.Via == signin.ViaAnyone:
		return a.SignIn.Anyone()
	case v.Via == signin.ViaToken:
		_, ok := anyToken(a, bearerToken(r))
		return ok
	default:
		return a.SignIn.Alive(r.Context(), v.Session())
	}
}

// admitted runs fn while the way in that let r in still counts, under the
// lock what ends KnightLoader's own ways in takes.
func admitted(a *app.App, r *http.Request, fn func() error) error {
	return whileStill(a, func() bool { return stillAdmitted(a, r) }, fn)
}

// whileStill runs fn while valid holds, under the lock what ends
// KnightLoader's own ways in takes, for a change another member of the group
// asked for.
func whileStill(a *app.App, valid func() bool, fn func() error) error {
	a.Ending.RLock()
	defer a.Ending.RUnlock()
	if !valid() {
		return errStale
	}
	return fn()
}

// lasting runs fn, the making of a new lasting way in such as joining a
// pairing group or the fortress, only for a visitor who signed in moments
// ago. It answers the request itself and reports false when fn did not run
// because of that.
func lasting(a *app.App, w http.ResponseWriter, r *http.Request, fn func() error) (bool, error) {
	if fromGroup(r) {
		return true, admitted(a, r, fn)
	}
	err := a.SignIn.Mint(r, func() error {
		a.Ending.RLock()
		defer a.Ending.RUnlock()
		return fn()
	})
	if errors.Is(err, signin.ErrNotFresh) {
		a.SignIn.SignInAgain(w, r)
		return false, nil
	}
	return true, err
}

// freshSignIn reports whether the visitor of r may see a phrase: signed in
// moments ago or just now with the password typed again, since a session may
// be open on a screen nobody watches. It answers r with the kit's question
// otherwise.
func freshSignIn(a *app.App, w http.ResponseWriter, r *http.Request, password string) bool {
	if password != "" && closed(a) {
		if _, err := a.SignIn.CheckPassword(r, "phrase", "", password); err == nil {
			return true
		}
	}
	ran, err := lasting(a, w, r, func() error { return nil })
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	return ran
}

// signInEnded ends KnightLoader's own ways in after the kit ended its own.
// A reset ends everything somebody else may hold: the MCP clients' tokens,
// the place in the pairing group, whose phrase opens the group's routes
// without a password, and every live stream.
func signInEnded(a *app.App) func(context.Context, signin.Ending) error {
	return func(_ context.Context, what signin.Ending) error {
		a.Ending.Lock()
		defer a.Ending.Unlock()
		switch what {
		case signin.EndReset, signin.EndFirstPassword, signin.EndRevoked:
			// A token an MCP client got while the app was open would
			// outlive the password that now closes it.
			for _, c := range a.MCPClients.List() {
				if err := a.APITokens.Revoke(c.TokenID); err != nil && !errors.Is(err, apitoken.ErrNotFound) {
					return err
				}
				if _, err := a.MCPClients.Remove(c.ID); err != nil {
					return err
				}
			}
		}
		if what == signin.EndReset {
			if _, err := renewGroup(a); err != nil {
				log.Printf("signin: after the reset, leaving the pairing group: %v", err)
			}
		}
		// A live stream was let in when it opened and would go on delivering
		// to the sessions that have just ended.
		a.Hub.DropAll()
		return nil
	}
}

// signInRoutes serves the kit's contract and its browser routes under
// /api/signin/ and /signin/, and everything else through next. The kit
// knows the base path from KL_BASE_PATH and wants it in front of its paths,
// which withBasePath has taken off.
func signInRoutes(a *app.App, next http.Handler) http.Handler {
	kit := a.SignIn.Handler()
	base := strings.TrimRight(buildinfo.BasePath, "/")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if !strings.HasPrefix(p, "/api/signin/") && !strings.HasPrefix(p, "/signin/") {
			next.ServeHTTP(w, r)
			return
		}
		// A member holds the phrase and no account here.
		if fromGroup(r) {
			http.NotFound(w, r)
			return
		}
		if signInShutInTunnel(a, r) {
			refuseTunnel(w)
			return
		}
		r = r.Clone(r.Context())
		r.URL.Path, r.URL.RawPath = base+p, ""
		// A page opened through the fortress sits in a sandboxed frame,
		// whose browser calls every request cross-site. The fortress
		// already bound the tab to the opener's session, so here it counts
		// as this site.
		if _, opened := kitfortress.Opened(r); opened {
			r.Header.Set("Sec-Fetch-Site", "same-origin")
			r.Header.Del("Origin")
		}
		kit.ServeHTTP(w, r)
	})
}

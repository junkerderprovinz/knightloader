// Package access opens the kit's sign-in for KnightLoader: one owner, the
// token levels KnightLoader's presets had, the documents in the app's own
// database, and on the first start the sign-in the versions before kept in
// auth.json, tokens.json and the passkeys table.
package access

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/appkey"
	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/internal/mcpclient"
	"github.com/junkerderprovinz/knightloader/internal/store"
	"github.com/junkerderprovinz/parleyport/kit/signin"
	"github.com/junkerderprovinz/parleyport/kit/signin/sqlstore"
)

// Owner is the id of the one account, the one the old password moves to.
const Owner = "owner"

// KeyFile holds the key the sign-in seals and MACs with, sealed with the app
// key where there is one.
const KeyFile = "signin.key"

const keyPurpose = "signin.key"

// Options is what Open needs from the app.
type Options struct {
	Dir    string
	Store  *store.Store
	AppKey *appkey.Key
	// Tokens is tokens.json, which keeps the MCP clients' tokens and, until
	// the kit ends one of the old ways in, the tokens the kit took over.
	Tokens *apitoken.Store
	MCP    *mcpclient.Store
	// Public is the address people reach KnightLoader at, nil while it has
	// none.
	Public func() *url.URL
	Base   string
	// Proxies are the trusted proxies, read on every request.
	Proxies func() []netip.Prefix
	// Ended ends KnightLoader's own ways in after the kit ended its own.
	Ended  func(ctx context.Context, what signin.Ending) error
	Logger *slog.Logger
}

// Open opens the sign-in.
func Open(ctx context.Context, o Options) (*signin.Auth, error) {
	// Sealed in place, the old file still opens in the version before.
	if err := auth.SealIn(o.Dir, o.AppKey); err != nil {
		return nil, err
	}
	key, err := sealKey(o.Dir, o.AppKey)
	if err != nil {
		return nil, err
	}
	seal, err := signin.NewSealer(key)
	if err != nil {
		return nil, err
	}
	docs, err := sqlstore.New(ctx, o.Store.DB(), store.SignInTable)
	if err != nil {
		return nil, err
	}
	old := sync.OnceValues(func() (*auth.Old, error) { return auth.Read(o.Dir, o.AppKey) })
	cfg := signin.Config{
		Domain:      "kl",
		Name:        "KnightLoader",
		Store:       docs,
		Seal:        seal,
		Owner:       true,
		Scopes:      apitoken.KitScopes,
		TokenPrefix: "kl",
		Public:      o.Public,
		Base:        o.Base,
		TrustedPeer: func(r *http.Request) bool { return trusted(o.Proxies, r) },
		Import:      func(context.Context) ([]signin.Imported, error) { return importOld(o, old) },
		Legacy:      legacy(o, old),
		Hooks: signin.Hooks{
			Ended: func(ctx context.Context, _ string, what signin.Ending) error {
				if o.Ended == nil {
					return nil
				}
				return o.Ended(ctx, what)
			},
		},
		Logger: o.Logger,
	}
	if o.Proxies != nil {
		cfg.Proxies = o.Proxies()
	}
	return signin.Open(cfg)
}

// trusted reports whether the peer of r is one of the proxies.
func trusted(proxies func() []netip.Prefix, r *http.Request) bool {
	if proxies == nil {
		return false
	}
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, p := range proxies() {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// importOld hands the kit the sign-in of the versions before: the password,
// the second factor, the passkeys and every token that is not an MCP
// client's, all on the one owner.
func importOld(o Options, old func() (*auth.Old, error)) ([]signin.Imported, error) {
	prev, err := old()
	if err != nil {
		return nil, err
	}
	var tokens []signin.ImportedToken
	for _, t := range o.Tokens.WithDigests() {
		// Instances added by address got one of these, and nothing pairs that way any more.
		if mcpToken(o.MCP, t.ID) || strings.HasPrefix(t.Name, "peer: ") {
			continue
		}
		tokens = append(tokens, signin.ImportedToken{Name: t.Name, Scopes: apitoken.Level(t.Scopes), Digest: t.Digest, Created: t.CreatedAt})
	}
	keys, err := o.Store.ListPasskeys()
	if err != nil {
		return nil, err
	}
	if prev.Hash() == "" && len(tokens) == 0 && len(keys) == 0 {
		return nil, nil
	}
	totp, err := prev.TwoFactor()
	if err != nil {
		return nil, fmt.Errorf("the second factor in %s: %w", auth.FileName, err)
	}
	owner := signin.Imported{
		ID: Owner, Name: "admin", Role: "admin", Owner: true,
		Password: prev.Hash(), TOTP: totp, Recovery: prev.Recovery(),
		Handle: prev.PasskeyHandle(), Tokens: tokens,
	}
	for _, p := range keys {
		var transports []string
		if p.Transports != "" {
			transports = strings.Split(p.Transports, ",")
		}
		used := time.Time{}
		if p.LastUsedAt > 0 {
			used = time.Unix(p.LastUsedAt, 0)
		}
		owner.Passkeys = append(owner.Passkeys, signin.ImportedPasskey{
			Name: p.Name, ID: p.CredentialID, PublicKey: p.PublicKey, SignCount: p.SignCount, AAGUID: p.AAGUID,
			Transports: transports, BackupEligible: p.BackedUp, BackupState: p.BackedUp, AttestationType: "none",
			RPID: p.RPID, Created: time.Unix(p.CreatedAt, 0), Used: used,
		})
	}
	return []signin.Imported{owner}, nil
}

func legacy(o Options, old func() (*auth.Old, error)) signin.Legacy {
	return signin.Legacy{
		Password: func(_ context.Context, hash, pw string) (bool, error) {
			prev, err := old()
			if err != nil {
				return false, err
			}
			return prev.CheckPassword(hash, pw), nil
		},
		Session: func(r *http.Request) (signin.OldSession, bool) {
			c, err := r.Cookie(auth.CookieName)
			if err != nil {
				return signin.OldSession{}, false
			}
			prev, err := old()
			if err != nil {
				return signin.OldSession{}, false
			}
			expires, key, ok := prev.Session(c.Value, time.Now())
			if !ok {
				return signin.OldSession{}, false
			}
			// A session lasted SessionTTL from when it was issued and was never renewed.
			issued := expires.Add(-auth.SessionTTL)
			return signin.OldSession{
				Account: Owner, Key: key, Issued: issued, SignedAt: issued, Expires: expires, Via: signin.ViaPassword,
			}, true
		},
		Recovery: func(hash, code string) bool {
			prev, err := old()
			return err == nil && prev.RecoveryMatches(hash, code)
		},
		Token: func(token string) (string, bool) {
			if !strings.HasPrefix(token, "kl_") {
				return "", false
			}
			return apitoken.Digest(token), true
		},
		Drop: func(context.Context, string, signin.Ending) error {
			return dropOld(o, old)
		},
	}
}

// dropOld removes what the versions before would still sign in with: the
// password and its sessions, the tokens the kit took over and the passkeys.
func dropOld(o Options, old func() (*auth.Old, error)) error {
	var errs []error
	if prev, err := old(); err != nil {
		errs = append(errs, err)
	} else {
		errs = append(errs, prev.Remove())
	}
	for _, t := range o.Tokens.List() {
		if !mcpToken(o.MCP, t.ID) {
			if err := o.Tokens.Revoke(t.ID); err != nil && !errors.Is(err, apitoken.ErrNotFound) {
				errs = append(errs, err)
			}
		}
	}
	if _, err := o.Store.DeleteAllPasskeys(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func mcpToken(mcp *mcpclient.Store, id string) bool {
	if mcp == nil {
		return false
	}
	_, ok := mcp.ByToken(id)
	return ok
}

// sealKey reads KeyFile, or makes it on the first start. With an app key it
// is sealed, and a file written before the key is sealed on the spot.
func sealKey(dir string, k *appkey.Key) ([]byte, error) {
	path := filepath.Join(dir, KeyFile)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		key := make([]byte, 32)
		rand.Read(key)
		return key, writeKey(path, key, k)
	}
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(string(b))
	sealed := appkey.IsSealed(text)
	if sealed {
		if text, err = k.OpenString(keyPurpose, text); err != nil {
			return nil, fmt.Errorf("%s does not open: %w", path, err)
		}
	}
	key, err := hex.DecodeString(text)
	if err != nil || len(key) < 32 {
		return nil, fmt.Errorf("%s is damaged", path)
	}
	if !sealed && k != nil {
		return key, writeKey(path, key, k)
	}
	return key, nil
}

func writeKey(path string, key []byte, k *appkey.Key) error {
	text := hex.EncodeToString(key)
	if k != nil {
		var err error
		if text, err = k.SealString(keyPurpose, text); err != nil {
			return err
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(text+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadKey is the key in KeyFile, for a backup, which carries it in the clear.
// It is nil before the sign-in first started.
func ReadKey(dir string, k *appkey.Key) ([]byte, error) {
	if _, err := os.Stat(filepath.Join(dir, KeyFile)); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return sealKey(dir, k)
}

// NeedsSealing reports whether dir holds a KeyFile that a start with an app
// key would seal for the first time.
func NeedsSealing(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, KeyFile))
	return err == nil && !appkey.IsSealed(strings.TrimSpace(string(b)))
}

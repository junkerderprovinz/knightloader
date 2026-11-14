// Package browsercookies reads the cookies of given sites straight from the
// browsers installed on this machine, so a site the user is signed in to in
// the browser is signed in to here as well, with nothing exported or pasted.
//
// Firefox and the browsers built on it keep their cookies in plain text.
// Chromium and its offspring encrypt them with a key of the user's own: DPAPI
// on Windows, the Keychain on macOS, the keyring or a fixed password on Linux.
// Chrome on Windows seals them with a key only Chrome itself can ask for
// (app-bound encryption), which is reported as ErrAppBound rather than read.
//
// Cookie values never leave this package except in the Cookie values
// returned; no error carries one.
package browsercookies

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Cookie is one cookie as a browser stores it.
type Cookie struct {
	// Host is ".example.com" for a cookie sent to the domain and every host
	// below it, "www.example.com" for one sent to that host alone.
	Host  string
	Name  string
	Value string
	Path  string
	// Expires is zero for a cookie that lives as long as the browser runs.
	Expires  time.Time
	Secure   bool
	HTTPOnly bool
	// LastUsed is when the browser last sent the cookie, zero where it does
	// not say.
	LastUsed time.Time
}

// Profile is one profile of an installed browser that has a cookie database.
type Profile struct {
	// Browser is the browser's display name, "Firefox" or "Edge".
	Browser string
	// Name is the profile's name as the browser shows it.
	Name string

	kind *browser
	goos string
	// root is the browser's own folder, which for Chromium holds Local State.
	root string
	// db is the profile's cookie database.
	db string
}

// String names the browser and the profile, "Firefox (default-release)".
func (p Profile) String() string { return p.Browser + " (" + p.Name + ")" }

var (
	// ErrAppBound is a browser that seals cookies with a key only it can use,
	// as Chrome does on Windows since version 127.
	ErrAppBound = errors.New("the browser seals its cookies with a key only the browser itself can use")
	// ErrLocked is a cookie database the running browser keeps others from
	// reading.
	ErrLocked = errors.New("the browser keeps its cookie file locked while it runs")
	// ErrNoKey is a key to the browser's cookies that could not be had.
	ErrNoKey = errors.New("the key the browser encrypts its cookies with could not be read")
)

// errUndecryptable is a value the key does not open.
var errUndecryptable = errors.New("a cookie did not decrypt with the browser's key")

// Found is a profile and its cookies of the asked domains.
type Found struct {
	Profile Profile
	Cookies []Cookie
}

// Unreadable is a profile whose cookies could not be read, and why.
type Unreadable struct {
	Profile Profile
	Err     error
}

// Profiles lists the profiles of the browsers installed for this user.
func Profiles() []Profile { return findProfiles(runtime.GOOS, userDirs()) }

// Read reads the cookies of the domains, and of every host below them, from
// every profile on this machine. A profile without any is left out.
func Read(domains []string) ([]Found, []Unreadable) { return read(Profiles(), domains) }

func read(profiles []Profile, domains []string) (found []Found, unreadable []Unreadable) {
	for _, p := range profiles {
		cookies, err := p.Cookies(domains)
		switch {
		case err != nil:
			unreadable = append(unreadable, Unreadable{Profile: p, Err: err})
		case len(cookies) > 0:
			found = append(found, Found{Profile: p, Cookies: cookies})
		}
	}
	return found, unreadable
}

// Cookies reads the profile's cookies of the domains and every host below
// them. The database is read from a copy, since the browser holds it open.
func (p Profile) Cookies(domains []string) ([]Cookie, error) {
	lower := make([]string, len(domains))
	for i, d := range domains {
		lower[i] = strings.TrimPrefix(strings.ToLower(d), ".")
	}
	tmp, err := os.MkdirTemp("", "kl-browsercookies-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	copied := filepath.Join(tmp, "cookies.db")
	if err := copyFile(p.db, copied); err != nil {
		return nil, err
	}
	// A browser writes through a write-ahead log, and a cookie set a moment
	// ago may only be there.
	if err := copyFile(p.db+"-wal", copied+"-wal"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if p.kind.family == firefox {
		return readFirefox(copied, lower)
	}
	return readChromium(p, copied, lower)
}

// copyFile copies a file the browser may hold open.
func copyFile(src, dst string) error {
	in, err := openShared(src)
	if err != nil {
		return describe(err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return describe(err)
	}
	return out.Close()
}

// describe turns a refusal to share the file into ErrLocked.
func describe(err error) error {
	if lockedOut(err) {
		return fmt.Errorf("%w (%v)", ErrLocked, err)
	}
	return err
}

// matches reports whether a cookie's host is one of the domains or below one.
func matches(host string, domains []string) bool {
	host = strings.TrimPrefix(strings.ToLower(host), ".")
	return slices.ContainsFunc(domains, func(d string) bool { return host == d || strings.HasSuffix(host, "."+d) })
}

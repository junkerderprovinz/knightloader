package ytdlp

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
)

// Cookie jars answer "Sign in to confirm you're not a bot", which only a
// logged-in session clears; yt-dlp takes it as --cookies with a Netscape
// cookie file.
//
// A jar is a live session. It is sealed in accounts.Store like hoster logins,
// so it never reaches settings.json or the diagnostics bundle; no error here
// carries its text; and Text, the only method returning plaintext, feeds the
// Backend.Cookies hook, which writes it straight to a temp file. No HTTP route
// reads a jar back.

// CookieService is the pseudo service id cookie jars are filed under in the
// shared accounts.Store, with the host as the account half of the key, as in
// hosterauth.
const CookieService = "ytdlpcookies"

// CookieStore keeps one cookies.txt per host.
type CookieStore struct {
	accounts *accounts.Store
}

// NewCookieStore wraps the app's own encrypted store; a second accounts.Store
// over the same file would overwrite the first one's writes.
func NewCookieStore(a *accounts.Store) *CookieStore { return &CookieStore{accounts: a} }

// Hosts lists every host with a stored jar, sorted.
func (s *CookieStore) Hosts() []string {
	if s == nil || s.accounts == nil {
		return nil
	}
	return s.accounts.AccountIDs(CookieService)
}

// Set stores (or, with empty text, clears) the cookies.txt for host. The text
// is stored verbatim; yt-dlp parses it, and validating one of the format's
// dialects here could refuse a jar yt-dlp accepts.
func (s *CookieStore) Set(host, text string) error {
	if s == nil || s.accounts == nil {
		return errors.New("ytdlp: no cookie store configured")
	}
	host = cookieHost(host)
	if host == "" {
		return errors.New("ytdlp: cookies need a host")
	}
	if strings.TrimSpace(text) == "" {
		return s.accounts.SetCredential(CookieService, host, accounts.Credential{})
	}
	return s.accounts.SetCredential(CookieService, host, accounts.Credential{APIKey: text})
}

// Remove clears host's jar.
func (s *CookieStore) Remove(host string) error { return s.Set(host, "") }

// Text returns the stored cookies.txt for a URL's host or a parent domain, or
// "". A decryption failure also yields "" rather than an error that could
// reach the task.
func (s *CookieStore) Text(rawurl string) string {
	if s == nil || s.accounts == nil {
		return ""
	}
	for _, host := range cookieHostChain(rawurl) {
		cred, err := s.accounts.GetCredential(CookieService, host)
		if err != nil {
			continue
		}
		if cred.APIKey != "" {
			return cred.APIKey
		}
	}
	return ""
}

// cookieHost normalises a host id, so a jar saved as "www.YouTube.com"
// matches a link to "youtube.com".
func cookieHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	h = strings.TrimPrefix(h, "www.")
	return h
}

// cookieHostChain is the host of rawurl followed by each parent domain, most
// specific first, so a jar saved as "youtube.com" also serves
// "music.youtube.com".
func cookieHostChain(rawurl string) []string {
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil
	}
	host := cookieHost(u.Hostname())
	var out []string
	for host != "" {
		out = append(out, host)
		i := strings.IndexByte(host, '.')
		if i < 0 {
			break
		}
		host = host[i+1:]
	}
	return out
}

// writeCookieFile writes text to a 0600 temp file for yt-dlp's --cookies and
// returns its path and a cleanup that removes it. The cleanup may run more
// than once. Errors never mention the jar's content.
func writeCookieFile(dir, text string) (path string, cleanup func(), err error) {
	f, err := os.CreateTemp(dir, "kl-cookies-*.txt")
	if err != nil {
		return "", func() {}, fmt.Errorf("cookie file: %w", err)
	}
	name := f.Name()
	remove := func() { _ = os.Remove(name) }
	// Set explicitly before writing. Windows has no POSIX modes, so a failure
	// is ignored; its temp directory is per-user.
	_ = f.Chmod(0o600)
	if _, err := f.WriteString(text); err != nil {
		_ = f.Close()
		remove()
		return "", func() {}, fmt.Errorf("cookie file: %w", err)
	}
	if err := f.Close(); err != nil {
		remove()
		return "", func() {}, fmt.Errorf("cookie file: %w", err)
	}
	return name, remove, nil
}

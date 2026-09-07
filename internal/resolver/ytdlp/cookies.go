package ytdlp

// cookies.go: one cookies.txt per site, sealed in the store every other
// credential in this app already lives in, handed to yt-dlp as a short-lived
// 0600 file and deleted the moment the process exits.
//
// It exists because "Sign in to confirm you're not a bot" has no other
// answer. A site that has decided this address looks automated will not be
// talked out of it by a slower download, a different user agent or a retry -
// the only thing that clears it is arriving as a session that is already
// logged in, which for yt-dlp means --cookies pointed at a Netscape cookie
// file. Nothing else in this backend can make a blocked download work.
//
// THE CONTENT IS A LIVE SESSION, and that governs every decision below:
//
//   - It is sealed by internal/accounts.Store (AES-256-GCM under the
//     per-install key in the data dir), exactly as hoster logins are - see
//     internal/hosterauth/store.go, whose reasoning this file copies rather
//     than growing a second at-rest scheme beside the first.
//   - It therefore never reaches settings.json, which is what the
//     diagnostics bundle (internal/api/routes_diagnostics.go) serialises and
//     what a person attaches to a public bug report. That is a property of
//     WHERE it is stored, not a redaction step somebody has to remember.
//   - Nothing in this file ever puts the text in an error. Every error path
//     below names the host and the operation and stops there, because an
//     error string travels straight into a task's Err field, from there into
//     the log ring, and from there into that same bundle.
//   - Text(), the one method that returns plaintext, is called by exactly one
//     caller (Backend.run, through the Cookies hook) and its result goes
//     straight into a file. It is deliberately NOT wired to any HTTP route:
//     a settings page shows Hosts() and writes with Set, and reading a jar
//     back out over the wire buys nothing a person could not paste again.
//
// Wiring: the app builds the yt-dlp backend in rewireBackends
// (internal/app/app_accounts.go), beside where it already hands that backend
// its RateLimit, Dir and Options closures, and the store belongs on the same
// *accounts.Store the rest of that function is already holding:
//
//	yb.Cookies = NewCookieStore(a.Accounts).Text
//
// Until that line exists, Backend.Cookies is nil and every download runs
// exactly as it did before this file - which is the same "nil means no
// opinion" contract RateLimit and Options already have, not a half-built
// feature waiting to misbehave.

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
)

// CookieService is the pseudo catalogue id cookie jars are filed under in the
// shared accounts.Store, with the host as the "account" half of the (service,
// account) key that store already indexes by - the same arrangement
// hosterauth.Service uses, and for the same reason: the catalogue is a short
// hand-maintained list of debrid services a picker searches, while this is one
// row per site from a list only the user's own pasting decides.
const CookieService = "ytdlpcookies"

// CookieStore keeps one cookies.txt per host.
type CookieStore struct {
	accounts *accounts.Store
}

// NewCookieStore wraps the app's existing encrypted store rather than opening
// its own. Two accounts.Store instances over one accounts.json each hold their
// own in-memory snapshot of the whole file, so the second to write silently
// erases what the first had just saved - the same trap NewStore in
// internal/hosterauth documents, and the same answer.
func NewCookieStore(a *accounts.Store) *CookieStore { return &CookieStore{accounts: a} }

// Hosts lists every host with a stored jar, sorted. Names only, never
// content: this is what a settings page renders.
func (s *CookieStore) Hosts() []string {
	if s == nil || s.accounts == nil {
		return nil
	}
	return s.accounts.AccountIDs(CookieService)
}

// Set stores (or, with empty text, clears) the cookies.txt for host.
//
// The text is stored verbatim, including its comment lines. yt-dlp reads the
// Netscape format and complains about a file it cannot parse; validating the
// shape here would mean this package deciding which of that format's several
// real-world dialects are legal, and getting that wrong would refuse a jar
// yt-dlp would have been perfectly happy with.
func (s *CookieStore) Set(host, text string) error {
	if s == nil || s.accounts == nil {
		return errors.New("ytdlp: no cookie store configured")
	}
	host = cookieHost(host)
	if host == "" {
		return errors.New("ytdlp: cookies need a host")
	}
	// Trimmed only to decide whether anything was pasted at all. The text
	// itself is stored as it came: a Netscape cookie file is line-oriented,
	// and quietly reshaping a file yt-dlp is going to parse is exactly the
	// kind of helpfulness that turns into a support thread about a jar that
	// works in one tool and not in the other.
	if strings.TrimSpace(text) == "" {
		return s.accounts.SetCredential(CookieService, host, accounts.Credential{})
	}
	return s.accounts.SetCredential(CookieService, host, accounts.Credential{APIKey: text})
}

// Remove clears host's jar.
func (s *CookieStore) Remove(host string) error { return s.Set(host, "") }

// Text returns the stored cookies.txt for a URL's host, or "" when there is
// none. A decryption failure also answers "" - see the file comment on why no
// error from this path may carry anything about the jar, and note that the
// only failures possible here (a truncated accounts.json, a .keyring replaced
// under a running install) are ones the debrid credentials in the same file
// would be reporting far more loudly than a cookie jar ever could.
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

// cookieHost normalises a stored host id the way hosterauth and the task
// list's own hostOf already do, so a jar saved from "www.YouTube.com" matches
// a link pasted as "https://youtube.com/...".
func cookieHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	h = strings.TrimPrefix(h, "www.")
	return h
}

// cookieHostChain is the host of rawurl followed by each of its parent
// domains, most specific first.
//
// The walk up matters because of where the session actually lives: YouTube
// serves video pages from www.youtube.com but the login cookies are set on
// .google.com and .youtube.com, and a person exporting a jar names it after
// the site they were on. Matching only the exact host would leave a jar saved
// as "youtube.com" unused for a "music.youtube.com" link, which looks exactly
// like the feature not working. Same shape as hostInSet in resolver.go, kept
// here as its own function because that one answers a boolean and this one
// has to hand back the keys to try in order.
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

// writeCookieFile puts text where yt-dlp's --cookies can read it and hands
// back the path plus the removal that has to run once the process is gone.
//
// 0600 and a private temp directory, both, rather than either alone:
// os.CreateTemp already creates the file with 0600, but that is a fact about
// the current implementation of one function, and this is a live session -
// so the mode is set explicitly as well, ahead of the write, and the file
// never exists in a readable state at any point.
//
// The cleanup is returned rather than deferred inside, because the file has
// to outlive this call by exactly as long as yt-dlp runs and not one moment
// longer. It is safe to call more than once, so a caller can defer it AND
// call it early on a path that gives up before spawning.
func writeCookieFile(dir, text string) (path string, cleanup func(), err error) {
	f, err := os.CreateTemp(dir, "kl-cookies-*.txt")
	if err != nil {
		// Nothing about the jar in this message, on purpose: it goes into the
		// task's Err field and from there into the diagnostics bundle.
		return "", func() {}, fmt.Errorf("cookie file: %w", err)
	}
	name := f.Name()
	remove := func() { _ = os.Remove(name) }
	if err := f.Chmod(0o600); err != nil {
		// Windows has no POSIX mode and Chmod there is close to a no-op, so a
		// refusal is not by itself a reason to abandon the download; the
		// temp directory is already per-user on every platform this builds
		// for. Recorded as nothing and carried on rather than failing a
		// download over a permission model the host does not have.
		_ = err
	}
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

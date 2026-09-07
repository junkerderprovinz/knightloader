package remotefs

import (
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

func request(url string) resolver.Request { return resolver.Request{URL: url} }

// TestMatchClaimsTheFourProtocolsAndNothingElseByShape is the guard on the one
// decision that can break every other resolver in the tree: what this one
// claims. A Match that is too wide silently takes ordinary links away from the
// backends that can actually fetch them, and the symptom is a download failing
// on a scheme rather than anything pointing back here.
func TestMatchClaimsTheFourProtocolsAndNothingElseByShape(t *testing.T) {
	r := Resolver{}
	for _, raw := range []string{
		"ftp://ftp.example.org/pub/thing.iso",
		"ftps://seedbox.example.net/files/film.mkv",
		"sftp://nas.lan/volume1/backup.tar",
		"webdav://nas.lan/dav/share",
		"webdavs://cloud.example.com/remote.php/dav/files/me/",
	} {
		if !r.Match(raw) {
			t.Errorf("Match(%q) = false, want it claimed", raw)
		}
	}
	for _, raw := range []string{
		"https://cloud.example.com/remote.php/dav/files/me/film.mkv",
		"http://nas.lan/dav/share/film.mkv",
		"https://rapidgator.net/file/abc/film.mkv",
		"magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
		"data:application/x-bittorrent;base64,ZA==",
		"not a url at all",
		"ftp://",
	} {
		if r.Match(raw) {
			t.Errorf("Match(%q) = true, want it left alone", raw)
		}
	}
}

// TestMatchClaimsHTTPSOnlyForAHostWithAnAccount is the rule that makes the
// https branch safe at all. Without the account gate this resolver would take
// every https link in the app on a guess about its path shape.
func TestMatchClaimsHTTPSOnlyForAHostWithAnAccount(t *testing.T) {
	r := Resolver{Accounts: Logins{"cloud.example.com": {Username: "me", Password: "pw"}}}

	if !r.Match("https://cloud.example.com/remote.php/dav/files/me/film.mkv") {
		t.Error("an https link on a configured WebDAV host was not claimed")
	}
	// Case is not part of a hostname's identity, and a person typing one into
	// an account form is not careful about it.
	if !r.Match("https://Cloud.Example.COM/dav/film.mkv") {
		t.Error("the host lookup is case-sensitive, so the same server typed differently is a different one")
	}
	if r.Match("https://rapidgator.net/file/abc/film.mkv") {
		t.Error("an ordinary https link was claimed; every hoster link in the app would now route here")
	}
	// http:// stays with the ordinary resolvers even on a configured host -
	// see Match's own comment for why the account gate deliberately does not
	// extend to the plaintext scheme.
	if r.Match("http://cloud.example.com/dav/film.mkv") {
		t.Error("a plaintext link on a configured host was claimed")
	}
}

func TestParseRefusesAPasswordInTheLink(t *testing.T) {
	// A password in a URL would be written to the task store in plain text and
	// shown in the collector's own URL column. Refusing is loud; stripping it
	// silently would fail authentication later with no reason attached.
	_, err := Parse("ftp://alice:hunter2@ftp.example.org/pub/thing.iso", false)
	if !errors.Is(err, ErrPasswordInLink) {
		t.Fatalf("error = %v, want the password refused by name", err)
	}
	// A username alone is not a secret and is kept, because "ftp://anonymous@"
	// is how half the public archives on the internet are addressed.
	got, err := Parse("ftp://anonymous@ftp.example.org/pub/thing.iso", false)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.User != "anonymous" {
		t.Errorf("user = %q, want the username kept", got.User)
	}
}

func TestParseFillsInTheDefaultsAndTheImplicitTLSDialect(t *testing.T) {
	cases := []struct {
		raw  string
		want Target
	}{
		{"ftp://ftp.example.org/pub/x.iso",
			Target{Kind: KindFTP, Host: "ftp.example.org", Port: 21, Path: "/pub/x.iso"}},
		{"ftps://box.example.net/x.iso",
			Target{Kind: KindFTPS, Host: "box.example.net", Port: 21, Path: "/x.iso", TLS: true}},
		// Port 990 is the only signal that says "TLS from the first byte" -
		// see the port constants for why the port and not a second scheme.
		{"ftps://box.example.net:990/x.iso",
			Target{Kind: KindFTPS, Host: "box.example.net", Port: 990, Path: "/x.iso", TLS: true, ImplicitTLS: true}},
		{"sftp://nas.lan:2222/vol/x.tar",
			Target{Kind: KindSFTP, Host: "nas.lan", Port: 2222, Path: "/vol/x.tar"}},
		{"webdav://nas.lan/dav",
			Target{Kind: KindWebDAV, Host: "nas.lan", Port: 80, Path: "/dav"}},
		{"webdavs://cloud.example.com/dav/",
			Target{Kind: KindWebDAV, Host: "cloud.example.com", Port: 443, Path: "/dav", TLS: true}},
		// A bare host addresses the server's root, which lists like any other
		// directory. "" would have every path helper below build "pub/x" and
		// hand a relative path to a server expecting an absolute one.
		{"ftp://ftp.example.org",
			Target{Kind: KindFTP, Host: "ftp.example.org", Port: 21, Path: "/"}},
		// %20 is a URL's way of spelling a space, not a file whose name has a
		// percent sign in it.
		{"ftp://ftp.example.org/pub/two%20words.iso",
			Target{Kind: KindFTP, Host: "ftp.example.org", Port: 21, Path: "/pub/two words.iso"}},
	}
	for _, c := range cases {
		got, err := Parse(c.raw, false)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.raw, err)
			continue
		}
		if got != c.want {
			t.Errorf("Parse(%q) = %+v, want %+v", c.raw, got, c.want)
		}
	}
}

func TestLinkOfIsOneSpellingPerFile(t *testing.T) {
	// Three spellings of one file would be three tasks the duplicate check
	// cannot fold together, so the staged link is always rebuilt rather than
	// carried over from the paste.
	same := []string{
		"ftp://ftp.example.org/pub/x.iso",
		"ftp://ftp.example.org:21/pub/x.iso",
		"ftp://anonymous@ftp.example.org/pub/./x.iso",
		"FTP://FTP.Example.ORG/pub/x.iso",
	}
	want := "ftp://ftp.example.org/pub/x.iso"
	for _, raw := range same {
		got, err := Parse(raw, false)
		if err != nil {
			t.Fatalf("Parse(%q): %v", raw, err)
		}
		if link := LinkOf(got); link != want {
			t.Errorf("LinkOf(%q) = %q, want %q", raw, link, want)
		}
	}
}

func TestHTTPURLKeepsTheDefaultPortOffAndEscapesThePath(t *testing.T) {
	// This string is handed to the engine, so it has to be a URL a real HTTP
	// client accepts: a raw space in the path produces a request line the
	// server rejects with 400 long before it looks at the file.
	t2, err := Parse("webdavs://cloud.example.com/dav/two words.mkv", false)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := t2.HTTPURL(), "https://cloud.example.com/dav/two%20words.mkv"; got != want {
		t.Errorf("HTTPURL = %q, want %q", got, want)
	}
	t3, _ := Parse("webdav://nas.lan:8080/dav/x.mkv", false)
	if got, want := t3.HTTPURL(), "http://nas.lan:8080/dav/x.mkv"; got != want {
		t.Errorf("HTTPURL = %q, want %q", got, want)
	}
}

// TestLoginForPrefersTheStoredAccountOverTheLink pins the order the two
// sources of a username are consulted in: the account is a decision somebody
// made, the link is a string somebody pasted.
func TestLoginForPrefersTheStoredAccountOverTheLink(t *testing.T) {
	r := Resolver{Accounts: Logins{"box.example.net": {Username: "alice", Password: "secret"}}}
	tgt, _ := Parse("ftp://bob@box.example.net/x.iso", false)
	got, err := r.loginFor(tgt)
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "alice" || got.Password != "secret" {
		t.Errorf("login = %+v, want the stored account", got)
	}

	// FTP has a real anonymous mode, so a public archive with no account still
	// resolves. SFTP has none, and says so by name rather than letting the
	// handshake fail as an authentication error three round trips later.
	bare := Resolver{}
	pub, _ := Parse("ftp://ftp.example.org/pub/x.iso", false)
	if _, err := bare.loginFor(pub); err != nil {
		t.Errorf("anonymous FTP was refused: %v", err)
	}
	nas, _ := Parse("sftp://nas.lan/vol/x.tar", false)
	if _, err := bare.loginFor(nas); !errors.Is(err, ErrNoAccount) {
		t.Errorf("error = %v, want it to say no account is stored", err)
	}
}

// TestDialErrorNeverLeaksTheOperatingSystemsOwnWords is the same guard
// internal/proxycfg's probe already carries. On Windows a refused connection
// comes back in the machine's display language, and letting it through puts
// German in the middle of an English error column - which is what the app-level
// wiring test's own log showed before this existed.
func TestDialErrorNeverLeaksTheOperatingSystemsOwnWords(t *testing.T) {
	// A real refusal, from a port nothing is bound to. Port 1 is reserved.
	c, err := net.DialTimeout("tcp", "127.0.0.1:1", 2*time.Second)
	if err == nil {
		_ = c.Close()
		t.Skip("something is listening on port 1 on this machine")
	}
	got := DialError("ftp", "127.0.0.1:1", err).Error()
	if !strings.Contains(got, "nothing answered on that port") {
		t.Errorf("DialError = %q, want it to name the failure in plain words", got)
	}
	for _, leak := range []string{"dial tcp", "connectex", "connect:", "refused"} {
		if strings.Contains(got, leak) {
			t.Errorf("the message carries raw operating-system text (%q): %q", leak, got)
		}
	}

	// A name that cannot resolve is a different mistake with a different fix,
	// so it gets its own sentence rather than being folded into the one above.
	_, err = net.LookupHost("nowhere.invalid.")
	if err == nil {
		t.Skip("this machine's resolver answers for .invalid")
	}
	if got := DialError("sftp", "nowhere.invalid:22", err).Error(); !strings.Contains(got, "does not resolve") {
		t.Errorf("DialError = %q, want the name failure named", got)
	}

	// Anything that is not a dial failure passes through untouched: inventing a
	// sentence for an error this does not recognise would hide the one detail
	// that mattered.
	own := errors.New("the server hung up mid-listing")
	if got := DialError("ftp", "host:21", own).Error(); !strings.Contains(got, own.Error()) {
		t.Errorf("DialError swallowed an unrelated error: %q", got)
	}
}

// TestCatalogueEntryMatchesTheResolverID guards the one thing the account
// store and this package agree on by a bare string with no shared constant
// between them - the same hazard internal/accounts' own
// TestCaptchaSolverEntries was written for. A rename on either side silently
// orphans every stored server login: the credential is still there, and no
// link ever finds it.
func TestCatalogueEntryMatchesTheResolverID(t *testing.T) {
	svc, ok := accounts.Lookup(ResolverID)
	if !ok {
		t.Fatalf("the catalogue has no entry for %q", ResolverID)
	}
	if svc.Kind != accounts.KindUsernamePassword {
		t.Errorf("kind = %q, want a username and a password", svc.Kind)
	}
	if svc.Group != accounts.GroupRemoteServer {
		t.Errorf("group = %q, want the remote-server group", svc.Group)
	}
}

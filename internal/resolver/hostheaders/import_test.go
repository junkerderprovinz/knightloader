package hostheaders

import (
	"strings"
	"testing"
)

// TestParseChromeCopyAsCurl is the paste this feature is actually used with.
// The line is the shape Chrome's "Copy as cURL (bash)" produces, down to the
// --compressed and the multi-line continuations.
func TestParseChromeCopyAsCurl(t *testing.T) {
	const paste = `curl 'https://forum.example.org/attachments/12/file.rar' \
  -H 'accept: text/html,application/xhtml+xml' \
  -H 'accept-language: de-DE,de;q=0.9' \
  -H 'cookie: xf_session=abc123; xf_user=42|deadbeef' \
  -H 'referer: https://forum.example.org/threads/release.99/' \
  -H 'x-forum-token: ` + secretToken + `' \
  -H 'range: bytes=0-' \
  --compressed`

	set, err := Parse(paste)
	if err != nil {
		t.Fatal(err)
	}
	set, err = Normalize(set)
	if err != nil {
		t.Fatal(err)
	}
	if set.Origin != "https://forum.example.org:443" {
		t.Errorf("Origin = %q, want the URL out of the command line", set.Origin)
	}
	got := set.Attach("https://forum.example.org/attachments/12/file.rar")
	if got["Cookie"] != "xf_session=abc123; xf_user=42|deadbeef" {
		t.Errorf("Cookie = %q", got["Cookie"])
	}
	if got["Referer"] != "https://forum.example.org/threads/release.99/" {
		t.Errorf("Referer = %q", got["Referer"])
	}
	if got["X-Forum-Token"] != secretToken {
		t.Errorf("X-Forum-Token = %q", got["X-Forum-Token"])
	}
	// Range would pin every chunk of a multi-connection download to the same
	// bytes; see the dropped list.
	if _, ok := got["Range"]; ok {
		t.Error("Range was imported; it must never come out of a paste")
	}
}

// TestParseCurlWithABasicLogin is the seedbox case: the whole credential is in
// -u and nothing else in the line says anything.
func TestParseCurlWithABasicLogin(t *testing.T) {
	// gitleaks:allow - the invented pair is the fixture, not a credential: this
	// test exists precisely because a person pastes a curl line WITH -u in it,
	// so the shape has to stay literal or the parser under test is not the one
	// being exercised.
	set, err := Parse(`curl.exe -u "demo:s3cret" "https://box.example.net:8443/files/x.mkv" -o x.mkv`)
	if err != nil {
		t.Fatal(err)
	}
	set, err = Normalize(set)
	if err != nil {
		t.Fatal(err)
	}
	if set.Origin != "https://box.example.net:8443" {
		t.Errorf("Origin = %q, want the URL and not the -o argument", set.Origin)
	}
	// "demo:s3cret" base64-encoded.
	if got := set.Attach(set.Origin + "/files/x.mkv"); got["Authorization"] != "Basic ZGVtbzpzM2NyZXQ=" {
		t.Errorf("Authorization = %q, want the -u login as Basic", got["Authorization"])
	}
}

// TestParseCurlRefusesAHalfTypedLogin: -u with no password is curl asking a
// human, and there is no human here. Sending the username with an empty
// password fails in a way that reads as a wrong password rather than as an
// unfinished paste.
func TestParseCurlRefusesAHalfTypedLogin(t *testing.T) {
	if _, err := Parse(`curl -u demo https://box.example.net/x`); err == nil {
		t.Fatal("a -u with no password was accepted")
	}
}

// TestParseCookieBlock is the shortest paste there is: document.cookie, or the
// Cookie row copied out of the network panel. It names no address at all,
// which is why Store.Import takes a fallback URL.
func TestParseCookieBlock(t *testing.T) {
	set, err := Parse("  nc_session=zzz; oc_sessionPassphrase=yyy; __Host-nc_sameSiteCookielax=true  ")
	if err != nil {
		t.Fatal(err)
	}
	if set.Origin != "" {
		t.Errorf("Origin = %q, want empty: a cookie block names no address", set.Origin)
	}
	if len(set.Headers) != 1 || set.Headers[0].Name != "Cookie" {
		t.Fatalf("Headers = %v, want one Cookie header", set.Headers)
	}
	if !strings.Contains(set.Headers[0].Value, "oc_sessionPassphrase=yyy") {
		t.Error("a cookie went missing")
	}
}

// TestParseHeaderBlockReadsTheOriginOutOfThePseudoHeaders: Chrome's "Copy
// request headers" on an HTTP/2 request starts with :authority and :scheme,
// which together are the origin - the one thing a header block can otherwise
// not say.
func TestParseHeaderBlockReadsTheOriginOutOfThePseudoHeaders(t *testing.T) {
	const paste = `:authority: cloud.example.org
:method: GET
:path: /remote.php/dav/files/demo/x.zip
:scheme: https
authorization: ` + secretBasic + `
cookie: nc_session=zzz
accept: */*`

	set, err := Parse(paste)
	if err != nil {
		t.Fatal(err)
	}
	set, err = Normalize(set)
	if err != nil {
		t.Fatal(err)
	}
	if set.Origin != "https://cloud.example.org:443" {
		t.Errorf("Origin = %q, want the one :scheme and :authority name", set.Origin)
	}
	got := set.Attach("https://cloud.example.org/remote.php/dav/files/demo/x.zip")
	if got["Authorization"] != secretBasic {
		t.Errorf("Authorization = %q", got["Authorization"])
	}
	if got["Cookie"] != "nc_session=zzz" {
		t.Errorf("Cookie = %q", got["Cookie"])
	}
	// The pseudo-headers are read for the origin and never stored: net/http
	// builds them from the URL and a request carrying them is refused.
	for _, name := range set.Names() {
		if strings.HasPrefix(name, ":") || name == "Method" || name == "Path" || name == "Scheme" || name == "Authority" {
			t.Errorf("%s was stored as a header", name)
		}
	}
}

// TestParseHTTP1BlockWalksPastTheRequestLine: a Firefox "Copy request headers"
// paste starts with "GET /x HTTP/1.1", which is not a header. Skipping it
// rather than refusing the paste is the difference between a feature that
// works on what people actually copy and one that does not.
func TestParseHTTP1BlockWalksPastTheRequestLine(t *testing.T) {
	const paste = `GET /files/x.zip HTTP/1.1
Host: box.lan
User-Agent: Mozilla/5.0
X-Auth-Token: ` + secretToken + `
Connection: keep-alive`

	set, err := Parse(paste)
	if err != nil {
		t.Fatal(err)
	}
	set, err = Normalize(set)
	if err != nil {
		t.Fatal(err)
	}
	// https and not http: a Host header says nothing about the scheme, and the
	// guess that costs a non-matching profile is better than the one that
	// attaches a credential to a plaintext hop.
	if set.Origin != "https://box.lan:443" {
		t.Errorf("Origin = %q, want https guessed off the Host header", set.Origin)
	}
	names := strings.Join(set.Names(), " ")
	if strings.Contains(names, "Host") || strings.Contains(names, "Connection") {
		t.Errorf("Names = %s, want neither Host nor Connection stored", names)
	}
	if got := set.Attach(set.Origin + "/files/x.zip"); got["X-Auth-Token"] != secretToken {
		t.Errorf("X-Auth-Token = %q", got["X-Auth-Token"])
	}
}

// TestParseFoldsEveryCookieSourceIntoOneHeader: a real Chrome paste carries
// the session in -H 'cookie:' and, on some versions, in -b as well, and a
// request holding the same cookie twice is answered differently by different
// servers.
func TestParseFoldsEveryCookieSourceIntoOneHeader(t *testing.T) {
	set, err := Parse(`curl 'https://box.lan/x' -H 'Cookie: a=1; b=2' -b 'b=3; c=4'`)
	if err != nil {
		t.Fatal(err)
	}
	set, err = Normalize(set)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Headers) != 1 {
		t.Fatalf("Names = %v, want one Cookie header", set.Names())
	}
	if got := set.Headers[0].Value; got != "a=1; b=2; c=4" {
		t.Errorf("Cookie = %q, want one of each name with the first spelling winning", got)
	}
}

func TestParseRefusesAPasteThatCarriesNoCredential(t *testing.T) {
	for _, in := range []string{"", "   ", "curl https://box.lan/x --compressed", "just some words"} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) accepted a paste with nothing in it", in)
		}
	}
}

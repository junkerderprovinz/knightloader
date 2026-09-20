package hostheaders

import (
	"strings"
	"testing"
)

// The paste has the shape Chrome's "Copy as cURL (bash)" produces.
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
	if _, ok := got["Range"]; ok {
		t.Error("Range was imported; it must never come out of a paste")
	}
}

func TestParseCurlWithABasicLogin(t *testing.T) {
	// gitleaks:allow: an invented login, the fixture for a pasted -u.
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

func TestParseCurlRefusesAHalfTypedLogin(t *testing.T) {
	if _, err := Parse(`curl -u demo https://box.example.net/x`); err == nil {
		t.Fatal("a -u with no password was accepted")
	}
}

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
	for _, name := range set.Names() {
		if strings.HasPrefix(name, ":") || name == "Method" || name == "Path" || name == "Scheme" || name == "Authority" {
			t.Errorf("%s was stored as a header", name)
		}
	}
}

// Firefox's "Copy request headers" starts with the request line.
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

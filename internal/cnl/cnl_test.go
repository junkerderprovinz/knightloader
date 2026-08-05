package cnl

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type recorder struct {
	mu        sync.Mutex
	urls      []string
	pkg       string
	passwords []string
}

func (r *recorder) AddLinksCnL(urls []string, pkg string, passwords []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.urls, r.pkg, r.passwords = urls, pkg, passwords
}

func (r *recorder) snapshot() ([]string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.urls...), r.pkg
}

func (r *recorder) snapshotPasswords() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.passwords...)
}

// newTestServer serves the CnL routes on an ephemeral port. The protocol port
// is well-known and may be held by a real JDownloader, so only the one test
// that pins the bind path uses it.
func newTestServer(t *testing.T) (*httptest.Server, *recorder) {
	t.Helper()
	rec := &recorder{}
	ts := httptest.NewServer(New(rec).handler())
	t.Cleanup(ts.Close)
	return ts, rec
}

// encryptCnL builds an addcrypted2 payload the way a website would.
func encryptCnL(t *testing.T, keyHex, plain string) string {
	t.Helper()
	key, _ := hex.DecodeString(keyHex)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	// zero-pad to block size
	b := []byte(plain)
	for len(b)%aes.BlockSize != 0 {
		b = append(b, 0)
	}
	out := make([]byte, len(b))
	cipher.NewCBCEncrypter(block, key).CryptBlocks(out, b)
	return base64.StdEncoding.EncodeToString(out)
}

const testKeyHex = "0123456789abcdef0123456789abcdef"

func TestDecryptCnL(t *testing.T) {
	links := "https://a.example/1\r\nhttps://b.example/2\r\n"
	crypted := encryptCnL(t, testKeyHex, links)
	jk := "function f(){ return '" + testKeyHex + "';}"

	got, err := DecryptCnL(jk, crypted)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "https://a.example/1" || got[1] != "https://b.example/2" {
		t.Fatalf("decrypted = %v, want the two links", got)
	}

	// Bare-hex jk (no function wrapper) is also accepted.
	if _, err := DecryptCnL(testKeyHex, crypted); err != nil {
		t.Fatalf("bare-hex jk rejected: %v", err)
	}
}

func TestServerFlashEndpoints(t *testing.T) {
	rec := &recorder{}
	s := New(rec)
	// The real bind path, on a port no real JDownloader uses, so that a broken
	// Start() cannot pass by way of the httptest shortcut the other tests take.
	if err := s.Start(19666); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close()
	time.Sleep(50 * time.Millisecond)
	baseURL := "http://127.0.0.1:19666"

	// jdcheck.js must announce a JD so extensions light up their CnL button.
	resp, err := http.Get(baseURL + "/jdcheck.js")
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 64)
	n, _ := resp.Body.Read(b)
	resp.Body.Close()
	if !strings.Contains(string(b[:n]), "jdownloader=true") {
		t.Fatalf("jdcheck.js = %q, want jdownloader=true", b[:n])
	}

	// /flash/add with a plain URL list.
	_, err = http.PostForm(baseURL+"/flash/add", url.Values{
		"urls":   {"https://x.example/f1\nhttps://x.example/f2"},
		"source": {"MySite"},
	})
	if err != nil {
		t.Fatal(err)
	}
	urls, pkg := rec.snapshot()
	if len(urls) != 2 || pkg != "MySite" {
		t.Fatalf("flash/add urls=%v pkg=%q, want 2 urls + MySite", urls, pkg)
	}

	// /flash/addcrypted2 with an encrypted payload.
	crypted := encryptCnL(t, testKeyHex, "https://enc.example/secret\r\n")
	_, err = http.PostForm(baseURL+"/flash/addcrypted2", url.Values{
		"jk":      {"function f(){ return '" + testKeyHex + "';}"},
		"crypted": {crypted},
	})
	if err != nil {
		t.Fatal(err)
	}
	urls, _ = rec.snapshot()
	if len(urls) != 1 || urls[0] != "https://enc.example/secret" {
		t.Fatalf("addcrypted2 urls=%v, want the decrypted link", urls)
	}
}

// TestPreflightOptsIntoPrivateNetworkAccess pins the two headers that decide
// whether a browser will talk to this listener at all. If it fails, Chrome's
// Private Network Access check rejects the preflight and every fetch/XHR based
// CnL button on the web fails silently against KnightLoader, while the old
// form-POST buttons keep working and hide the breakage.
func TestPreflightOptsIntoPrivateNetworkAccess(t *testing.T) {
	ts, _ := newTestServer(t)

	// Preflights are answered for every path, including ones we do not route,
	// because a 404 on a preflight surfaces in the page as a CORS error.
	paths := []string{"/", "/flash", "/flash/", "/flash/add", "/flash/addcrypted2", "/flash/addcrypted", "/jdcheck.js", "/not/a/route"}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodOptions, ts.URL+p, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Origin", "https://filehoster.example")
			req.Header.Set("Access-Control-Request-Method", "POST")
			req.Header.Set("Access-Control-Request-Headers", "content-type")
			req.Header.Set("Access-Control-Request-Private-Network", "true")
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
				t.Fatalf("preflight status = %d, want 204 or 200", resp.StatusCode)
			}
			if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
				t.Errorf("Access-Control-Allow-Origin = %q, want *", got)
			}
			if got := resp.Header.Get("Access-Control-Allow-Private-Network"); got != "true" {
				t.Errorf("Access-Control-Allow-Private-Network = %q, want true", got)
			}
			if got := resp.Header.Get("Access-Control-Allow-Methods"); !strings.Contains(got, "POST") || !strings.Contains(got, "OPTIONS") {
				t.Errorf("Access-Control-Allow-Methods = %q, want POST and OPTIONS", got)
			}
			if got := resp.Header.Get("Access-Control-Allow-Headers"); !strings.Contains(strings.ToLower(got), "content-type") {
				t.Errorf("Access-Control-Allow-Headers = %q, want content-type", got)
			}
		})
	}
}

// TestCrossOriginResponsesCarryAllowOrigin covers the actual request rather
// than the preflight. Without the header on the response the browser discards
// the body, so a site never learns whether its links arrived, and error
// responses need it just as much as successful ones.
func TestCrossOriginResponsesCarryAllowOrigin(t *testing.T) {
	ts, _ := newTestServer(t)

	cases := []struct {
		name   string
		path   string
		form   url.Values
		status int
	}{
		{"successful add", "/flash/add", url.Values{"urls": {"https://x.example/1"}}, http.StatusOK},
		{"rejected add", "/flash/add", url.Values{"urls": {""}}, http.StatusBadRequest},
		{"failed decrypt", "/flash/addcrypted2", url.Values{"jk": {"nope"}, "crypted": {"nope"}}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, ts.URL+tc.path, strings.NewReader(tc.form.Encode()))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Origin", "https://filehoster.example")
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tc.status {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.status)
			}
			if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
				t.Fatalf("Access-Control-Allow-Origin = %q, want * (browser would drop this response)", got)
			}
		})
	}
}

// TestPasswordsReachTheAdder pins that archive passwords survive the transport.
// They used to be parsed off the wire and dropped, which turned every
// password-protected archive into a stalled extraction with no visible cause.
func TestPasswordsReachTheAdder(t *testing.T) {
	crypted := encryptCnL(t, testKeyHex, "https://enc.example/secret\r\n")
	jk := "function f(){ return '" + testKeyHex + "';}"

	cases := []struct {
		name string
		path string
		form url.Values
		want []string
	}{
		{
			name: "add, newline separated",
			path: "/flash/add",
			form: url.Values{"urls": {"https://x.example/1"}, "passwords": {"first\r\nsecond\n\nthird\n"}},
			want: []string{"first", "second", "third"},
		},
		{
			name: "add, password may contain spaces",
			path: "/flash/add",
			form: url.Values{"urls": {"https://x.example/1"}, "passwords": {"correct horse battery staple"}},
			want: []string{"correct horse battery staple"},
		},
		{
			name: "add, singular spelling",
			path: "/flash/add",
			form: url.Values{"urls": {"https://x.example/1"}, "password": {"legacy"}},
			want: []string{"legacy"},
		},
		{
			name: "add, none sent",
			path: "/flash/add",
			form: url.Values{"urls": {"https://x.example/1"}},
			want: nil,
		},
		{
			name: "addcrypted2 carries them too",
			path: "/flash/addcrypted2",
			form: url.Values{"jk": {jk}, "crypted": {crypted}, "passwords": {"enc-pw"}},
			want: []string{"enc-pw"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts, rec := newTestServer(t)
			resp, err := ts.Client().PostForm(ts.URL+tc.path, tc.form)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			got := rec.snapshotPasswords()
			if len(got) != len(tc.want) {
				t.Fatalf("passwords = %q, want %q", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("passwords = %q, want %q", got, tc.want)
				}
			}
		})
	}
}

// TestAddCryptedV1AnswersNotImplemented guards the legibility of a failure we
// cannot fix. A 404 here is indistinguishable from "no downloader running", so
// users would report the wrong bug.
func TestAddCryptedV1AnswersNotImplemented(t *testing.T) {
	ts, _ := newTestServer(t)

	for _, method := range []string{http.MethodPost, http.MethodGet} {
		t.Run(method, func(t *testing.T) {
			req, err := http.NewRequest(method, ts.URL+"/flash/addcrypted", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusNotImplemented {
				t.Fatalf("status = %d, want 501", resp.StatusCode)
			}
			body, _ := io.ReadAll(resp.Body)
			if strings.TrimSpace(string(body)) == "" {
				t.Fatal("501 body is empty, want a one-line explanation")
			}
		})
	}
}

// TestAddAcceptsQueryParameters pins GET support: some sites hand the payload
// over in the URL instead of a form body, and those buttons 405'd before.
func TestAddAcceptsQueryParameters(t *testing.T) {
	crypted := encryptCnL(t, testKeyHex, "https://enc.example/secret\r\n")
	jk := "function f(){ return '" + testKeyHex + "';}"

	cases := []struct {
		name  string
		path  string
		query url.Values
		want  string
	}{
		{
			name:  "plain add",
			path:  "/flash/add",
			query: url.Values{"urls": {"https://x.example/q1"}, "package": {"QuerySite"}, "passwords": {"qpw"}},
			want:  "https://x.example/q1",
		},
		{
			name:  "addcrypted2",
			path:  "/flash/addcrypted2",
			query: url.Values{"jk": {jk}, "crypted": {crypted}, "passwords": {"qpw"}},
			want:  "https://enc.example/secret",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts, rec := newTestServer(t)
			resp, err := ts.Client().Get(ts.URL + tc.path + "?" + tc.query.Encode())
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d body = %q, want 200", resp.StatusCode, body)
			}
			if !strings.Contains(string(body), "success") {
				t.Errorf("body = %q, want success", body)
			}
			urls, _ := rec.snapshot()
			if len(urls) != 1 || urls[0] != tc.want {
				t.Fatalf("urls = %v, want [%s]", urls, tc.want)
			}
			if pw := rec.snapshotPasswords(); len(pw) != 1 || pw[0] != "qpw" {
				t.Errorf("passwords = %q, want [qpw]", pw)
			}
		})
	}
}

// TestProbeRoutesGreet covers the three places a site may look to decide
// whether a downloader is listening. A miss on any of them means the site never
// renders its CnL button at all.
func TestProbeRoutesGreet(t *testing.T) {
	ts, _ := newTestServer(t)

	for _, p := range []string{"/", "/flash", "/flash/"} {
		t.Run(p, func(t *testing.T) {
			resp, err := ts.Client().Get(ts.URL + p)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			if string(body) != "JDownloader\r\n" {
				t.Fatalf("body = %q, want %q", body, "JDownloader\r\n")
			}
		})
	}

	// The root greeting must not swallow unknown paths into a 200, or a site
	// probing a route we do not implement concludes we support it.
	resp, err := ts.Client().Get(ts.URL + "/not/a/route")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown path status = %d, want 404", resp.StatusCode)
	}
}

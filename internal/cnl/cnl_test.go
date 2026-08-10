package cnl

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// recorder implements both Adder and ContainerAdder, so the same helper
// serves every test that does not specifically care whether a JD-shaped
// backend is present. containerErr, when set, is what AddContainerCnL
// returns instead of recording — the addcrypted (v1) equivalent of a backend
// failure (e.g. no KL_JD configured on the app behind this listener).
type recorder struct {
	mu        sync.Mutex
	urls      []string
	pkg       string
	passwords []string

	containerData []byte
	containerPkg  string
	containerErr  error
}

func (r *recorder) AddLinksCnL(urls []string, pkg string, passwords []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.urls, r.pkg, r.passwords = urls, pkg, passwords
}

func (r *recorder) AddContainerCnL(data []byte, pkg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.containerErr != nil {
		return r.containerErr
	}
	r.containerData = append([]byte(nil), data...)
	r.containerPkg = pkg
	return nil
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

func (r *recorder) snapshotContainer() ([]byte, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]byte(nil), r.containerData...), r.containerPkg
}

// linksOnlyAdder implements Adder but deliberately not ContainerAdder, for
// testing what happens when the Adder behind this listener has no JD-shaped
// backend to hand an addcrypted (v1) submission to — a bridge whose remote is
// an older KnightLoader, or an App with no KL_JD configured.
type linksOnlyAdder struct{}

func (linksOnlyAdder) AddLinksCnL(urls []string, pkg string, passwords []string) {}

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

// newTestServerWithoutContainerBackend is newTestServer for an Adder with no
// JD-shaped backend at all — the 501 branch of /flash/addcrypted.
func newTestServerWithoutContainerBackend(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(New(linksOnlyAdder{}).handler())
	t.Cleanup(ts.Close)
	return ts
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
	paths := []string{
		"/", "/flash", "/flash/", "/flash/add", "/flash/addcrypted2", "/flash/addcrypted",
		"/jdcheck.js", "/flash/addcnl", "/flashgot", "/alive", "/favicon.ico", "/crossdomain.xml",
		"/not/a/route",
	}
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

// TestAddCryptedV1WithoutBackendAnswers501 guards the legibility of a failure
// this instance genuinely cannot fix on its own: an Adder with no JD-shaped
// backend behind it. A 404 here is indistinguishable from "no downloader
// running", so users would report the wrong bug.
func TestAddCryptedV1WithoutBackendAnswers501(t *testing.T) {
	ts := newTestServerWithoutContainerBackend(t)

	resp, err := ts.Client().PostForm(ts.URL+"/flash/addcrypted", url.Values{"crypted": {"c3JzYQ=="}})
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
}

// TestAddCryptedV1RefusesGET is the third submission route's own instance of
// TestSubmissionRefusesGET's rule: it now does real work (hands content to a
// JD-shaped backend), so it needs the identical POST-only guard the other two
// submission routes have, not just the 501 stub's incidental safety.
func TestAddCryptedV1RefusesGET(t *testing.T) {
	ts, rec := newTestServer(t)
	q := url.Values{"crypted": {"c3JzYQ=="}}
	resp, err := ts.Client().Get(ts.URL + "/flash/addcrypted?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("GET /flash/addcrypted answered 200; a drive-by request must not be able to submit")
	}
	if data, _ := rec.snapshotContainer(); len(data) != 0 {
		t.Fatalf("a GET reached the adder with %q", data)
	}
}

// TestAddCryptedV1Success drives the whole route: the JD-shaped backend
// receives exactly the bytes and package the site posted, and the site sees
// the same "success\r\n" the other two submission routes answer with.
func TestAddCryptedV1Success(t *testing.T) {
	ts, rec := newTestServer(t)
	resp, err := ts.Client().PostForm(ts.URL+"/flash/addcrypted", url.Values{
		"crypted": {"c3JzYS1lbmNyeXB0ZWQtcGF5bG9hZA=="},
		"package": {"CryptedSite"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body = %q, want 200", resp.StatusCode, body)
	}
	if strings.TrimSpace(string(body)) != "success" {
		t.Errorf("body = %q, want success", body)
	}
	data, pkg := rec.snapshotContainer()
	if string(data) != "c3JzYS1lbmNyeXB0ZWQtcGF5bG9hZA==" {
		t.Errorf("container data = %q, want the crypted field verbatim", data)
	}
	if pkg != "CryptedSite" {
		t.Errorf("package = %q, want CryptedSite", pkg)
	}
}

// TestAddCryptedV1AppliesSpaceToPlusFixup pins the one transform this route is
// allowed to make on the wire content before handing it on. Some clients
// form-encode a literal '+' in the base64 as a space, and unlike
// addcrypted2's AES payload this one carries no integrity check of its own to
// catch that silently corrupting it — verified against JDownloader's own
// ExternInterfaceImpl#addcrypted, which applies the identical fixup.
func TestAddCryptedV1AppliesSpaceToPlusFixup(t *testing.T) {
	ts, rec := newTestServer(t)
	// A space here is standing in for what was a '+' before some client's form
	// encoding mangled it.
	resp, err := ts.Client().PostForm(ts.URL+"/flash/addcrypted", url.Values{"crypted": {"abc def+ghi"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	data, _ := rec.snapshotContainer()
	if string(data) != "abc+def+ghi" {
		t.Errorf("container data = %q, want spaces turned into +", data)
	}
}

// TestAddCryptedV1EmptyContentIsBadRequest pins the same "reject early with a
// clear reason" behaviour /flash/add already has for an empty urls field.
func TestAddCryptedV1EmptyContentIsBadRequest(t *testing.T) {
	ts, _ := newTestServer(t)
	resp, err := ts.Client().PostForm(ts.URL+"/flash/addcrypted", url.Values{"crypted": {"   "}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// TestAddCryptedV1BackendFailureAnswersBadGateway is the branch where a
// JD-shaped backend exists but the submission itself failed (e.g. JD could
// not make sense of the payload). The site must not be told "success" for a
// submission that never reached the list.
func TestAddCryptedV1BackendFailureAnswersBadGateway(t *testing.T) {
	rec := &recorder{containerErr: errors.New("jd opened the container but produced no links")}
	ts := httptest.NewServer(New(rec).handler())
	t.Cleanup(ts.Close)

	resp, err := ts.Client().PostForm(ts.URL+"/flash/addcrypted", url.Values{"crypted": {"c3JzYQ=="}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
}

// TestFiveMissingProbeRoutesAnswerGET pins the routes real CnL sites and
// browser extensions probe for before ever trying to add a link. Every one of
// them is pure liveness: none may accept a link or a password, which is
// TestProbeRoutesRefusePOST's job to guard.
func TestFiveMissingProbeRoutesAnswerGET(t *testing.T) {
	ts, _ := newTestServer(t)
	for _, path := range []string{"/flash/addcnl", "/flashgot", "/alive", "/favicon.ico", "/crossdomain.xml"} {
		t.Run(path, func(t *testing.T) {
			resp, err := ts.Client().Get(ts.URL + path)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("GET %s status = %d, want 200", path, resp.StatusCode)
			}
		})
	}
}

// TestProbeRoutesRefusePOST is do-not-widen-GET's mirror image: these five
// routes are read-only probes, and none of them may grow a POST-triggered
// side effect either — the whole point of adding them was to answer a
// liveness check, not to open five more submission surfaces.
func TestProbeRoutesRefusePOST(t *testing.T) {
	ts, rec := newTestServer(t)
	for _, path := range []string{"/flash/addcnl", "/flashgot", "/alive", "/favicon.ico", "/crossdomain.xml"} {
		t.Run(path, func(t *testing.T) {
			resp, err := ts.Client().PostForm(ts.URL+path, url.Values{"urls": {"https://evil.example/x"}})
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				t.Errorf("POST %s answered 200; a probe route must not accept a submission", path)
			}
			if urls, _ := rec.snapshot(); len(urls) != 0 {
				t.Errorf("POST %s reached the adder with %v", path, urls)
			}
		})
	}
}

// TestSubmissionRefusesGET is a security test, not a compatibility one. A GET
// route on these endpoints would be a browser "simple request": no preflight,
// no user gesture, no navigation. Any page — an ad iframe, an <img src>, an
// email preview — could then queue downloads and archive passwords into this
// instance without the user ever knowing.
func TestSubmissionRefusesGET(t *testing.T) {
	for _, path := range []string{"/flash/add", "/flash/addcrypted2"} {
		t.Run(path, func(t *testing.T) {
			ts, rec := newTestServer(t)
			q := url.Values{"urls": {"https://evil.example/payload.exe"}, "passwords": {"pwned"}}
			resp, err := ts.Client().Get(ts.URL + path + "?" + q.Encode())
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				t.Fatalf("GET %s answered 200; a drive-by request must not be able to submit", path)
			}
			if urls, _ := rec.snapshot(); len(urls) != 0 {
				t.Fatalf("a GET reached the adder with %v", urls)
			}
		})
	}
}

// TestPostReadsQueryParameters keeps the compatibility half: a site may put the
// payload in the query string as long as it still posts.
func TestPostReadsQueryParameters(t *testing.T) {
	ts, rec := newTestServer(t)
	q := url.Values{"urls": {"https://x.example/q1"}, "package": {"QuerySite"}, "passwords": {"qpw"}}
	resp, err := ts.Client().Post(ts.URL+"/flash/add?"+q.Encode(), "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body = %q, want 200", resp.StatusCode, body)
	}
	urls, pkg := rec.snapshot()
	if len(urls) != 1 || urls[0] != "https://x.example/q1" {
		t.Fatalf("urls = %v", urls)
	}
	if pkg != "QuerySite" {
		t.Errorf("package = %q, want QuerySite", pkg)
	}
	if pw := rec.snapshotPasswords(); len(pw) != 1 || pw[0] != "qpw" {
		t.Errorf("passwords = %q, want [qpw]", pw)
	}
}

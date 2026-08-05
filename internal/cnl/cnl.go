// Package cnl implements the Click'n'Load protocol: browser extensions and
// "CNL" buttons on websites POST link lists to 127.0.0.1:9666, the de-facto
// standard port JDownloader and pyLoad listen on. KnightLoader answers the
// same protocol, so existing extensions work unchanged.
package cnl

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Adder is what the app exposes to CnL (AddLinksCnL). passwords carries the
// archive passwords a CnL button ships alongside its links; it is nil when the
// site sent none, which is the common case.
type Adder interface {
	AddLinksCnL(urls []string, pkg string, passwords []string)
}

// Server is the CnL listener.
type Server struct {
	adder Adder
	srv   *http.Server
}

// New builds the listener; Start binds 127.0.0.1:port (9666 is the standard).
func New(adder Adder) *Server { return &Server{adder: adder} }

// handler builds the routing table. It is separate from Start so tests can
// drive the protocol without binding the well-known port, which may be held by
// a real JDownloader.
func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()

	// Several sites probe for a running downloader before they render their
	// CnL button, and they do not agree on where: some ask /flash, some
	// /flash/, some the bare root. All three answer the same greeting JD does.
	greet := func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "JDownloader\r\n")
	}
	mux.HandleFunc("GET /{$}", greet)
	mux.HandleFunc("GET /flash", greet)
	mux.HandleFunc("GET /flash/{$}", greet)

	mux.HandleFunc("GET /jdcheck.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = fmt.Fprint(w, "jdownloader=true;\nvar version='90000';\n")
	})

	// Submission is POST only, deliberately. A GET route here would be a
	// "simple request" in the browser's sense: no preflight, no user gesture,
	// no navigation. Any page in the world — an ad iframe, an <img src>, an
	// email preview — could then queue arbitrary downloads, and arbitrary
	// archive passwords, into this instance. Sites that pass parameters in the
	// query string still work, because ParseForm merges the query into
	// FormValue for a POST as well.
	add := func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		urls := splitLinks(r.FormValue("urls"))
		if len(urls) == 0 {
			http.Error(w, "no urls", http.StatusBadRequest)
			return
		}
		s.adder.AddLinksCnL(urls, packageOf(r), passwordsOf(r))
		_, _ = fmt.Fprint(w, "success\r\n")
	}
	mux.HandleFunc("POST /flash/add", add)

	addCrypted2 := func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		urls, err := DecryptCnL(r.FormValue("jk"), r.FormValue("crypted"))
		if err != nil || len(urls) == 0 {
			http.Error(w, "decrypt failed", http.StatusBadRequest)
			return
		}
		s.adder.AddLinksCnL(urls, packageOf(r), passwordsOf(r))
		_, _ = fmt.Fprint(w, "success\r\n")
	}
	mux.HandleFunc("POST /flash/addcrypted2", addCrypted2)

	// addcrypted (v1) encrypts its payload against JDownloader's own RSA public
	// key, so nobody but JDownloader can open it. Answering 501 instead of
	// letting the route 404 keeps the failure legible: the site reports "not
	// supported" rather than "no downloader running", which is the difference
	// between a bug report we can act on and a week of guessing.
	mux.HandleFunc("/flash/addcrypted", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "addcrypted (v1) is RSA-encrypted for JDownloader and cannot be decrypted; the site must use addcrypted2", http.StatusNotImplemented)
	})

	return withCORS(mux)
}

// Start begins serving; it returns an error if the port is taken (e.g. a real
// JDownloader is running) so the caller can log and continue without CnL.
func (s *Server) Start(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	s.srv = &http.Server{Handler: s.handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = s.srv.Serve(ln) }()
	return nil
}

// withCORS stamps the Click'n'Load CORS policy onto every response and answers
// preflights itself, for every path rather than per route, so that a site
// probing an endpoint we do not serve still gets a clean CORS answer instead of
// a 404 the browser reports to the page as a CORS failure.
//
// The wildcard origin is deliberate and is correct HERE AND ONLY HERE. CnL
// exists precisely so that an arbitrary third-party page may hand links to a
// downloader running on the same machine, and this listener binds 127.0.0.1
// only, so "any origin" still means "a page open in the browser of the person
// sitting at this keyboard". Restricting it to an origin allowlist would break
// every CnL button on every site, which is the entire point of the package. The
// main API in internal/api deliberately does the opposite: it sends no CORS
// headers at all and relies on same-origin plus session auth, because it can
// start, delete and reconfigure downloads. Do not "harmonise" the two.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			// Chrome's Private Network Access check refuses to let a public
			// page (https://filehoster.example) reach a private address
			// (127.0.0.1) unless the preflight explicitly opts in with this
			// header. Without it, fetch/XHR based CnL buttons fail with an
			// opaque network error while old-style form POSTs, which are never
			// preflighted, keep working: exactly the kind of half-broken that
			// costs days to diagnose.
			h.Set("Access-Control-Allow-Private-Network", "true")
			h.Set("Access-Control-Max-Age", "86400")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Close stops the listener.
func (s *Server) Close() error {
	if s.srv != nil {
		return s.srv.Close()
	}
	return nil
}

func packageOf(r *http.Request) string {
	for _, k := range []string{"package", "source"} {
		if v := strings.TrimSpace(r.FormValue(k)); v != "" {
			return v
		}
	}
	return "Click'n'Load"
}

// passwordsOf pulls the archive passwords a CnL button ships with its links.
// They arrive newline-separated in a single field; "password" is the older
// singular spelling that some sites still send. Unlike link lists these must
// not be split on spaces, because a password may legitimately contain one.
func passwordsOf(r *http.Request) []string {
	raw := r.FormValue("passwords")
	if strings.TrimSpace(raw) == "" {
		raw = r.FormValue("password")
	}
	var out []string
	for _, p := range strings.FieldsFunc(raw, func(c rune) bool { return c == '\n' || c == '\r' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func splitLinks(s string) []string {
	var out []string
	for _, l := range strings.FieldsFunc(s, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ' ' || r == '\t'
	}) {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://") {
			out = append(out, l)
		}
	}
	return out
}

// jkHex pulls the hex key out of the "jk" JavaScript snippet, which by
// convention is `function f(){ return '<hex>';}`. The key is extracted, never
// executed.
var jkHex = regexp.MustCompile(`(?i)return\s*['"]([0-9a-f]+)['"]`)

// DecryptCnL decodes an addcrypted2 payload: AES-128-CBC, key == IV, key from
// the jk function, ciphertext base64 in crypted.
func DecryptCnL(jk, crypted string) ([]string, error) {
	m := jkHex.FindStringSubmatch(jk)
	if m == nil {
		// Some pages send the bare hex without the function wrapper.
		bare := strings.TrimSpace(jk)
		if ok, _ := regexp.MatchString(`^(?i)[0-9a-f]{32}$`, bare); ok {
			m = []string{bare, bare}
		} else {
			return nil, errors.New("cnl: no hex key in jk")
		}
	}
	key, err := hex.DecodeString(m[1])
	if err != nil || len(key) != 16 {
		return nil, errors.New("cnl: jk key must be 16 bytes of hex")
	}
	ct, err := base64.StdEncoding.DecodeString(strings.TrimSpace(crypted))
	if err != nil {
		return nil, fmt.Errorf("cnl: bad base64: %w", err)
	}
	if len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return nil, errors.New("cnl: ciphertext not block-aligned")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	pt := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, key).CryptBlocks(pt, ct)
	// Strip zero + PKCS#7 padding, both occur in the wild.
	pt = stripPadding(pt)
	return splitLinks(string(pt)), nil
}

func stripPadding(b []byte) []byte {
	b = []byte(strings.TrimRight(string(b), "\x00"))
	if n := len(b); n > 0 {
		if p := int(b[n-1]); p > 0 && p <= aes.BlockSize && n >= p {
			ok := true
			for _, c := range b[n-p:] {
				if int(c) != p {
					ok = false
					break
				}
			}
			if ok {
				b = b[:n-p]
			}
		}
	}
	return b
}

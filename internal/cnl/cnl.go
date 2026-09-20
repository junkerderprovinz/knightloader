// Package cnl implements the Click'n'Load protocol: browser extensions and
// "CNL" buttons on websites POST link lists to 127.0.0.1:9666, the port
// JDownloader and pyLoad listen on. KnightLoader answers the same protocol, so
// existing extensions work unchanged.
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

// Adder receives the links of a submission. passwords holds the archive
// passwords the site sent with them, usually none.
type Adder interface {
	AddLinksCnL(urls []string, pkg string, passwords []string)
}

// ContainerAdder is an Adder that can also take a Click'n'Load v1
// ("addcrypted") submission, which is encrypted for JDownloader's own RSA key
// and so has to go to the JD backend like an uploaded .dlc. Without it,
// /flash/addcrypted answers 501.
type ContainerAdder interface {
	AddContainerCnL(data []byte, pkg string) error
}

// Server is the CnL listener.
type Server struct {
	adder Adder
	srv   *http.Server
}

// New builds the listener; Start binds 127.0.0.1:port (9666 is the standard).
func New(adder Adder) *Server { return &Server{adder: adder} }

// handler builds the routing table. It is separate from Start so tests can
// drive the protocol without binding the well-known port.
func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()

	// Sites probe for a running downloader at different paths before showing
	// their button. These are liveness checks only and accept nothing.
	greet := func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "JDownloader\r\n")
	}
	mux.HandleFunc("GET /{$}", greet)
	mux.HandleFunc("GET /flash", greet)
	mux.HandleFunc("GET /flash/{$}", greet)
	mux.HandleFunc("GET /flash/addcnl", greet)
	mux.HandleFunc("GET /alive", greet)

	mux.HandleFunc("GET /jdcheck.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = fmt.Fprint(w, "jdownloader=true;\nvar version='90000';\n")
	})

	// FlashGot's detection probe. JDownloader also queues downloads from GET
	// parameters here; that would be a drive-by submission route, so this
	// answers the probe only.
	mux.HandleFunc("GET /flashgot", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/x-icon")
		w.WriteHeader(http.StatusOK)
	})

	// The Flash cross-domain policy, still probed for by old CnL
	// implementations. Content matches JDownloader's
	// Cnl2APIBasics#crossdomainxml.
	mux.HandleFunc("GET /crossdomain.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		_, _ = fmt.Fprint(w, "<?xml version=\"1.0\"?>\n"+
			"<!DOCTYPE cross-domain-policy SYSTEM \"http://www.macromedia.com/xml/dtds/cross-domain-policy.dtd\">\n"+
			"<cross-domain-policy>\n<allow-access-from domain=\"*\" />\n</cross-domain-policy>\n")
	})

	// Submission is POST only. A GET would be a browser "simple request" that
	// any page, ad iframe or <img src> could fire to queue downloads and
	// passwords. Query parameters still work, since ParseForm merges them for
	// a POST too.
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

	// addcrypted (v1) is encrypted for JDownloader's RSA key, so only a JD
	// backend can open it. POST only for the same reason as above. JD's own
	// handler reads no passwords here, so neither does this one.
	mux.HandleFunc("POST /flash/addcrypted", func(w http.ResponseWriter, r *http.Request) {
		ca, ok := s.adder.(ContainerAdder)
		if !ok {
			// 501 lets the site report "not supported" rather than "no
			// downloader running".
			http.Error(w, "addcrypted (v1) needs the JDownloader backend, which is not available here; the site must use addcrypted2", http.StatusNotImplemented)
			return
		}
		_ = r.ParseForm()
		raw := r.FormValue("crypted")
		if strings.TrimSpace(raw) == "" {
			http.Error(w, "no crypted content", http.StatusBadRequest)
			return
		}
		// Some clients turn '+' in the base64 into a space when form-encoding
		// it. JDownloader applies the same fixup
		// (org.jdownloader.api.cnl2.ExternInterfaceImpl#addcrypted).
		fixed := strings.ReplaceAll(strings.TrimSpace(raw), " ", "+")
		if err := ca.AddContainerCnL([]byte(fixed), packageOf(r)); err != nil {
			http.Error(w, "addcrypted (v1): "+err.Error(), http.StatusBadGateway)
			return
		}
		_, _ = fmt.Fprint(w, "success\r\n")
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

// withCORS applies the Click'n'Load CORS policy to every path and answers
// preflights itself, so a probe of an unrouted path gets a clean answer
// rather than a 404 the page sees as a CORS failure.
//
// The wildcard origin is right here and nowhere else: CnL exists so any
// third-party page can hand links to a downloader on the same machine, and
// the listener binds loopback only. An origin allowlist would break every
// site not on it. The main API in internal/api sends no CORS headers and
// relies on same-origin and session auth instead; the two differ on purpose.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			// Chrome's Private Network Access check lets a public page reach
			// 127.0.0.1 only if the preflight opts in. Without it fetch-based
			// buttons fail while plain form POSTs keep working.
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

// passwordsOf returns the newline-separated archive passwords of a
// submission; "password" is an older singular spelling. They are not split on
// spaces, since a password may contain one.
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

// jkHex pulls the hex key out of the "jk" JavaScript snippet, conventionally
// `function f(){ return '<hex>';}`. The snippet is never executed.
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
	// Both zero and PKCS#7 padding occur in the wild.
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

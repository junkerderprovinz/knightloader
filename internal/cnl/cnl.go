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

// Adder is what the app exposes to CnL (AddLinks).
type Adder interface {
	AddLinksCnL(urls []string, pkg string)
}

// Server is the CnL listener.
type Server struct {
	adder Adder
	srv   *http.Server
}

// New builds the listener; Start binds 127.0.0.1:port (9666 is the standard).
func New(adder Adder) *Server { return &Server{adder: adder} }

// Start begins serving; it returns an error if the port is taken (e.g. a real
// JDownloader is running) so the caller can log and continue without CnL.
func (s *Server) Start(port int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/jdcheck.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = fmt.Fprint(w, "jdownloader=true;\nvar version='90000';\n")
	})
	mux.HandleFunc("/flash", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "JDownloader\r\n")
	})
	mux.HandleFunc("POST /flash/add", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		urls := splitLinks(r.FormValue("urls"))
		if len(urls) == 0 {
			http.Error(w, "no urls", http.StatusBadRequest)
			return
		}
		s.adder.AddLinksCnL(urls, packageOf(r))
		_, _ = fmt.Fprint(w, "success\r\n")
	})
	mux.HandleFunc("POST /flash/addcrypted2", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		urls, err := DecryptCnL(r.FormValue("jk"), r.FormValue("crypted"))
		if err != nil || len(urls) == 0 {
			http.Error(w, "decrypt failed", http.StatusBadRequest)
			return
		}
		s.adder.AddLinksCnL(urls, packageOf(r))
		_, _ = fmt.Fprint(w, "success\r\n")
	})
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = s.srv.Serve(ln) }()
	return nil
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

package cnl

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type recorder struct {
	mu   sync.Mutex
	urls []string
	pkg  string
}

func (r *recorder) AddLinksCnL(urls []string, pkg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.urls = urls
	r.pkg = pkg
}

func (r *recorder) snapshot() ([]string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.urls...), r.pkg
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

func TestDecryptCnL(t *testing.T) {
	keyHex := "0123456789abcdef0123456789abcdef"
	links := "https://a.example/1\r\nhttps://b.example/2\r\n"
	crypted := encryptCnL(t, keyHex, links)
	jk := "function f(){ return '" + keyHex + "';}"

	got, err := DecryptCnL(jk, crypted)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "https://a.example/1" || got[1] != "https://b.example/2" {
		t.Fatalf("decrypted = %v, want the two links", got)
	}

	// Bare-hex jk (no function wrapper) is also accepted.
	if _, err := DecryptCnL(keyHex, crypted); err != nil {
		t.Fatalf("bare-hex jk rejected: %v", err)
	}
}

func TestServerFlashEndpoints(t *testing.T) {
	rec := &recorder{}
	s := New(rec)
	// Port 0 lets the OS pick; drive the handler through the live listener.
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
	keyHex := "0123456789abcdef0123456789abcdef"
	crypted := encryptCnL(t, keyHex, "https://enc.example/secret\r\n")
	_, err = http.PostForm(baseURL+"/flash/addcrypted2", url.Values{
		"jk":      {"function f(){ return '" + keyHex + "';}"},
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

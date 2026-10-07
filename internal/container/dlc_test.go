package container

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The application key of these tests, and the key a made-up DLC is sealed
// with. Neither is anybody's real key.
const (
	testAppAES = "0123456789abcdef"
	testAppIV  = "fedcba9876543210"
	testDLCAES = "k3yOfThisOneDLC!"
)

// tail is the block a made-up DLC ends with, the same one fakeDLC uses.
var tail = func() string {
	s := fakeDLC("")
	return s[len(s)-dlcKeyLen:]
}()

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// dlcXMLDoc is a DLC's XML as JDownloader writes it, every value in base64.
func dlcXMLDoc(packages map[string][]string) string {
	var b strings.Builder
	b.WriteString(`<dlc><header><generator><app>` + b64("JDownloader") + `</app></generator>`)
	b.WriteString(`<dlcxmlversion>` + b64("20_02_2008") + `</dlcxmlversion></header><content>`)
	names := make([]string, 0, len(packages))
	for n := range packages {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		b.WriteString(`<package name="` + b64(n) + `">`)
		for _, u := range packages[n] {
			b.WriteString(`<file><url>` + b64(u) + `</url><filename>` + b64("part.rar") + `</filename><size>` + b64("1024") + `</size></file>`)
		}
		b.WriteString(`</package>`)
	}
	b.WriteString(`</content></dlc>`)
	return b.String()
}

func encryptCBC(t *testing.T, key, iv, plain []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, plain)
	return out
}

func pkcs7(b []byte) []byte {
	n := aes.BlockSize - len(b)%aes.BlockSize
	return append(append([]byte(nil), b...), bytes.Repeat([]byte{byte(n)}, n)...)
}

func zeroPad(b []byte) []byte {
	out := append([]byte(nil), b...)
	for len(out)%aes.BlockSize != 0 {
		out = append(out, 0)
	}
	return out
}

// sealDLC builds a DLC file around xml: the payload sealed with testDLCAES
// and pad, then the key block.
func sealDLC(t *testing.T, xml string, pad func([]byte) []byte) []byte {
	t.Helper()
	inner := []byte(b64(xml))
	ct := encryptCBC(t, []byte(testDLCAES), []byte(testDLCAES), pad(inner))
	return []byte(base64.StdEncoding.EncodeToString(ct) + tail)
}

// sealedKey is the <rc> the service sends: testDLCAES encrypted with the
// application key, followed by bytes clients ignore.
func sealedKey(t *testing.T, appKey, appIV string) string {
	t.Helper()
	sealed := encryptCBC(t, []byte(appKey), []byte(appIV), []byte(testDLCAES))
	return base64.StdEncoding.EncodeToString(append(sealed, "trailing"...))
}

// dlcStub stands in for the DLC service with the given answer and counts the
// requests it gets.
type dlcStub struct {
	calls atomic.Int32
	query atomic.Value
}

func useDLCService(t *testing.T, key, iv string, handler func(w http.ResponseWriter, r *http.Request)) *dlcStub {
	t.Helper()
	stub := &dlcStub{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.calls.Add(1)
		stub.query.Store(r.URL.Query())
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	prevKey, prevIV, prevURL := dlcKey, dlcIV, dlcService
	dlcKey, dlcIV, dlcService = key, iv, srv.URL+"/dlcrypt/service.php"
	t.Cleanup(func() { dlcKey, dlcIV, dlcService = prevKey, prevIV, prevURL })
	return stub
}

func answer(rc string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<rc>" + rc + "</rc>")) }
}

func TestOpenReadsEveryLinkOfADLC(t *testing.T) {
	want := []string{
		"https://hoster.example/file/aaa/movie.part1.rar.html",
		"https://hoster.example/file/bbb/movie.part2.rar.html",
		"https://other.example/d/ccc?x=1&y=2",
	}
	xml := dlcXMLDoc(map[string][]string{"Movie": want[:2], "Extras": want[2:]})
	for _, pad := range []struct {
		name string
		fn   func([]byte) []byte
	}{{"pkcs7", pkcs7}, {"zero", zeroPad}} {
		t.Run(pad.name, func(t *testing.T) {
			stub := useDLCService(t, testAppAES, testAppIV, answer(sealedKey(t, testAppAES, testAppIV)))
			got, err := Open(context.Background(), "movie.dlc", sealDLC(t, xml, pad.fn))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			slices.Sort(got)
			sorted := slices.Clone(want)
			slices.Sort(sorted)
			if !slices.Equal(got, sorted) {
				t.Errorf("links = %v, want %v", got, sorted)
			}
			if n := stub.calls.Load(); n != 1 {
				t.Errorf("the service was asked %d times, want once", n)
			}
			q := stub.query.Load().(url.Values)
			if q.Get("srcType") != "dlc" || q.Get("destType") != "KNIGHT" || q.Get("data") != tail {
				t.Errorf("the service was asked srcType=%q destType=%q data=%q", q.Get("srcType"), q.Get("destType"), q.Get("data"))
			}
		})
	}
}

func TestOpenDLCWithLineBreaks(t *testing.T) {
	useDLCService(t, testAppAES, testAppIV, answer(sealedKey(t, testAppAES, testAppIV)))
	file := sealDLC(t, dlcXMLDoc(map[string][]string{"p": {"https://hoster.example/file/a"}}), pkcs7)
	wrapped := bytes.Join([][]byte{file[:40], file[40:120], file[120:]}, []byte("\r\n"))
	got, err := Open(context.Background(), "a.dlc", append(wrapped, '\n'))
	if err != nil || len(got) != 1 {
		t.Fatalf("Open = %v, %v; want the one link", got, err)
	}
}

func TestOpenDLCReadsPlainAndDoubleEncodedURLs(t *testing.T) {
	xml := `<dlc><header></header><content><package name="p">` +
		`<file><url>https://hoster.example/plain</url></file>` +
		`<file><url>` + b64(b64("https://hoster.example/twice")) + `</url></file>` +
		`</package></content></dlc>`
	useDLCService(t, testAppAES, testAppIV, answer(sealedKey(t, testAppAES, testAppIV)))
	got, err := Open(context.Background(), "p.dlc", sealDLC(t, xml, pkcs7))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := []string{"https://hoster.example/plain", "https://hoster.example/twice"}
	if !slices.Equal(got, want) {
		t.Errorf("links = %v, want %v", got, want)
	}
}

func TestADLCThatDoesNotOpenGoesToTheBackendWithTheReason(t *testing.T) {
	xml := dlcXMLDoc(map[string][]string{"p": {"https://hoster.example/file/a"}})
	tests := []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		file    string
		want    string
	}{
		{
			name:    "service error",
			handler: func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "down", http.StatusBadGateway) },
			want:    "502",
		},
		{
			name:    "answer without a key",
			handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>maintenance</html>")) },
			want:    "no key",
		},
		{
			name:    "rate limited",
			handler: answer(dlcRateLimited),
			want:    "too many",
		},
		{
			name:    "key for another client",
			handler: answer(sealedKey(t, "anotherclientkey", "anotherclientiv!")),
			want:    "does not open",
		},
		{
			name:    "no links inside",
			handler: answer(sealedKey(t, testAppAES, testAppIV)),
			file:    dlcXMLDoc(map[string][]string{"p": {"just a name"}}),
			want:    "none of its entries",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := useDLCService(t, testAppAES, testAppIV, tt.handler)
			body := xml
			if tt.file != "" {
				body = tt.file
			}
			links, err := Open(context.Background(), "film.dlc", sealDLC(t, body, pkcs7))
			if !errors.Is(err, ErrNeedsBackend) {
				t.Fatalf("Open = %v, %v; want ErrNeedsBackend", links, err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("reason %q does not mention %q", err, tt.want)
			}
			if n := stub.calls.Load(); n != 1 {
				t.Errorf("the service was asked %d times, want once", n)
			}
		})
	}
}

func TestADLCServiceThatHangsIsGivenUp(t *testing.T) {
	stub := useDLCService(t, testAppAES, testAppIV, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	file := sealDLC(t, dlcXMLDoc(map[string][]string{"p": {"https://hoster.example/a"}}), pkcs7)
	_, err := Open(ctx, "slow.dlc", file)
	if !errors.Is(err, ErrNeedsBackend) {
		t.Fatalf("Open = %v, want ErrNeedsBackend", err)
	}
	if strings.Contains(err.Error(), tail) || strings.Contains(err.Error(), "data=") {
		t.Errorf("the reason %q carries the request address and with it the DLC's key block", err)
	}
	if n := stub.calls.Load(); n != 1 {
		t.Errorf("the service was asked %d times, want once", n)
	}
}

func TestABuildWithoutTheKeyLeavesADLCToTheBackend(t *testing.T) {
	stub := useDLCService(t, "", "", answer(sealedKey(t, testAppAES, testAppIV)))
	if CanOpenDLC() {
		t.Fatal("CanOpenDLC with no key")
	}
	_, err := Open(context.Background(), "film.dlc", sealDLC(t, dlcXMLDoc(map[string][]string{"p": {"https://hoster.example/a"}}), pkcs7))
	if !errors.Is(err, ErrNeedsBackend) || !errors.Is(err, errNoDLCKey) {
		t.Fatalf("Open = %v, want ErrNeedsBackend for want of a key", err)
	}
	if n := stub.calls.Load(); n != 0 {
		t.Errorf("the service was asked %d times without a key to read its answer", n)
	}
}

func TestOpenKeepsTheChecksOfLinks(t *testing.T) {
	stub := useDLCService(t, testAppAES, testAppIV, answer(sealedKey(t, testAppAES, testAppIV)))
	if _, err := Open(context.Background(), "film.dlc", []byte("<html>404 not found</html>")); err == nil || errors.Is(err, ErrNeedsBackend) {
		t.Errorf("a broken .dlc gave %v, want the ValidateDLC refusal", err)
	}
	got, err := Open(context.Background(), "list.txt", []byte("https://example.com/a"))
	if err != nil || len(got) != 1 {
		t.Errorf("a link list gave %v, %v", got, err)
	}
	if n := stub.calls.Load(); n != 0 {
		t.Errorf("the service was asked %d times for files that are no DLC", n)
	}
}

func TestDecodeLoose(t *testing.T) {
	for _, in := range []string{"aGVsbG8=", "aGVsbG8", "aGVs\r\nbG8=", "aGVsbG8=\x03\x03\x03", "aGVsbG8\x00\x00"} {
		got, err := decodeLoose(in)
		if err != nil || string(got) != "hello" {
			t.Errorf("decodeLoose(%q) = %q, %v", in, got, err)
		}
	}
}

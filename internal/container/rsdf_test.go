package container

import (
	"crypto/aes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"testing"
)

// writeRSDF builds an RSDF the way the format's writers do: each link in AES
// CFB-8 under the fixed key, one base64 line per link, the whole hex encoded.
// The IV is JDownloader's precomputed constant, so a reader that derives it
// differently fails here.
func writeRSDF(t *testing.T, links ...string) []byte {
	t.Helper()
	block, err := aes.NewCipher(rsdfKey)
	if err != nil {
		t.Fatal(err)
	}
	iv, _ := hex.DecodeString("a3d5a33cb95ac1f5cbdb1ad25cb0a7aa")
	stream := make([]byte, aes.BlockSize)
	var lines []string
	for _, link := range links {
		ct := make([]byte, len(link))
		for i := range len(link) {
			block.Encrypt(stream, iv)
			ct[i] = link[i] ^ stream[0]
			copy(iv, iv[1:])
			iv[aes.BlockSize-1] = ct[i]
		}
		lines = append(lines, base64.StdEncoding.EncodeToString(ct))
	}
	return []byte(strings.ToUpper(hex.EncodeToString([]byte(strings.Join(lines, "\r\n")))))
}

func TestRSDFOpensWithoutTheBackend(t *testing.T) {
	want := []string{"https://example.com/file/a", "http://example.org/b.rar"}
	data := writeRSDF(t, want[0], "CCF: "+want[1])

	if got := Detect("film.rsdf", data); got != KindRSDF {
		t.Fatalf("Detect = %q, want rsdf", got)
	}
	links, err := Links("film.rsdf", data)
	if err != nil {
		t.Fatalf("Links: %v", err)
	}
	if !slices.Equal(links, want) {
		t.Errorf("links = %q, want %q", links, want)
	}
}

func TestRSDFWrappedOverSeveralLinesStillOpens(t *testing.T) {
	data := writeRSDF(t, "https://example.com/a", "https://example.com/b")
	var wrapped strings.Builder
	for i := 0; i < len(data); i += 76 {
		wrapped.Write(data[i:min(i+76, len(data))])
		wrapped.WriteString("\r\n")
	}
	links, err := DecodeRSDF([]byte(wrapped.String()))
	if err != nil || len(links) != 2 {
		t.Fatalf("DecodeRSDF = %q, %v", links, err)
	}
}

func TestRSDFThatDoesNotDecodeGoesToTheBackend(t *testing.T) {
	garbage := []byte(strings.ToUpper(hex.EncodeToString([]byte(base64.StdEncoding.EncodeToString([]byte("not a link at all"))))))
	_, err := Links("film.rsdf", garbage)
	if !errors.Is(err, ErrNeedsBackend) {
		t.Fatalf("Links = %v, want ErrNeedsBackend so JDownloader gets a try", err)
	}
	if !strings.Contains(err.Error(), "RSDF") {
		t.Errorf("error %q does not say why the local decoding failed", err)
	}
}

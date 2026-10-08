package container

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// dlcKey and dlcIV are KnightLoader's own application key for the DLC
// service. They are set at build time from the KL_DLC_KEY and KL_DLC_IV
// secrets with -ldflags -X, and a build without them hands every DLC to the
// JDownloader backend. See docs/decisions.md.
var (
	dlcKey string
	dlcIV  string
)

// dlcService turns a DLC's key block into the key that opens it, encrypted
// for the client named by destType.
var dlcService = "https://service.jdownloader.org/dlcrypt/service.php"

// dlcDestType is the client id the service issued to KnightLoader, spelled
// the way it was registered there.
const dlcDestType = "KIGHT"

// dlcTimeout bounds the one request a DLC costs. The service answers in well
// under a second; this only keeps an upload from hanging on a dead one.
const dlcTimeout = 20 * time.Second

// dlcRateLimited is the service's answer while it is refusing a client that
// opened too many DLCs in a short time.
const dlcRateLimited = "2YVhzRFdjR2dDQy9JL25aVXFjQ1RPZ"

var dlcRC = regexp.MustCompile(`(?s)<rc>(.*?)</rc>`)

// errNoDLCKey is the fallback reason in a build made without the key.
var errNoDLCKey = errors.New("this build carries no DLC key")

// CanOpenDLC reports whether this build can open a DLC itself.
func CanOpenDLC() bool {
	return len(dlcKey) == aes.BlockSize && len(dlcIV) == aes.BlockSize
}

// Open is Links with one difference: a DLC is opened here when this build
// carries the key. When it does not, or the service or its answer fails, the
// error is ErrNeedsBackend wrapping the reason, as for an RSDF or CCF that
// will not decode.
func Open(ctx context.Context, name string, data []byte) ([]string, error) {
	links, err := Links(name, data)
	if !errors.Is(err, ErrNeedsBackend) || Detect(name, data) != KindDLC {
		return links, err
	}
	links, err = OpenDLC(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNeedsBackend, err)
	}
	return links, nil
}

// OpenDLC returns the links in a DLC, at the cost of one request to the DLC
// service.
func OpenDLC(ctx context.Context, data []byte) ([]string, error) {
	if err := ValidateDLC(data); err != nil {
		return nil, err
	}
	if !CanOpenDLC() {
		return nil, errNoDLCKey
	}
	body := strings.NewReplacer("\r", "", "\n", "").Replace(strings.TrimSpace(string(data)))
	keyBlock, payload := body[len(body)-dlcKeyLen:], body[:len(body)-dlcKeyLen]

	rc, err := fetchDLCKey(ctx, keyBlock)
	if err != nil {
		return nil, err
	}
	plain, err := decryptDLC(rc, payload)
	if err != nil {
		return nil, err
	}
	links, err := dlcLinks(plain)
	if err != nil {
		return nil, err
	}
	if len(links) == 0 {
		return nil, errors.New("the DLC opens, but none of its entries is a link")
	}
	return links, nil
}

// fetchDLCKey asks the service for the encrypted key of one DLC and returns
// the content of its <rc> element. There is no retry: a refusal or a dead
// service is the JDownloader backend's to deal with.
func fetchDLCKey(ctx context.Context, keyBlock string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, dlcTimeout)
	defer cancel()
	q := url.Values{"srcType": {"dlc"}, "destType": {dlcDestType}, "data": {keyBlock}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dlcService+"?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	host := req.URL.Host
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// The bare cause: the URL carries the DLC's key block, which has no
		// place in a log.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return "", fmt.Errorf("the DLC service at %s did not answer: %w", host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the DLC service at %s answered %s", host, resp.Status)
	}
	answer, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", fmt.Errorf("the DLC service at %s: %w", host, err)
	}
	m := dlcRC.FindSubmatch(answer)
	if m == nil || len(strings.TrimSpace(string(m[1]))) == 0 {
		return "", fmt.Errorf("the DLC service at %s sent no key for this file", host)
	}
	rc := strings.TrimSpace(string(m[1]))
	if rc == dlcRateLimited {
		return "", fmt.Errorf("the DLC service at %s is refusing for now, because too many DLCs were opened in a short time", host)
	}
	return rc, nil
}

// decryptDLC opens the payload with the key the service sent. That key comes
// encrypted with the application key, and once decrypted it is both the key
// and the IV of the payload, whose plaintext is the base64 of the DLC's XML.
func decryptDLC(rc, payload string) ([]byte, error) {
	sealed, err := decodeLoose(rc)
	if err != nil || len(sealed) < aes.BlockSize {
		return nil, errors.New("the key the DLC service sent is not one this build can read")
	}
	key := cbc([]byte(dlcKey), []byte(dlcIV), sealed[:aes.BlockSize])

	ct, err := decodeLoose(payload)
	if err != nil || len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return nil, errors.New("the DLC's content is damaged")
	}
	xmlText, err := decodeLoose(string(cbc(key, key, ct)))
	if err != nil || !strings.Contains(string(xmlText), "<content") {
		return nil, errors.New("the key the DLC service sent does not open this DLC")
	}
	return xmlText, nil
}

// cbc decrypts whole AES blocks in CBC mode, leaving any padding in place:
// decodeLoose drops it with the rest of what is not base64.
func cbc(key, iv, data []byte) []byte {
	block, err := aes.NewCipher(key)
	if err != nil {
		// Both callers pass 16 bytes.
		panic(err)
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)
	return out
}

// decodeLoose decodes base64 the way JDownloader and pyLoad do, skipping
// whatever is not in the alphabet. That covers line breaks, PKCS#7 and zero
// padding behind a decrypted payload, and a missing or partial "=" at the end.
func decodeLoose(s string) ([]byte, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '=' {
			break
		}
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/' {
			b.WriteByte(c)
		}
	}
	clean := b.String()
	if len(clean)%4 == 1 {
		clean = clean[:len(clean)-1]
	}
	return base64.RawStdEncoding.DecodeString(clean)
}

// dlcXML is the part of a DLC's XML that holds links. Since 2008 every value
// is base64; the first DLCs wrote them as plain text.
type dlcXML struct {
	Packages []struct {
		Files []struct {
			URLs []string `xml:"url"`
		} `xml:"file"`
	} `xml:"content>package"`
}

// dlcLinks reads the links out of a decrypted DLC.
func dlcLinks(plain []byte) ([]string, error) {
	s := strings.TrimSpace(string(plain))
	// The oldest DLCs are a bare <header> and <content> with no root.
	if i := strings.Index(s, "<dlc"); i >= 0 {
		s = s[i:]
		if j := strings.LastIndex(s, "</dlc>"); j >= 0 {
			s = s[:j+len("</dlc>")]
		}
	} else {
		s = "<dlc>" + s + "</dlc>"
	}
	var doc dlcXML
	if err := xml.Unmarshal([]byte(s), &doc); err != nil {
		return nil, fmt.Errorf("the DLC opens, but its content is not readable: %w", err)
	}
	var links []string
	for _, p := range doc.Packages {
		for _, f := range p.Files {
			for _, u := range f.URLs {
				links = append(links, dlcURL(u)...)
			}
		}
	}
	return links, nil
}

// dlcURL reads one <url>. It is base64 in every DLC since 2008, plain in the
// ones before, and some generators encoded it twice.
func dlcURL(v string) []string {
	v = strings.TrimSpace(v)
	for range 3 {
		if links := urlBodyLinks(v); len(links) > 0 {
			return links
		}
		raw, err := decodeLoose(v)
		if err != nil || len(raw) == 0 {
			return nil
		}
		v = strings.TrimSpace(string(raw))
	}
	return nil
}

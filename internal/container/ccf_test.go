package container

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math/bits"
	"slices"
	"testing"
)

var ccfLinks = []string{"https://example.com/file/a.rar", "https://example.com/file/b.rar?x=1&y=2"}

// cryptLoadXML is the document every CCF version wraps, in CryptLoad's shape.
const cryptLoadXML = `<?xml version="1.0" encoding="utf-8"?><CryptLoad><Package service="" name="Film" url="Directlinks">` +
	`<Download Url="https://example.com/file/a.rar"><Url>https://example.com/file/a.rar</Url><FileName>a.rar</FileName></Download>` +
	`<Download Url="https://example.com/file/b.rar?x=1&amp;y=2"><Url>https://example.com/file/b.rar?x=1&amp;y=2</Url></Download>` +
	`</Package></CryptLoad>`

func cbcEncrypt(t *testing.T, k ccfKey, plain []byte) []byte {
	t.Helper()
	key, _ := hex.DecodeString(k.key)
	iv, _ := hex.DecodeString(k.iv)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	padded := append([]byte(nil), plain...)
	for len(padded)%aes.BlockSize != 0 {
		padded = append(padded, 0)
	}
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
	return out
}

// writeCCF3 runs CryptLoad's 3.0 scrambling backwards. The key sequence does
// not depend on the data, so it is worked out first and then applied in
// reverse, step by step against JDownloader's decoder.
func writeCCF3(seed uint32, plain []byte) []byte {
	last := len(plain) % 64
	if last == 0 {
		last = 64
	}
	body := append([]byte(nil), plain...)
	for len(body)%64 != 0 {
		body = append(body, 0)
	}
	head := append([]byte("CCF3.0"), 0, 0, 0, 0, byte(last))
	binary.LittleEndian.PutUint32(head[6:10], seed)

	key := seed
	out := head
	for off := 0; off < len(body); off += 64 {
		var first [64]uint32
		for i := range first {
			if key&1 != 0 {
				key = bits.RotateLeft32(key, -int((key&0xff)%12))
			} else {
				key = bits.RotateLeft32(key, int((key&0xff)%9))
			}
			first[i] = key
		}
		var second [64]uint32
		for i := range second {
			second[i] = key
			if key&1 != 0 {
				key = bits.RotateLeft32(key, -int(key&12))
			} else {
				key = bits.RotateLeft32(key, int(key&9))
			}
		}
		var buf [64]byte
		copy(buf[:], body[off:off+64])
		for row := 0; row < 8; row++ {
			var tmp [8]byte
			copy(tmp[:], buf[row*8:])
			for m := 7; m >= 0; m-- {
				tmp = bitBox(tmp)
				tmp[m] ^= byte(second[row*8+m])
			}
			copy(buf[row*8:], tmp[:])
		}
		for i := 63; i >= 0; i-- {
			buf = byteBox(buf)
			buf[i] ^= byte(first[i])
		}
		out = append(out, buf[:]...)
	}
	return out
}

func TestCCFOpensWithoutTheBackend(t *testing.T) {
	plain := []byte(cryptLoadXML)
	cases := map[string][]byte{
		"0.7 to 1.0, first key":  cbcEncrypt(t, ccf07Keys[0], plain),
		"0.7 to 1.0, second key": cbcEncrypt(t, ccf07Keys[1], plain),
		"0.7 to 1.0, third key":  cbcEncrypt(t, ccf07Keys[2], plain),
		"3.0":                    writeCCF3(0x5eed1234, plain),
		"5.0": cbcEncrypt(t, ccf5Keys[0],
			cbcEncrypt(t, ccf5Keys[1], cbcEncrypt(t, ccf5Keys[0], plain))),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			links, err := Links("film.ccf", data)
			if err != nil {
				t.Fatalf("Links: %v", err)
			}
			if !slices.Equal(links, ccfLinks) {
				t.Errorf("links = %q, want %q", links, ccfLinks)
			}
		})
	}
}

func TestCCFKeepsOnlyWhatTheLinkScannerAccepts(t *testing.T) {
	doc := `<CryptLoad><Package>` +
		`<Download><Url>file:///etc/passwd</Url></Download>` +
		`<Download><Url>javascript:alert(1)</Url></Download>` +
		"<Download><Url>https://a.example/x\nhttps://b.example/y</Url></Download>" +
		`<Download><Url>not a url at all</Url></Download>` +
		`</Package></CryptLoad>`
	links, err := Links("film.ccf", cbcEncrypt(t, ccf07Keys[0], []byte(doc)))
	if err != nil {
		t.Fatalf("Links: %v", err)
	}
	want := []string{"https://a.example/x", "https://b.example/y"}
	if !slices.Equal(links, want) {
		t.Errorf("links = %q, want %q", links, want)
	}
}

func TestCCF3DropsThePaddingOfItsLastBlock(t *testing.T) {
	for _, plain := range []string{cryptLoadXML, cryptLoadXML[:128]} {
		got := decryptCCF3(writeCCF3(42, []byte(plain)))
		if string(got) != plain {
			t.Errorf("decrypted %d bytes, want the %d of the document and nothing after", len(got), len(plain))
		}
	}
}

func TestCCFNoKeyOpensGoesToTheBackend(t *testing.T) {
	stranger := ccfKey{"00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff", "000102030405060708090a0b0c0d0e0f"}
	for name, data := range map[string][]byte{
		"unknown key":   cbcEncrypt(t, stranger, []byte(cryptLoadXML)),
		"not CryptLoad": cbcEncrypt(t, ccf07Keys[0], []byte("<html>gone</html>")),
		"bare 3.0":      []byte("CCF3.0"),
	} {
		_, err := Links("film.ccf", data)
		if !errors.Is(err, ErrNeedsBackend) {
			t.Errorf("%s: Links = %v, want ErrNeedsBackend so JDownloader gets a try", name, err)
		}
	}
}

func TestACCFThatOpensButHoldsNoLinkIsRefusedHere(t *testing.T) {
	doc := `<?xml version="1.0"?><CryptLoad><Package name="Film">` +
		`<Download><Url>file:///etc/passwd</Url></Download>` +
		`<Download><Url>javascript:alert(1)</Url></Download>` +
		`<Download><Url>not a url at all</Url></Download>` +
		`</Package></CryptLoad>`
	_, err := Links("film.ccf", cbcEncrypt(t, ccf07Keys[0], []byte(doc)))
	if errors.Is(err, ErrNeedsBackend) || !errors.Is(err, ErrEmpty) {
		t.Errorf("Links = %v, want ErrEmpty: JDownloader opens it with the same keys and finds nothing either", err)
	}
}

func TestACCFLinkKeepsTheSpacesAndBracesInIt(t *testing.T) {
	doc := `<CryptLoad><Package>` +
		`<Download><Url>https://example.com/files/a b.zip</Url></Download>` +
		`<Download><Url>https://example.com/get?x=[1]&amp;y={2}</Url></Download>` +
		`</Package></CryptLoad>`
	links, err := Links("film.ccf", cbcEncrypt(t, ccf07Keys[0], []byte(doc)))
	if err != nil {
		t.Fatalf("Links: %v", err)
	}
	want := []string{"https://example.com/files/a%20b.zip", "https://example.com/get?x=[1]&y={2}"}
	if !slices.Equal(links, want) {
		t.Errorf("links = %q, want %q", links, want)
	}
}

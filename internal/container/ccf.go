package container

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"html"
	"math/bits"
	"regexp"
)

// CCF came from CryptLoad and went through three schemes, all with keys built
// into the program. Formats and keys follow JDownloader's
// org.jdownloader.container.C (GPL-3.0).

// ccfKey is one AES-256 key and its CBC IV.
type ccfKey struct{ key, iv string }

// ccf07Keys are the keys of CCF 0.7 to 1.0, which encrypt once with one of them.
var ccf07Keys = []ccfKey{
	{"026900E977C6402442B661329CFE62D6ED21BDEB0CD6321318A8EDC7BC5A6C86", "8CE1173EBAD76E08584B94573926231E"},
	{"171BF8E34C3D0C0C2693FDD2B080423A5B98F4D028A0AF4D82A385D837A8F95F", "9FE95FFF7CA4FC0FCEF25E4F7444AE67"},
	{"5F679C00548737E120E6518A981BD0BA11AF5C719E97502983AD6AA38ED721C3", "E3D153AD609EF7358D66684180C7331A"},
}

// ccf5Keys are the two keys of CCF 5.0, which encrypts with the first, the
// second and the first again.
var ccf5Keys = [2]ccfKey{
	{"64e9e143ce4634da5cd99b0cbfa3002a9a3765e10cb19cff906db6a68f95b398", "e0feabe3f4b13e6f05f4a5a35b7fbdc8"},
	{"ffc9122b34fae1043087dca5faaaab109414049a6ad2f9f161c7576be464e48a", "b506b639984c9285adfac5b42bae6f47"},
}

// ccf3Magic opens a CCF 3.0, the one version that marks itself.
var ccf3Magic = []byte("CCF3.0")

// ccfURL finds the links in the decrypted CryptLoad XML.
var ccfURL = regexp.MustCompile(`(?is)<url>(.*?)</url>`)

// DecodeCCF returns the links in a CCF of version 0.7 to 1.0, 3.0 or 5.0.
// Every version decrypts to CryptLoad's XML, and its name in the result is
// how a right key is told from a wrong one.
func DecodeCCF(data []byte) ([]string, error) {
	plain, ok := decryptCCF(data)
	if !ok {
		return nil, errors.New("no CCF key opens this file")
	}
	// Each <Url> goes through the scanner on its own: joined, the scanner
	// would take one body's end for a mail client's line break and glue the
	// next body onto it.
	var links []string
	for _, m := range ccfURL.FindAllSubmatch(plain, -1) {
		links = append(links, parseText(html.UnescapeString(string(m[1])))...)
	}
	if len(links) == 0 {
		return nil, errors.New("the CCF decrypted to no links")
	}
	return links, nil
}

func decryptCCF(data []byte) ([]byte, bool) {
	if bytes.HasPrefix(data, ccf3Magic) {
		return isCryptLoad(decryptCCF3(data))
	}
	for _, k := range ccf07Keys {
		if plain, ok := isCryptLoad(cbcDecrypt(k, data)); ok {
			return plain, true
		}
	}
	return isCryptLoad(cbcDecrypt(ccf5Keys[0], cbcDecrypt(ccf5Keys[1], cbcDecrypt(ccf5Keys[0], data))))
}

func isCryptLoad(plain []byte) ([]byte, bool) {
	return plain, bytes.Contains(plain, []byte("CryptLoad"))
}

// cbcDecrypt is AES-CBC without padding removal, as CCF has none to remove.
// Input that is not whole blocks is not a CCF and comes back as nil.
func cbcDecrypt(k ccfKey, data []byte) []byte {
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil
	}
	key, _ := hex.DecodeString(k.key)
	iv, _ := hex.DecodeString(k.iv)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)
	return out
}

// decryptCCF3 undoes CryptLoad's own scrambling: a header with a 32-bit seed
// and the length of the last block, then 64-byte blocks, each XORed with a
// rotating key while being transposed as an 8x8 byte matrix and then, row by
// row, as 8x8 bit matrices.
func decryptCCF3(data []byte) []byte {
	const header = 11
	if len(data) <= header || (len(data)-header)%64 != 0 || data[10] > 64 {
		return nil
	}
	body := data[header:]
	key := binary.LittleEndian.Uint32(data[6:10])
	out := make([]byte, 0, len(body))
	var buf [64]byte
	for off := 0; off < len(body); off += 64 {
		copy(buf[:], body[off:off+64])
		for i := range buf {
			if key&1 != 0 {
				key = bits.RotateLeft32(key, -int((key&0xff)%12))
			} else {
				key = bits.RotateLeft32(key, int((key&0xff)%9))
			}
			buf[i] ^= byte(key)
			buf = byteBox(buf)
		}
		for row := 0; row < 8; row++ {
			var tmp [8]byte
			copy(tmp[:], buf[row*8:])
			for m := range tmp {
				tmp[m] ^= byte(key)
				tmp = bitBox(tmp)
				if key&1 != 0 {
					key = bits.RotateLeft32(key, -int(key&12))
				} else {
					key = bits.RotateLeft32(key, int(key&9))
				}
			}
			copy(buf[row*8:], tmp[:])
		}
		out = append(out, buf[:]...)
	}
	return out[:len(out)-(64-int(data[10]))]
}

// byteBox transposes 64 bytes as an 8x8 matrix.
func byteBox(b [64]byte) [64]byte {
	var t [64]byte
	for i := 0; i < 8; i++ {
		for j := 0; j < 8; j++ {
			t[i*8+j] = b[j*8+i]
		}
	}
	return t
}

// bitBox transposes 8 bytes as an 8x8 bit matrix.
func bitBox(b [8]byte) [8]byte {
	var t [8]byte
	for i := 0; i < 8; i++ {
		for j := 0; j < 8; j++ {
			if b[i]&(1<<j) != 0 {
				t[j] |= 1 << i
			}
		}
	}
	return t
}

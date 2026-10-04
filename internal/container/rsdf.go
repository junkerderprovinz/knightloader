package container

import (
	"crypto/aes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// rsdfKey is the AES-192 key every RSDF is written with, and the reason the
// format can be read without anybody's help. Format and key follow
// JDownloader's org.jdownloader.container.R (GPL-3.0).
var rsdfKey = []byte{
	0x8c, 0x35, 0x19, 0x2d, 0x96, 0x4d, 0xc3, 0x18, 0x2c, 0x6f, 0x84, 0xf3,
	0x25, 0x22, 0x39, 0xeb, 0x4a, 0x32, 0x0d, 0x25, 0x00, 0x00, 0x00, 0x00,
}

// DecodeRSDF returns the links in an RSDF: hex around base64 lines, each one
// link in AES CFB-8 whose shift register runs on from one line to the next.
func DecodeRSDF(data []byte) ([]string, error) {
	raw, err := hex.DecodeString(strings.Join(strings.Fields(string(data)), ""))
	if err != nil {
		return nil, fmt.Errorf("the RSDF is not hex throughout: %w", err)
	}
	block, err := aes.NewCipher(rsdfKey)
	if err != nil {
		return nil, err
	}
	// The IV is the key's encryption of sixteen 0xff bytes.
	iv := make([]byte, aes.BlockSize)
	for i := range iv {
		iv[i] = 0xff
	}
	block.Encrypt(iv, iv)

	var plain strings.Builder
	stream := make([]byte, aes.BlockSize)
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		ct, err := base64.StdEncoding.DecodeString(line)
		if err != nil {
			return nil, fmt.Errorf("an RSDF line is not base64: %w", err)
		}
		for _, c := range ct {
			block.Encrypt(stream, iv)
			plain.WriteByte(c ^ stream[0])
			copy(iv, iv[1:])
			iv[aes.BlockSize-1] = c
		}
		plain.WriteByte('\n')
	}
	links := parseText(strings.ReplaceAll(plain.String(), "CCF:", " "))
	if len(links) == 0 {
		return nil, errors.New("the RSDF decrypted to no links")
	}
	return links, nil
}

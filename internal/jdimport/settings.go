package jdimport

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"errors"
	"io/fs"
	"sort"
	"strings"
)

// accountsKey is the AES key JDownloader seals its account list with, from
// the @CryptedStorage annotation on AccountSettings.getAccounts(). The same
// bytes serve as the IV.
var accountsKey = []byte{1, 6, 4, 5, 2, 7, 4, 3, 12, 61, 14, 75, 0xfe, 0xf9, 0xd4, 33}

// decryptCBC opens AppWork's JSonStorage encryption: AES-128-CBC with the key
// as IV and PKCS#5 padding. Padding that does not check out is kept, as
// AppWork falls back to reading without padding.
func decryptCBC(data, key []byte) ([]byte, error) {
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, errors.New("the file is not a whole number of AES blocks")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, key).CryptBlocks(out, data)
	if n := int(out[len(out)-1]); n >= 1 && n <= aes.BlockSize && bytes.Equal(out[len(out)-n:], bytes.Repeat([]byte{byte(n)}, n)) {
		out = out[:len(out)-n]
	}
	return out, nil
}

// jsonBytes drops a UTF-8 byte order mark, which AppWork accepts on read.
func jsonBytes(b []byte) []byte {
	return bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
}

// accountData is the part of JDownloader's AccountData this package reads.
type accountData struct {
	User       string         `json:"user"`
	Password   string         `json:"password"`
	Hoster     string         `json:"hoster"`
	Enabled    bool           `json:"enabled"`
	Properties map[string]any `json:"properties"`
}

func (c *Config) readAccounts(fsys fs.FS, dir string) {
	b, ok := c.readFile(fsys, dir, fileAccounts)
	if !ok {
		return
	}
	plain, err := decryptCBC(b, accountsKey)
	if err == nil {
		var byHost map[string][]accountData
		if err = json.Unmarshal(jsonBytes(plain), &byHost); err == nil {
			c.Accounts = flattenAccounts(byHost)
			return
		}
	}
	c.Problems = append(c.Problems, Reason{
		Code:   "accountsLocked",
		Params: map[string]string{"file": fileAccounts},
		Text:   fileAccounts + " could not be decrypted, so no account can be taken over.",
	})
}

// flattenAccounts lists the accounts host by host in name order, each host's
// accounts in JDownloader's order.
func flattenAccounts(byHost map[string][]accountData) []Account {
	hosts := make([]string, 0, len(byHost))
	for h := range byHost {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	var out []Account
	for _, h := range hosts {
		for _, d := range byHost[h] {
			host := strings.ToLower(strings.TrimSpace(h))
			if host == "" {
				host = strings.ToLower(strings.TrimSpace(d.Hoster))
			}
			out = append(out, Account{
				Host:       host,
				User:       d.User,
				Password:   d.Password,
				Enabled:    d.Enabled,
				Properties: d.Properties,
			})
		}
	}
	return out
}

// readGeneral reads the default download folder. JDownloader writes the key
// only once it differs from the system's Downloads folder, so an absent key
// leaves DownloadDir empty.
func (c *Config) readGeneral(fsys fs.FS, dir string) {
	b, ok := c.readFile(fsys, dir, fileGeneral)
	if !ok {
		return
	}
	var m map[string]any
	if err := json.Unmarshal(jsonBytes(b), &m); err != nil {
		c.problem(fileGeneral, err)
		return
	}
	if s, ok := m["defaultdownloadfolder"].(string); ok {
		c.DownloadDir = strings.TrimSpace(s)
	}
}

// readPasswords reads the extraction extension's password list.
func (c *Config) readPasswords(fsys fs.FS, dir string) {
	b, ok := c.readFile(fsys, dir, filePasswords)
	if !ok {
		return
	}
	var list []string
	if err := json.Unmarshal(jsonBytes(b), &list); err != nil {
		c.problem(filePasswords, err)
		return
	}
	for _, pw := range list {
		if pw != "" {
			c.ArchivePasswords = append(c.ArchivePasswords, pw)
		}
	}
}

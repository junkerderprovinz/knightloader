// Package jdimporttest writes a JDownloader 2 cfg folder for tests, in the
// formats JDownloader itself writes: the encrypted account list, the rule
// lists, the settings files and a download list zip. It exists only for
// tests; nothing in the built binaries imports it.
package jdimporttest

import (
	"archive/zip"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Account is one entry of the account list.
type Account struct {
	Host       string
	User       string
	Password   string
	Enabled    bool
	Properties map[string]any
}

// Link is one link of a download list package. State is JDownloader's final
// link state, such as "FINISHED", or empty for a link still to download.
type Link struct {
	URL        string
	Name       string
	Size       int64
	Enabled    bool
	State      string
	Protected  bool
	Properties map[string]any
}

// Package is one download list package.
type Package struct {
	Name    string
	Folder  string
	Comment string
	Links   []Link
}

// Config is what a fixture cfg folder holds. A nil or empty field writes no
// file, as JDownloader leaves out a file still at its defaults.
type Config struct {
	Accounts []Account
	// Packagizer and LinkFilter are rule objects as JDownloader serialises
	// them.
	Packagizer    []map[string]any
	LinkFilter    []map[string]any
	PackagizerOff bool
	Passwords     []string
	DownloadDir   string
	Packages      []Package
	// ListNumber names the download list downloadList<ListNumber>.zip.
	ListNumber int
}

// accountsKey is JDownloader's key for the account list, from AccountSettings.
var accountsKey = []byte{1, 6, 4, 5, 2, 7, 4, 3, 12, 61, 14, 75, 0xfe, 0xf9, 0xd4, 33}

// Files returns the cfg folder's files by name.
func Files(t testing.TB, c Config) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	put := func(name string, v any) {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = b
	}
	if len(c.Accounts) > 0 {
		byHost := map[string][]map[string]any{}
		for _, a := range c.Accounts {
			byHost[a.Host] = append(byHost[a.Host], map[string]any{
				"user": a.User, "password": a.Password, "hoster": a.Host, "enabled": a.Enabled,
				"properties": a.Properties, "id": -1, "maxSimultanDownloads": 0,
			})
		}
		plain, err := json.Marshal(byHost)
		if err != nil {
			t.Fatal(err)
		}
		files["org.jdownloader.settings.AccountSettings.accounts.ejs"] = seal(t, plain)
	}
	if c.Packagizer != nil {
		put("org.jdownloader.controlling.packagizer.PackagizerSettings.rulelist.json", c.Packagizer)
	}
	if c.PackagizerOff {
		put("org.jdownloader.controlling.packagizer.PackagizerSettings.json", map[string]any{"packagizerenabled": false})
	}
	if c.LinkFilter != nil {
		put("org.jdownloader.controlling.filter.LinkFilterSettings.filterlist.json", c.LinkFilter)
	}
	if c.Passwords != nil {
		put("org.jdownloader.extensions.extraction.ExtractionExtension.passwordlist.json", c.Passwords)
	}
	if c.DownloadDir != "" {
		put("org.jdownloader.settings.GeneralSettings.json", map[string]any{
			"defaultdownloadfolder": c.DownloadDir, "maxsimultanedownloads": 3,
		})
	}
	if c.Packages != nil {
		files[fmt.Sprintf("downloadList%d.zip", c.ListNumber)] = DownloadList(t, c.Packages)
	}
	return files
}

// Write writes the cfg folder into dir.
func Write(t testing.TB, dir string, c Config) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, b := range Files(t, c) {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Zip returns a zip of the cfg folder with every file below prefix, such as
// "JDownloader 2.0/cfg/".
func Zip(t testing.TB, prefix string, c Config) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, b := range Files(t, c) {
		w, err := zw.Create(prefix + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// DownloadList builds a download list zip: one JSON entry per package and
// one per link, named as JDownloader names them.
func DownloadList(t testing.TB, pkgs []Package) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, v any) {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	for i, p := range pkgs {
		props := map[string]any{}
		if p.Comment != "" {
			props["COMMENT"] = p.Comment
		}
		add(fmt.Sprintf("%02d", i), map[string]any{
			"uid": i + 1, "name": p.Name, "downloadFolder": p.Folder, "properties": props, "links": []any{},
		})
		for j, l := range p.Links {
			entry := map[string]any{
				"uid": 100*i + j, "name": l.Name, "url": l.URL, "size": l.Size, "enabled": l.Enabled,
				"availablestatus": "TRUE", "urlProtection": "UNSET", "properties": l.Properties,
			}
			if l.State != "" {
				entry["finalLinkState"] = l.State
			}
			if l.Protected {
				entry["urlProtection"] = "PROTECTED_CONTAINER"
				entry["url"] = "CRYPTED:AAAA"
				entry["properties"] = nil
			}
			add(fmt.Sprintf("%02d_%02d", i, j), entry)
		}
	}
	add("extraInfo", map[string]any{"rootPath": "/output", "timeStamp": 1})
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// seal encrypts as AppWork's JSonStorage does: AES-128-CBC, the key as IV,
// PKCS#5 padding.
func seal(t testing.TB, plain []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(accountsKey)
	if err != nil {
		t.Fatal(err)
	}
	n := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append(append([]byte{}, plain...), bytes.Repeat([]byte{byte(n)}, n)...)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, accountsKey).CryptBlocks(out, padded)
	return out
}

// Rule is a rule object as JDownloader writes it, every condition present and
// switched off, with over laid on top.
func Rule(name string, over map[string]any) map[string]any {
	text := func() map[string]any {
		return map[string]any{"enabled": false, "matchType": "CONTAINS", "regex": nil, "useRegex": false}
	}
	r := map[string]any{
		"name": name, "enabled": true, "staticRule": false, "created": 1700000000000,
		"filenameFilter": text(), "hosterURLFilter": text(), "sourceURLFilter": text(),
		"packagenameFilter": text(), "commentFilter": text(),
		"filesizeFilter": map[string]any{"enabled": false, "from": 0, "to": 0, "matchType": "BETWEEN"},
		"filetypeFilter": map[string]any{
			"enabled": false, "matchType": "IS", "useRegex": false, "customs": nil,
			"audioFilesEnabled": false, "videoFilesEnabled": false, "archivesEnabled": false,
			"imagesEnabled": false, "docFilesEnabled": false, "subFilesEnabled": false,
			"exeFilesEnabled": false, "hashEnabled": false,
		},
		"matchAlwaysFilter":      map[string]any{"enabled": false},
		"onlineStatusFilter":     map[string]any{"enabled": false, "matchType": "IS", "onlineStatus": "OFFLINE"},
		"pluginStatusFilter":     map[string]any{"enabled": false, "matchType": "IS", "pluginStatus": "PREMIUM"},
		"originFilter":           map[string]any{"enabled": false, "matchType": "IS", "origins": nil},
		"conditionFilter":        nil,
		"linkEnabledFilter":      map[string]any{"enabled": false, "matchType": "IS_TRUE"},
		"downloadListDupeFilter": map[string]any{"enabled": false, "matchType": "IS_TRUE"},
		"linkgrabberDupeFilter":  map[string]any{"enabled": false, "matchType": "IS_TRUE"},
		"autoAddEnabled":         nil, "autoExtractionEnabled": nil, "autoForcedStartEnabled": nil,
		"autoStartEnabled": nil, "linkEnabled": nil, "chunks": 0, "priority": nil,
		"comment": nil, "downloadDestination": nil, "filename": nil, "packageName": nil,
		"rename": nil, "moveto": nil, "stopAfterThisRule": false,
	}
	for k, v := range over {
		r[k] = v
	}
	return r
}

// Text is a switched-on text condition.
func Text(matchType, value string, regex bool) map[string]any {
	return map[string]any{"enabled": true, "matchType": matchType, "regex": value, "useRegex": regex}
}

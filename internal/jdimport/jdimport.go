// Package jdimport reads a JDownloader 2 configuration folder (its cfg
// directory) and turns what KnightLoader can use into KnightLoader's own
// shapes: hoster and debrid accounts, Packagizer and link filter rules, the
// archive password list, the download folder and the open download list.
//
// It only reads. The folder comes in as an fs.FS, a directory or a zip of
// one, and nothing in it is written. Whatever does not map is returned with a
// Reason instead of being dropped, so the preview can say what stays behind.
package jdimport

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// Reason says why something was left out or changed on the way in. Code keys
// a sentence the interface translates, Params fills it, and Text says the same
// in English for logs and other clients.
type Reason struct {
	Code   string            `json:"code"`
	Params map[string]string `json:"params,omitempty"`
	Text   string            `json:"text"`
}

func reason(code, text string, params map[string]string) *Reason {
	return &Reason{Code: code, Params: params, Text: text}
}

// Account is one account from JDownloader's account list. Password never
// leaves the server: it is tagged out of every JSON answer.
type Account struct {
	// Host is the hoster the account is filed under in JDownloader, such as
	// "rapidgator.net" or "real-debrid.com".
	Host     string `json:"host"`
	User     string `json:"user"`
	Password string `json:"-"`
	Enabled  bool   `json:"enabled"`
	// Properties are what JDownloader's plugin stored on the account after
	// logging in. Some keep the API key there.
	Properties map[string]any `json:"-"`
}

// Link is one link of a package in JDownloader's download list. URL is empty
// when the address is not one KnightLoader can fetch.
type Link struct {
	URL       string
	Name      string
	Size      int64
	Enabled   bool
	Finished  bool
	Protected bool
}

// Package is one package of JDownloader's download list.
type Package struct {
	Name    string
	Comment string
	Folder  string
	// Passwords are the archive passwords the links carry, in order.
	Passwords []string
	// DownloadPassword is what a hoster asks for before it hands over a file.
	DownloadPassword string
	Links            []Link
}

// Config is everything read from one cfg folder.
type Config struct {
	Accounts         []Account
	Packagizer       []JDRule
	LinkFilter       []JDRule
	ArchivePasswords []string
	DownloadDir      string
	Packages         []Package
	// Problems are files that were there and could not be read. A file that
	// is simply absent is not a problem: JDownloader writes most of them only
	// once a setting differs from its default.
	Problems []Reason
	// Found names the files that were read, so the preview can say where the
	// import came from.
	Found []string
}

// ErrNoConfig is returned when the folder holds none of the files this
// package reads.
var ErrNoConfig = errors.New("jdimport: no JDownloader configuration found")

// The files read from the cfg folder, by the names JDownloader gives them.
const (
	fileAccounts   = "org.jdownloader.settings.AccountSettings.accounts.ejs"
	fileGeneral    = "org.jdownloader.settings.GeneralSettings.json"
	filePackagizer = "org.jdownloader.controlling.packagizer.PackagizerSettings.rulelist.json"
	fileLinkFilter = "org.jdownloader.controlling.filter.LinkFilterSettings.filterlist.json"
	filePasswords  = "org.jdownloader.extensions.extraction.ExtractionExtension.passwordlist.json"

	// The switches for the whole rule lists, beside the lists.
	filePackagizerSettings = "org.jdownloader.controlling.packagizer.PackagizerSettings.json"
	fileLinkFilterSettings = "org.jdownloader.controlling.filter.LinkFilterSettings.json"
)

// maxFile caps one settings file. JDownloader's own files are kilobytes; a
// download list zip is read entry by entry under maxListBytes instead.
const maxFile = 32 << 20

// Read finds the cfg folder inside fsys and reads every file it knows. fsys
// may be the cfg folder itself, the JDownloader folder above it, or a zip
// holding either at any depth up to a few levels.
func Read(fsys fs.FS) (*Config, error) {
	dir, err := locate(fsys)
	if err != nil {
		return nil, err
	}
	c := &Config{}
	c.readAccounts(fsys, dir)
	c.readGeneral(fsys, dir)
	c.Packagizer = c.readRules(fsys, dir, filePackagizer, filePackagizerSettings, "packagizerenabled")
	c.LinkFilter = c.readRules(fsys, dir, fileLinkFilter, fileLinkFilterSettings, "linkfilterenabled")
	c.readPasswords(fsys, dir)
	c.readDownloadList(fsys, dir)
	return c, nil
}

// isConfigFile reports whether name is one of the files that mark a cfg
// folder.
func isConfigFile(name string) bool {
	switch name {
	case fileAccounts, fileGeneral, filePackagizer, fileLinkFilter, filePasswords:
		return true
	}
	return listNumber(name) >= 0
}

// maxDepth bounds the search for the cfg folder, so a zip of a whole disk is
// not walked to the bottom.
const maxDepth = 4

// locate returns the directory of fsys that holds JDownloader's files. When
// several do, as in a zip of two installs, the one with the most of them wins,
// and a tie goes to the shallowest.
func locate(fsys fs.FS) (string, error) {
	type hit struct {
		dir   string
		count int
		depth int
	}
	counts := map[string]*hit{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable corner of a folder is not a reason to stop.
			if d != nil && d.IsDir() && p != "." {
				return fs.SkipDir
			}
			return nil
		}
		depth := 0
		if p != "." {
			depth = strings.Count(p, "/") + 1
		}
		if d.IsDir() {
			if depth > maxDepth {
				return fs.SkipDir
			}
			return nil
		}
		if !isConfigFile(d.Name()) {
			return nil
		}
		dir := path.Dir(p)
		h := counts[dir]
		if h == nil {
			h = &hit{dir: dir, depth: depth}
			counts[dir] = h
		}
		h.count++
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("jdimport: %w", err)
	}
	if len(counts) == 0 {
		return "", ErrNoConfig
	}
	hits := make([]*hit, 0, len(counts))
	for _, h := range counts {
		hits = append(hits, h)
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].count != hits[j].count {
			return hits[i].count > hits[j].count
		}
		if hits[i].depth != hits[j].depth {
			return hits[i].depth < hits[j].depth
		}
		return hits[i].dir < hits[j].dir
	})
	return hits[0].dir, nil
}

// readFile reads one file of the cfg folder. The second result is false when
// the file is absent; a file that is there and cannot be read is recorded as a
// problem and also returns false.
func (c *Config) readFile(fsys fs.FS, dir, name string) ([]byte, bool) {
	f, err := fsys.Open(path.Join(dir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		c.problem(name, err)
		return nil, false
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err != nil {
		c.problem(name, err)
		return nil, false
	}
	if len(b) > maxFile {
		c.problem(name, fmt.Errorf("it is larger than %d MiB", maxFile>>20))
		return nil, false
	}
	c.Found = append(c.Found, name)
	return b, true
}

func (c *Config) problem(file string, err error) {
	c.Problems = append(c.Problems, Reason{
		Code:   "fileUnreadable",
		Params: map[string]string{"file": file, "error": err.Error()},
		Text:   fmt.Sprintf("%s could not be read: %v", file, err),
	})
}

// ZipFS returns a zip of the folder as a file system. Windows PowerShell 5.1
// writes backslashes into the entry names, which archive/zip does not split
// on, so they are turned into slashes before the first Open.
func ZipFS(zr *zip.Reader) fs.FS {
	for _, f := range zr.File {
		f.Name = strings.ReplaceAll(f.Name, `\`, "/")
	}
	return zr
}

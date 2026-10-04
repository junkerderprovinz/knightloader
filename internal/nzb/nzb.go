// Package nzb reads .nzb files: the list of files a Usenet release consists
// of, and for each the articles its parts were posted in.
package nzb

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// NZB is one parsed .nzb.
type NZB struct {
	// Meta holds the <head> entries, such as "title" and "password", by type.
	Meta  map[string]string
	Files []File
}

// File is one file of the release.
type File struct {
	Subject string
	Poster  string
	// Posted is when the file was posted, zero when the .nzb does not say.
	Posted time.Time
	Groups []string
	// Segments are the file's articles in part order, each number once.
	Segments []Segment
	// Name is taken from the subject, the quoted name in it where there is one.
	Name string
}

// Segment is one article.
type Segment struct {
	Number int
	// Bytes is the article's size as posted, yEnc overhead included.
	Bytes int64
	// ID is the message id without angle brackets.
	ID string
}

// Bytes adds up the file's articles: its size before decoding, a few percent
// above the file's own.
func (f File) Bytes() int64 {
	var n int64
	for _, s := range f.Segments {
		n += s.Bytes
	}
	return n
}

// ErrEmpty is an .nzb that lists no file with an article in it.
var ErrEmpty = errors.New("nzb: the file lists nothing to download")

type xmlNZB struct {
	Head struct {
		Meta []struct {
			Type  string `xml:"type,attr"`
			Value string `xml:",chardata"`
		} `xml:"meta"`
	} `xml:"head"`
	Files []struct {
		Poster   string   `xml:"poster,attr"`
		Date     string   `xml:"date,attr"`
		Subject  string   `xml:"subject,attr"`
		Groups   []string `xml:"groups>group"`
		Segments []struct {
			Bytes  int64  `xml:"bytes,attr"`
			Number int    `xml:"number,attr"`
			ID     string `xml:",chardata"`
		} `xml:"segments>segment"`
	} `xml:"file"`
}

// Parse reads an .nzb. Files without articles are left out, and a segment
// listed twice is kept once. Two files of one name get a number added to the
// second, so neither overwrites the other on disk.
func Parse(data []byte) (*NZB, error) {
	var raw xmlNZB
	d := xml.NewDecoder(bytes.NewReader(data))
	// Indexers write ISO-8859-1 as often as UTF-8; the names that matter are
	// ASCII either way.
	d.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	d.Strict = false
	if err := d.Decode(&raw); err != nil {
		return nil, fmt.Errorf("nzb: %w", err)
	}
	out := &NZB{Meta: map[string]string{}}
	for _, m := range raw.Head.Meta {
		if t := strings.ToLower(strings.TrimSpace(m.Type)); t != "" {
			out.Meta[t] = strings.TrimSpace(m.Value)
		}
	}
	taken := map[string]int{}
	for _, rf := range raw.Files {
		f := File{Subject: strings.TrimSpace(rf.Subject), Poster: strings.TrimSpace(rf.Poster)}
		if sec, err := strconv.ParseInt(strings.TrimSpace(rf.Date), 10, 64); err == nil && sec > 0 {
			f.Posted = time.Unix(sec, 0).UTC()
		}
		for _, g := range rf.Groups {
			if g = strings.TrimSpace(g); g != "" {
				f.Groups = append(f.Groups, g)
			}
		}
		seen := map[int]bool{}
		for _, s := range rf.Segments {
			id := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(s.ID), "<"), ">")
			if id == "" || s.Number < 1 || seen[s.Number] {
				continue
			}
			seen[s.Number] = true
			f.Segments = append(f.Segments, Segment{Number: s.Number, Bytes: max(s.Bytes, 0), ID: id})
		}
		if len(f.Segments) == 0 {
			continue
		}
		slices.SortFunc(f.Segments, func(a, b Segment) int { return a.Number - b.Number })
		f.Name = uniqueName(NameFromSubject(f.Subject), taken)
		out.Files = append(out.Files, f)
	}
	if len(out.Files) == 0 {
		return nil, ErrEmpty
	}
	return out, nil
}

var (
	quoted = regexp.MustCompile(`"([^"]+)"`)
	// A subject without quotes usually still ends in the name, its part count
	// and "yEnc": `Show.S01E01.mkv (1/50) yEnc` or `[3/9] Show.r00 yEnc (1/50)`.
	bare = regexp.MustCompile(`([^\s"\[\]()]+\.[A-Za-z0-9]{1,10})(?:\s*\(\d+/\d+\))?\s+yEnc`)
)

// NameFromSubject finds the file name in a posting's subject: the text in
// quotes where there is any, else the word before "yEnc", else the subject
// itself. It never holds a folder.
func NameFromSubject(subject string) string {
	name := ""
	if m := quoted.FindStringSubmatch(subject); m != nil {
		name = m[1]
	} else if m := bare.FindStringSubmatch(subject); m != nil {
		name = m[1]
	} else {
		name = subject
	}
	name = path.Base(strings.ReplaceAll(strings.TrimSpace(name), `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if r < ' ' || strings.ContainsRune(`<>:"|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, ". ")
	if name == "" || name == "/" {
		return "file"
	}
	return name
}

func uniqueName(name string, taken map[string]int) string {
	key := strings.ToLower(name)
	n := taken[key]
	taken[key] = n + 1
	if n == 0 {
		return name
	}
	ext := path.Ext(name)
	return fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, ext), n+1, ext)
}

var recoveryVolume = regexp.MustCompile(`(?i)\.vol\d+[+-]\d+\.par2$`)

// IsRecoveryVolume reports whether name is a par2 recovery volume, which a
// repair needs and a whole download does not. The index file, name.par2, is
// not one.
func IsRecoveryVolume(name string) bool {
	return recoveryVolume.MatchString(name)
}

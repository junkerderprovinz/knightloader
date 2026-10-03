package jdimport

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// listFile matches JDownloader's download list backups. Every save writes
// downloadList<n+1>.zip, so the highest number is the newest; a plain
// downloadList.zip is the oldest form.
var listFile = regexp.MustCompile(`^downloadList([0-9]*)\.zip$`)

// listNumber ranks a download list file, newest highest: 0 for the plain
// downloadList.zip, n+1 for downloadList<n>.zip, and -1 for any other name.
func listNumber(name string) int {
	m := listFile.FindStringSubmatch(name)
	if m == nil {
		return -1
	}
	if m[1] == "" {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return -1
	}
	return n + 1
}

// maxListBytes caps what one download list may unpack to, so a crafted zip
// cannot fill the memory.
const maxListBytes = 512 << 20

// listEntry is a package ("07") or a link of one ("07_012").
var listEntry = regexp.MustCompile(`^([0-9]+)(?:_([0-9]+))?$`)

type packageStorable struct {
	Name           string         `json:"name"`
	DownloadFolder string         `json:"downloadFolder"`
	Properties     map[string]any `json:"properties"`
}

type linkStorable struct {
	Name           string         `json:"name"`
	URL            string         `json:"url"`
	Size           int64          `json:"size"`
	Enabled        bool           `json:"enabled"`
	FinalLinkState string         `json:"finalLinkState"`
	URLProtection  string         `json:"urlProtection"`
	Properties     map[string]any `json:"properties"`
}

// readDownloadList reads the newest download list that opens, falling back to
// older ones as JDownloader itself does when the newest is damaged.
func (c *Config) readDownloadList(fsys fs.FS, dir string) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && listNumber(e.Name()) >= 0 {
			names = append(names, e.Name())
		}
	}
	sort.Slice(names, func(i, j int) bool { return listNumber(names[i]) > listNumber(names[j]) })
	// Only the newest failure is reported; an older list is a fallback.
	reported := false
	for _, name := range names {
		pkgs, err := readList(fsys, path.Join(dir, name))
		if err == nil {
			c.Packages = pkgs
			c.Found = append(c.Found, name)
			return
		}
		if !reported {
			reported = true
			c.problem(name, err)
		}
	}
}

func readList(fsys fs.FS, name string) ([]Package, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxListBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxListBytes {
		return nil, fmt.Errorf("it is larger than %d MiB", maxListBytes>>20)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}

	type indexed struct {
		pkg   packageStorable
		links map[int]linkStorable
	}
	byIndex := map[int]*indexed{}
	get := func(i int) *indexed {
		if byIndex[i] == nil {
			byIndex[i] = &indexed{links: map[int]linkStorable{}}
		}
		return byIndex[i]
	}
	budget := int64(maxListBytes)
	for _, zf := range zr.File {
		m := listEntry.FindStringSubmatch(zf.Name)
		if m == nil {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(io.LimitReader(rc, budget+1))
		rc.Close()
		if err != nil {
			return nil, err
		}
		budget -= int64(len(raw))
		if budget < 0 {
			return nil, fmt.Errorf("it unpacks to more than %d MiB", maxListBytes>>20)
		}
		pi, _ := strconv.Atoi(m[1])
		if m[2] == "" {
			if err := json.Unmarshal(jsonBytes(raw), &get(pi).pkg); err != nil {
				return nil, fmt.Errorf("%s: %w", zf.Name, err)
			}
			continue
		}
		li, _ := strconv.Atoi(m[2])
		var l linkStorable
		if err := json.Unmarshal(jsonBytes(raw), &l); err != nil {
			return nil, fmt.Errorf("%s: %w", zf.Name, err)
		}
		get(pi).links[li] = l
	}

	order := make([]int, 0, len(byIndex))
	for i := range byIndex {
		order = append(order, i)
	}
	sort.Ints(order)
	out := make([]Package, 0, len(order))
	for _, i := range order {
		in := byIndex[i]
		p := Package{
			Name:    in.pkg.Name,
			Comment: propString(in.pkg.Properties, "COMMENT"),
			Folder:  in.pkg.DownloadFolder,
		}
		seen := map[string]bool{}
		li := make([]int, 0, len(in.links))
		for k := range in.links {
			li = append(li, k)
		}
		sort.Ints(li)
		for _, k := range li {
			l := in.links[k]
			for _, pw := range propStrings(l.Properties, "PWLIST") {
				if !seen[pw] {
					seen[pw] = true
					p.Passwords = append(p.Passwords, pw)
				}
			}
			if p.DownloadPassword == "" {
				p.DownloadPassword = propString(l.Properties, "pass")
			}
			p.Links = append(p.Links, toLink(l))
		}
		out = append(out, p)
	}
	return out, nil
}

func toLink(l linkStorable) Link {
	link := Link{
		Name:     l.Name,
		Size:     l.Size,
		Enabled:  l.Enabled,
		Finished: strings.HasPrefix(l.FinalLinkState, "FINISHED"),
	}
	// A link from a protected container keeps its address encrypted, so the
	// container's owner decides who sees it. It is left encrypted here too.
	if strings.HasPrefix(l.URLProtection, "PROTECTED") || strings.HasPrefix(l.URL, "CRYPTED:") {
		link.Protected = true
		return link
	}
	link.URL = webURL(l.URL)
	if link.URL == "" {
		link.URL = webURL(propString(l.Properties, "URL_CONTENT"))
	}
	return link
}

// webURL turns the address a JDownloader plugin matched into one any client
// can fetch, or returns "" for a plugin's own scheme. JDownloader's direct
// download plugin prefixes plain web addresses with directhttp:// or
// http(s)viajd://.
func webURL(raw string) string {
	raw = strings.TrimSpace(raw)
	lower := strings.ToLower(raw)
	switch {
	case strings.HasPrefix(lower, "directhttp://"):
		raw = raw[len("directhttp://"):]
		if l := strings.ToLower(raw); !strings.HasPrefix(l, "http://") && !strings.HasPrefix(l, "https://") {
			raw = "http://" + raw
		}
	case strings.HasPrefix(lower, "httpviajd://"):
		raw = "http://" + raw[len("httpviajd://"):]
	case strings.HasPrefix(lower, "httpsviajd://"):
		raw = "https://" + raw[len("httpsviajd://"):]
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "ftp", "ftps", "sftp":
		return raw
	}
	return ""
}

func propString(props map[string]any, key string) string {
	s, _ := props[key].(string)
	return strings.TrimSpace(s)
}

// propStrings reads a property JDownloader stores as one string or as a list.
func propStrings(props map[string]any, key string) []string {
	var out []string
	switch v := props[key].(type) {
	case string:
		if s := strings.TrimSpace(v); s != "" {
			out = append(out, s)
		}
	case []any:
		for _, e := range v {
			if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	}
	return out
}

// Links is what a package brings into the collector.
type Links struct {
	URLs  []string
	Notes []Reason
}

// PackageLinks lists the links of a package that are left to download, and
// says how many were left out and why.
func PackageLinks(p Package) Links {
	var out Links
	var finished, off, protected, internal int
	seen := map[string]bool{}
	for _, l := range p.Links {
		switch {
		case l.Finished:
			finished++
		case !l.Enabled:
			off++
		case l.Protected:
			protected++
		case l.URL == "":
			internal++
		case !seen[l.URL]:
			seen[l.URL] = true
			out.URLs = append(out.URLs, l.URL)
		}
	}
	note := func(n int, code, one, many string) {
		if n == 0 {
			return
		}
		text := one
		if n > 1 {
			text = fmt.Sprintf(many, n)
		}
		out.Notes = append(out.Notes, Reason{Code: code, Params: map[string]string{"n": strconv.Itoa(n)}, Text: text})
	}
	note(finished, "linksFinished",
		"1 finished link is left out.",
		"%d finished links are left out.")
	note(off, "linksOff",
		"1 link switched off in JDownloader is left out.",
		"%d links switched off in JDownloader are left out.")
	note(protected, "linksProtected",
		"1 link comes from a protected container whose address JDownloader keeps hidden. Add the container again.",
		"%d links come from a protected container whose addresses JDownloader keeps hidden. Add the container again.")
	note(internal, "linksInternal",
		"1 link has an address only JDownloader's plugins understand and is left out.",
		"%d links have an address only JDownloader's plugins understand and are left out.")
	return out
}

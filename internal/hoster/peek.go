package hoster

import (
	"context"
	"io"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// maxPage caps how much of a page Peek keeps. The pages the clients read for
// a confirmation form or an error are well below it.
const maxPage = 1 << 20

// Answer is what a link answered when asked for its first byte.
type Answer struct {
	// URL is where the redirects ended.
	URL    *url.URL
	Status int
	Header http.Header
	// Name and Size describe the file. Both stay empty for a page.
	Name string
	Size int64
	// Page is the start of an answer that is a web page rather than the file.
	Page []byte
}

// Session copies hc with a cookie jar of its own, for a hoster that keeps a
// visitor's access to a share in cookies.
func Session(hc *http.Client) *http.Client {
	jar, _ := cookiejar.New(nil)
	s := *hc
	s.Jar = jar
	return &s
}

// File reports whether the answer is the file itself.
func (a *Answer) File() bool { return a.Page == nil && a.Status/100 == 2 }

// Peek asks for the first byte of link, following redirects through hc, and
// reads the file's name and size from the answer, so a cloud share can be
// named and sized without downloading it. An answer in HTML that is not sent
// as an attachment comes back as a page instead.
func Peek(ctx context.Context, hc *http.Client, link string, header http.Header) (*Answer, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	a := &Answer{URL: resp.Request.URL, Status: resp.StatusCode, Header: resp.Header}
	disp, params, _ := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if resp.StatusCode/100 != 2 || (disp != "attachment" && (ct == "text/html" || ct == "application/json")) {
		a.Page, err = io.ReadAll(io.LimitReader(resp.Body, maxPage))
		if a.Page == nil {
			a.Page = []byte{}
		}
		return a, err
	}
	a.Name = params["filename"]
	if a.Name == "" {
		a.Name = path.Base(a.URL.Path)
	}
	a.Size = max(resp.ContentLength, 0)
	if _, total, ok := strings.Cut(resp.Header.Get("Content-Range"), "/"); ok {
		a.Size, _ = strconv.ParseInt(total, 10, 64)
	}
	return a, nil
}

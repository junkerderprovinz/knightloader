package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// logo is a picture a page shows as its logo: an address to fetch, or the
// bytes themselves for an inline SVG or a data: URL.
type logo struct {
	url  string
	body []byte
}

// logoTries is how many of a page's logos are fetched before giving up.
const logoTries = 3

// pageLogo finds host's logo near the top of its front page.
func pageLogo(host string) (icon, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	page, base, err := frontPage(ctx, host)
	if err != nil {
		return icon{}, "", err
	}
	// A page on another site, such as a parking service or a seizure notice,
	// shows that site's logo.
	if !sameSite([]string{host, strings.TrimPrefix(strings.ToLower(base.Hostname()), "www.")}) {
		return icon{}, "", fmt.Errorf("the front page is on %s", base.Hostname())
	}
	logos := headerLogos(page, base)
	if len(logos) == 0 {
		return icon{}, "", errors.New("no logo in the page")
	}
	var errs []error
	for _, l := range logos[:min(len(logos), logoTries)] {
		body := l.body
		if body == nil {
			if body, err = get(ctx, l.url); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		ic, err := normalise(body)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		return ic, "header logo", nil
	}
	return icon{}, "", errors.Join(errs...)
}

// frontPage fetches host's front page over https, then on its www. name and
// then over plain http, and returns it with the address it ended up at.
func frontPage(ctx context.Context, host string) ([]byte, *url.URL, error) {
	var last error
	for _, origin := range []string{"https://" + host, "https://www." + host, "http://" + host} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/", nil)
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("User-Agent", browserAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml")
		resp, err := serviceClient.Do(req)
		if err != nil {
			last = err
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		switch {
		case err != nil:
			last = err
		case resp.StatusCode != http.StatusOK:
			last = fmt.Errorf("%s: %s", origin, resp.Status)
		default:
			return body, resp.Request.URL, nil
		}
	}
	return nil, nil, last
}

// headerLogos returns the logos in page in the order it shows them: every
// image and inline SVG inside a header, a nav or an element whose class or
// id says logo, and every image whose address, alt text, class or id says
// so.
func headerLogos(page []byte, base *url.URL) []logo {
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return nil
	}
	var out []logo
	var walk func(n *html.Node, near bool)
	walk = func(n *html.Node, near bool) {
		if n.Type == html.ElementNode {
			near = near || n.DataAtom == atom.Header || n.DataAtom == atom.Nav || saysLogo(attr(n, "class"), attr(n, "id"))
			switch n.DataAtom {
			case atom.Img:
				src := imageSource(n)
				if near || saysLogo(src, attr(n, "alt")) {
					if l, ok := imageLogo(src, base); ok {
						out = append(out, l)
					}
				}
				return
			case atom.Svg:
				if near {
					if l, ok := inlineSVG(n); ok {
						out = append(out, l)
					}
				}
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, near)
		}
	}
	walk(doc, false)
	return out
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func saysLogo(values ...string) bool {
	for _, v := range values {
		if strings.Contains(strings.ToLower(v), "logo") {
			return true
		}
	}
	return false
}

// imageSource is the address an image shows once loaded. A lazily loaded
// one keeps it in data-src and a placeholder in src.
func imageSource(n *html.Node) string {
	for _, key := range []string{"data-src", "data-lazy-src", "src"} {
		if v := strings.TrimSpace(attr(n, key)); v != "" {
			return v
		}
	}
	return ""
}

// imageLogo turns an image's address into a logo: the bytes of a data: URL,
// or an http(s) address resolved against the page.
func imageLogo(src string, base *url.URL) (logo, bool) {
	if meta, data, ok := strings.Cut(src, ","); ok && strings.HasPrefix(strings.ToLower(meta), "data:image/") {
		var body []byte
		var err error
		if strings.HasSuffix(strings.ToLower(meta), ";base64") {
			body, err = base64.StdEncoding.DecodeString(strings.Join(strings.Fields(data), ""))
		} else {
			var s string
			s, err = url.PathUnescape(data)
			body = []byte(s)
		}
		return logo{body: body}, err == nil && len(body) > 0
	}
	ref, err := url.Parse(src)
	if err != nil || src == "" {
		return logo{}, false
	}
	abs := base.ResolveReference(ref)
	if abs.Scheme != "http" && abs.Scheme != "https" {
		return logo{}, false
	}
	return logo{url: abs.String()}, true
}

// inlineSVG writes an SVG from the page as a document of its own. One that
// draws with <use> takes its shapes from elsewhere in the page and comes out
// empty.
func inlineSVG(n *html.Node) (logo, bool) {
	var buf bytes.Buffer
	if err := html.Render(&buf, n); err != nil {
		return logo{}, false
	}
	svg := buf.String()
	if strings.Contains(svg, "<use") {
		return logo{}, false
	}
	if !strings.Contains(svg, "xmlns=") {
		svg = strings.Replace(svg, "<svg", `<svg xmlns="http://www.w3.org/2000/svg"`, 1)
	}
	return logo{body: []byte(svg)}, true
}

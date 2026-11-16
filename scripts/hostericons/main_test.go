package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"slices"
	"strings"
	"testing"
)

func TestAnImportNamedWithoutItsTLDGoesToTheOneMissingHost(t *testing.T) {
	missing := []string{"analoverdose.com", "cnubis.com", "foo.com", "foo.net", "genericusenet"}
	for name, want := range map[string]string{
		"analoverdose.png": "analoverdose.com",
		"cnubis.com":       "cnubis.com",
		"Cnubis.com.PNG":   "cnubis.com",
		"www.cnubis.com":   "cnubis.com",
		"foo.svg":          "",
		"elsewhere.png":    "",
		"rapidgator.net":   "rapidgator.net",
	} {
		if got := importHost(name, missing); got != want {
			t.Errorf("importHost(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestHeaderLogosComeInTheOrderThePageShowsThem(t *testing.T) {
	page := []byte(`<html><head><link rel="icon" href="/favicon.ico"></head><body>
<div class="banner"><img src="/ad.png" alt="Sale"></div>
<header><nav><a href="/"><img src="/img/brand.png" alt="Home"></a></nav></header>
<main><img src="/photo.jpg" alt="A file"><img data-src="//cdn.example.org/site-logo.webp" src="data:image/gif;base64,R0lGODlhAQABAAAAACw="></main>
<div id="logo"><svg viewBox="0 0 10 10"><path d="M0 0h10v10z"/></svg></div>
</body></html>`)
	base, _ := url.Parse("https://www.example.org/start")
	logos := headerLogos(page, base)

	var got []string
	for _, l := range logos {
		if l.body != nil {
			got = append(got, "inline")
		} else {
			got = append(got, l.url)
		}
	}
	want := []string{"https://www.example.org/img/brand.png", "https://cdn.example.org/site-logo.webp", "inline"}
	if !slices.Equal(got, want) {
		t.Fatalf("headerLogos = %q, want %q", got, want)
	}
	if svg := string(logos[2].body); !strings.Contains(svg, `xmlns="http://www.w3.org/2000/svg"`) || !strings.Contains(svg, `<path`) {
		t.Errorf("inline SVG = %s, want a standalone document with its paths", svg)
	}
}

func TestAnInlineSVGThatBorrowsFromThePageIsNotALogo(t *testing.T) {
	page := []byte(`<header><svg class="logo"><use href="#sprite-logo"></use></svg></header>`)
	base, _ := url.Parse("https://example.org/")
	if logos := headerLogos(page, base); len(logos) != 0 {
		t.Errorf("headerLogos = %+v, want none for an SVG drawn from a sprite elsewhere", logos)
	}
}

func TestAWideLogoIsFittedIntoTheSquare(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 200, 50))
	for y := range 50 {
		for x := range 200 {
			src.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: 40, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	ic, err := normalise(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(ic.data))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != size || b.Dy() != size {
		t.Fatalf("icon is %v, want %dx%d", b, size, size)
	}
	if _, _, _, a := img.At(size/2, 2).RGBA(); a != 0 {
		t.Error("the band above a wide logo is not transparent")
	}
	if _, _, _, a := img.At(size/2, size/2).RGBA(); a == 0 {
		t.Error("the middle of a wide logo is empty")
	}
}

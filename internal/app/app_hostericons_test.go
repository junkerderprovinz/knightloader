package app

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// allowLocalIcons lets the icon dialer reach httptest's 127.0.0.1 for one test.
func allowLocalIcons(t *testing.T) {
	t.Helper()
	prev := iconDialAllowed
	iconDialAllowed = func(net.IP) bool { return true }
	t.Cleanup(func() { iconDialAllowed = prev })
}

func parseIPForTest(t *testing.T, s string) net.IP {
	t.Helper()
	ip := net.ParseIP(s)
	if ip == nil {
		t.Fatalf("%q is not an address this test can use", s)
	}
	return ip
}

// icoBytes builds an .ico header and directory with one entry per size and no
// image data, which icoLargestFrame never reads. A width byte of 0 means 256.
func icoBytes(sizes ...int) []byte {
	out := make([]byte, 6+16*len(sizes))
	binary.LittleEndian.PutUint16(out[2:4], 1) // type: icon
	binary.LittleEndian.PutUint16(out[4:6], uint16(len(sizes)))
	for i, s := range sizes {
		off := 6 + i*16
		out[off] = byte(s % 256)
		out[off+1] = byte(s % 256)
	}
	return out
}

// pngBytes is a PNG signature and IHDR chunk only, which is all
// image.DecodeConfig reads. The CRC is real because the decoder checks it.
func pngBytes(w, h int) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], uint32(w))
	binary.BigEndian.PutUint32(ihdr[4:8], uint32(h))
	ihdr[8], ihdr[9], ihdr[10], ihdr[11], ihdr[12] = 8, 6, 0, 0, 0 // 8-bit RGBA

	b := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, uint32(len(ihdr)))
	b = append(b, length...)
	b = append(b, []byte("IHDR")...)
	b = append(b, ihdr...)
	sum := make([]byte, 4)
	binary.BigEndian.PutUint32(sum, crc32.ChecksumIEEE(append([]byte("IHDR"), ihdr...)))
	return append(b, sum...)
}

func TestIcoLargestFrameReadsTheDirectoryNotTheFirstEntry(t *testing.T) {
	// rapidgator.net serves a .ico whose first frame is 16x16 and second 32x32.
	if got := icoLargestFrame(icoBytes(16, 32)); got != 32 {
		t.Errorf("icoLargestFrame(16,32) = %d, want 32", got)
	}
	if got := icoLargestFrame(icoBytes(48)); got != 48 {
		t.Errorf("icoLargestFrame(48) = %d, want 48", got)
	}
	if got := icoLargestFrame([]byte("not an icon")); got != 0 {
		t.Errorf("icoLargestFrame(garbage) = %d, want 0", got)
	}
	if got := icoLargestFrame(icoBytes(16, 32)[:7]); got != 0 {
		t.Errorf("icoLargestFrame(header only) = %d, want 0", got)
	}
	// A truncated directory still answers with the entries it has.
	if got := icoLargestFrame(icoBytes(16, 32)[:22]); got != 16 {
		t.Errorf("icoLargestFrame(first entry only) = %d, want 16", got)
	}
}

func TestPublicIPRefusesEverythingLocal(t *testing.T) {
	// An icon href comes from someone else's HTML, so this guard keeps it from
	// reaching into the local network.
	for _, addr := range []string{
		"127.0.0.1", "::1", "10.0.0.5", "192.168.20.46", "172.16.4.4",
		"169.254.1.1", "100.64.0.1", "100.127.255.254", "0.0.0.0", "224.0.0.1",
	} {
		if publicIP(parseIPForTest(t, addr)) {
			t.Errorf("publicIP(%s) = true, want false since that address is inside somebody's network", addr)
		}
	}
	for _, addr := range []string{"1.1.1.1", "93.184.216.34", "2606:4700::1111", "100.63.255.255", "100.128.0.1"} {
		if !publicIP(parseIPForTest(t, addr)) {
			t.Errorf("publicIP(%s) = false, want true for an ordinary public address", addr)
		}
	}
}

// TestDeclaredIconsFindsAnIconOnAnotherHost: alldebrid.com returns 404 for both
// well-known paths and declares its icon on a CDN.
func TestDeclaredIconsFindsAnIconOnAnotherHost(t *testing.T) {
	allowLocalIcons(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head>
			<link rel="stylesheet" href="/style.css">
			<link rel="shortcut icon" type="image/png" href="https://cdn.example.org/lib/favicon.png">
			<link rel="apple-touch-icon" href="/touch.png">
			<link rel="icon" sizes="32x32" href="/small.png">
		</head><body></body></html>`)
	}))
	defer srv.Close()

	got := declaredIcons(context.Background(), srv.URL+"/")
	if len(got) != 3 {
		t.Fatalf("declaredIcons found %d candidates, want 3: %+v", len(got), got)
	}
	// Largest declared size first; an apple-touch-icon without sizes counts
	// as 180.
	if !strings.HasSuffix(got[0].url, "/touch.png") {
		t.Errorf("first candidate = %q, want the apple-touch-icon", got[0].url)
	}
	if !strings.HasSuffix(got[1].url, "/small.png") || got[1].declared != 32 {
		t.Errorf("second candidate = %q (declared %d), want /small.png at 32", got[1].url, got[1].declared)
	}
	if got[2].url != "https://cdn.example.org/lib/favicon.png" {
		t.Errorf("cross-host candidate = %q, want it kept verbatim", got[2].url)
	}
	for _, c := range got {
		if strings.Contains(c.url, "style.css") {
			t.Errorf("a stylesheet was taken for an icon: %q", c.url)
		}
	}
}

// TestFetchFaviconKeepsTheLargestImage: /favicon.ico usually answers first and
// is usually the smallest image a site has.
func TestFetchFaviconKeepsTheLargestImage(t *testing.T) {
	allowLocalIcons(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><head><link rel="icon" href="/tiny.png"><link rel="icon" sizes="128x128" href="/big.png"></head></html>`)
		case "/tiny.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes(16, 16))
		case "/big.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes(128, 128))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	body, ct, err := fetchIconsFrom(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetchIconsFrom: %v", err)
	}
	if ct != "image/png" {
		t.Fatalf("content type = %q, want image/png", ct)
	}
	if got := iconPixelSize(body, ct); got != 128 {
		t.Errorf("kept an icon of %dpx, want the 128px one", got)
	}
}

// TestFetchFaviconRefusesAnUnservableType: an SVG can carry script, and the
// icon is served from the instance's own origin.
func TestFetchFaviconRefusesAnUnservableType(t *testing.T) {
	allowLocalIcons(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><head><link rel="icon" href="/icon.svg"></head></html>`)
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		fmt.Fprint(w, `<svg xmlns="http://www.w3.org/2000/svg"></svg>`)
	}))
	defer srv.Close()

	if _, _, err := fetchIconsFrom(context.Background(), srv.URL); err == nil {
		t.Fatal("fetchIconsFrom accepted an SVG, want it refused")
	}
}

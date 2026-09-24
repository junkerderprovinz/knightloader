package app

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
			<link rel="mask-icon" href="/pinned.svg" color="#de2600">
		</head><body></body></html>`)
	}))
	defer srv.Close()

	got, _, err := declaredIcons(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
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
		if strings.Contains(c.url, "pinned.svg") {
			t.Errorf("Safari's one-colour mask-icon was taken for an icon: %q", c.url)
		}
	}
}

// TestDeclaredIconsDecodesCharacterReferences: an href may write a slash as
// &#x2F; and & as &amp;, while a bare & before a parameter that starts like an
// entity name stays as it is.
func TestDeclaredIconsDecodesCharacterReferences(t *testing.T) {
	allowLocalIcons(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head>
			<link href="&#x2F;assets&#x2F;icons&#x2F;favicon-152.png" rel="apple-touch-icon" sizes="152x152">
			<link rel="icon" sizes="32x32" href="/icon.png?v=2&amp;theme=light">
			<link rel="icon" sizes="24x24" href="/regional.png?a=1&region=eu">
			<link rel=icon sizes=16x16 href=/minified.ico>
		</head></html>`)
	}))
	defer srv.Close()

	got, _, err := declaredIcons(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		srv.URL + "/assets/icons/favicon-152.png",
		srv.URL + "/icon.png?v=2&theme=light",
		srv.URL + "/regional.png?a=1&region=eu",
		srv.URL + "/minified.ico",
	}
	if len(got) != len(want) {
		t.Fatalf("declaredIcons found %+v, want %v", got, want)
	}
	for i := range want {
		if got[i].url != want[i] {
			t.Errorf("candidate %d = %q, want %q", i, got[i].url, want[i])
		}
	}
}

// TestDeclaredIconsReadsTheWebAppManifest: a manifest's relative src resolves
// against the manifest rather than the page, and a monochrome icon is skipped.
func TestDeclaredIconsReadsTheWebAppManifest(t *testing.T) {
	allowLocalIcons(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><head>
				<link rel="shortcut icon" href="/favicon.ico">
				<link rel="manifest" href="/app/manifest.json">
			</head></html>`)
		case "/app/manifest.json":
			fmt.Fprint(w, `{"icons":[
				{"src":"icons/192.png","sizes":"192x192","purpose":"any maskable"},
				{"src":"icons/mono.png","sizes":"512x512","purpose":"monochrome"},
				{"src":"icons/512.png","sizes":"256x256 512x512"}
			]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	got, _, err := declaredIcons(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	want := []iconCandidate{
		{url: srv.URL + "/favicon.ico"},
		{url: srv.URL + "/app/icons/512.png", declared: 512},
		{url: srv.URL + "/app/icons/192.png", declared: 192},
	}
	if len(got) != len(want) {
		t.Fatalf("declaredIcons found %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("candidate %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestFailedDeclaredIconsLeaveRoomForTheFavicon: a page that declares a dozen
// sizes that all 404 still gets its working /favicon.ico tried.
func TestFailedDeclaredIconsLeaveRoomForTheFavicon(t *testing.T) {
	allowLocalIcons(t)
	var imageRequests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<html><head>")
			for size := 16; size <= 256; size += 16 {
				fmt.Fprintf(w, `<link rel="apple-touch-icon" sizes="%dx%d" href="/missing-%d.png">`, size, size, size)
			}
			fmt.Fprint(w, "</head></html>")
			return
		}
		imageRequests.Add(1)
		if r.URL.Path == "/favicon.ico" {
			_, _ = w.Write(icoBytes(16, 32))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	body, ct, err := fetchIconsFrom(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetchIconsFrom gave up before /favicon.ico: %v", err)
	}
	if iconTypes[ct] != "ico" || iconPixelSize(body, ct) != 32 {
		t.Errorf("got a %s of %dpx, want the 32px favicon.ico", ct, iconPixelSize(body, ct))
	}
	if n := imageRequests.Load(); n > iconMaxAttempts {
		t.Errorf("asked the host for %d images, want at most %d", n, iconMaxAttempts)
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

// TestFetchFaviconGoesByTheBytesNotTheLabel: a PNG sent as image/x-icon would
// otherwise be measured as a broken .ico.
func TestFetchFaviconGoesByTheBytesNotTheLabel(t *testing.T) {
	allowLocalIcons(t)
	srv := iconSite(t, "", map[string]servedFile{"/favicon.ico": {"image/x-icon", pngBytes(48, 48)}})
	body, ct, err := fetchIconsFrom(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetchIconsFrom: %v", err)
	}
	if ct != "image/png" || iconPixelSize(body, ct) != 48 {
		t.Errorf("got a %s of %dpx, want the 48px PNG it really is", ct, iconPixelSize(body, ct))
	}
}

// servedFile is one response of iconSite.
type servedFile struct {
	contentType string
	body        []byte
}

// iconSite serves a front page with head inside its <head>, the given files,
// and a 404 for everything else.
func iconSite(t *testing.T, head string, files map[string]servedFile) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<html><head>"+head+"</head></html>")
			return
		}
		f, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", f.contentType)
		_, _ = w.Write(f.body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

const svgIcon = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><rect width="16" height="16"/></svg>`

// TestFetchFaviconAcceptsAnSVG: servers label an SVG as anything from
// image/svg+xml to text/plain.
func TestFetchFaviconAcceptsAnSVG(t *testing.T) {
	allowLocalIcons(t)
	cases := map[string]servedFile{
		"labelled": {"image/svg+xml", []byte(svgIcon)},
		"with a prolog": {"application/octet-stream", []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!-- Generator: Sketch -->
<!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.1//EN" "http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd">
` + svgIcon)},
		"with a byte order mark": {"text/plain", []byte("\xef\xbb\xbf\n" + svgIcon)},
	}
	for name, file := range cases {
		srv := iconSite(t, `<link rel="icon" type="image/svg+xml" href="/icon.svg">`,
			map[string]servedFile{"/icon.svg": file})
		body, ct, err := fetchIconsFrom(context.Background(), srv.URL)
		if err != nil {
			t.Errorf("%s: fetchIconsFrom refused the SVG: %v", name, err)
			continue
		}
		if ct != "image/svg+xml" || string(body) != string(file.body) {
			t.Errorf("%s: got %q as %s, want the SVG as image/svg+xml", name, body, ct)
		}
	}
}

// TestFetchFaviconRefusesAnHTMLPageLabelledSVG: an SVG ends the search, so an
// error page sent as image/svg+xml would hide every icon after it.
func TestFetchFaviconRefusesAnHTMLPageLabelledSVG(t *testing.T) {
	allowLocalIcons(t)
	srv := iconSite(t, `<link rel="icon" href="/icon.svg">`, map[string]servedFile{
		"/icon.svg": {"image/svg+xml", []byte(`<!DOCTYPE html><html><body>` + svgIcon + `</body></html>`)},
	})
	if _, ct, err := fetchIconsFrom(context.Background(), srv.URL); err == nil {
		t.Fatalf("fetchIconsFrom took an HTML page for an icon (%s)", ct)
	}
}

// TestAnSVGIconOnlyBeatsASmallRaster covers pages that offer both, with the
// raster found first.
func TestAnSVGIconOnlyBeatsASmallRaster(t *testing.T) {
	allowLocalIcons(t)
	files := map[string]servedFile{
		"/icon.svg":  {"image/svg+xml", []byte(svgIcon)},
		"/small.png": {"image/png", pngBytes(16, 16)},
		"/big.png":   {"image/png", pngBytes(180, 180)},
	}
	cases := []struct{ head, want string }{
		{`<link rel="icon" href="/small.png"><link rel="icon" href="/icon.svg">`, "image/svg+xml"},
		{`<link rel="icon" href="/big.png"><link rel="icon" href="/icon.svg">`, "image/png"},
	}
	for _, c := range cases {
		srv := iconSite(t, c.head, files)
		_, ct, err := fetchIconsFrom(context.Background(), srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		if ct != c.want {
			t.Errorf("for %s kept the %s, want the %s", c.head, ct, c.want)
		}
	}
}

// TestFetchIconsFromFallsBackToTheNextOrigin: an origin that does not answer,
// or answers without an icon, hands over to the next, and the first one with
// an icon ends the search.
func TestFetchIconsFromFallsBackToTheNextOrigin(t *testing.T) {
	allowLocalIcons(t)
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	iconless := iconSite(t, "", nil)
	working := iconSite(t, "", map[string]servedFile{"/favicon.ico": {"image/x-icon", icoBytes(32)}})
	later := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("asked for %s after an earlier origin had an icon", r.URL.Path)
	}))
	defer later.Close()

	body, ct, err := fetchIconsFrom(context.Background(), dead.URL, iconless.URL, working.URL, later.URL)
	if err != nil {
		t.Fatalf("fetchIconsFrom: %v", err)
	}
	if iconPixelSize(body, ct) != 32 {
		t.Errorf("got a %s of %dpx, want the working origin's 32px favicon.ico", ct, iconPixelSize(body, ct))
	}
}

func TestIconOriginsPutHTTPSFirstAndPlainHTTPLast(t *testing.T) {
	cases := map[string][]string{
		"jianguoyun.com":  {"https://jianguoyun.com", "https://www.jianguoyun.com", "http://jianguoyun.com"},
		"www.example.org": {"https://www.example.org", "http://www.example.org"},
	}
	for host, want := range cases {
		if got := iconOrigins(host); !slices.Equal(got, want) {
			t.Errorf("iconOrigins(%q) = %v, want %v", host, got, want)
		}
	}
}

// TestWellKnownIconPathsFollowTheFrontPageRedirect: a bare name that only
// redirects its front page to www. has no /favicon.ico of its own.
func TestWellKnownIconPathsFollowTheFrontPageRedirect(t *testing.T) {
	allowLocalIcons(t)
	www := iconSite(t, "", map[string]servedFile{"/favicon.ico": {"image/x-icon", icoBytes(32)}})
	bare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, www.URL+"/", http.StatusMovedPermanently)
			return
		}
		http.NotFound(w, r)
	}))
	defer bare.Close()

	if _, _, err := fetchIconsFrom(context.Background(), bare.URL); err != nil {
		t.Fatalf("fetchIconsFrom did not look beside the page it was sent to: %v", err)
	}
}

// TestAPageClosedMidFetchDoesNotHideTheIcon: a miss is remembered for hours,
// and a browser that leaves the page cancels the icon requests it had open.
func TestAPageClosedMidFetchDoesNotHideTheIcon(t *testing.T) {
	a := &App{DataDir: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := a.HosterIcon(ctx, "example.org"); err == nil {
		t.Fatal("HosterIcon found an icon with a cancelled context")
	}
	if a.icons["example.org"].missing {
		t.Error("a cancelled request was remembered as a site without an icon")
	}
}

func TestAnIconFetchedBeforeARestartIsReadFromDisk(t *testing.T) {
	a := &App{DataDir: t.TempDir()}
	dir := filepath.Join(a.DataDir, "icons")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := []byte("\x89PNG\r\n\x1a\nstored")
	if err := os.WriteFile(filepath.Join(dir, iconFileBase("example.org")+".png"), want, 0o644); err != nil {
		t.Fatal(err)
	}
	// A cancelled context makes any network fetch fail, so an answer can only
	// come from the file.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, ct, err := a.HosterIcon(ctx, "example.org")
	if err != nil {
		t.Fatalf("HosterIcon = %v, want the stored icon", err)
	}
	if string(got) != string(want) || ct != "image/png" {
		t.Errorf("HosterIcon = %q (%s), want the stored PNG", got, ct)
	}
}

func TestTheNewerOfTwoStoredIconsIsReadBack(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, iconFileBase("example.org"))
	older, newer := base+".png", base+".svg"
	if err := os.WriteFile(older, []byte("old png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte("<svg/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	hourAgo := time.Now().Add(-time.Hour)
	if err := os.Chtimes(older, hourAgo, hourAgo); err != nil {
		t.Fatal(err)
	}
	// Map order changes from run to run, so one lucky pass proves nothing.
	for range 20 {
		_, ct, _, found := readCachedIcon(dir, "example.org")
		if !found || ct != "image/svg+xml" {
			t.Fatalf("readCachedIcon = %q (found %v), want the newer SVG", ct, found)
		}
	}
}

func TestParkingAndSeizurePagesAreNotAHostersIcon(t *testing.T) {
	for host, want := range map[string]bool{
		"www.hugedomains.com":         true,
		"forsale.dynadot.com":         true,
		"www.alliance4creativity.com": true,
		"rapidgator.net":              false,
		"notdan.com":                  false,
	} {
		if got := parkedHost(host); got != want {
			t.Errorf("parkedHost(%q) = %v, want %v", host, got, want)
		}
	}
}

// The address check runs on the address being connected, after the name has
// been resolved, so a name that points into the local network is refused just
// as its address written out would be.
func TestTheIconDialerRefusesANameThatResolvesToTheLocalNetwork(t *testing.T) {
	var reached atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Store(true)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nlocal"))
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())

	_, _, err := fetchIconAt(context.Background(), "http://localhost:"+port+"/icon.png")
	if err == nil || !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("fetchIconAt = %v, want the local address refused", err)
	}
	if reached.Load() {
		t.Error("the request reached the local server")
	}
}

// The www. and plain http addresses usually redirect to the one front page. A
// page reached again that way has had its allowance, so a site that declares
// dozens of missing icons still costs at most iconMaxAttempts image requests.
func TestOriginsThatRedirectToTheSamePageShareOneAllowance(t *testing.T) {
	allowLocalIcons(t)
	var images atomic.Int32
	var links strings.Builder
	for i := range 30 {
		fmt.Fprintf(&links, `<link rel="icon" sizes="%dx%d" href="/missing-%d.png">`, 200-i, 200-i, i)
	}
	main := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, "<html><head>%s</head></html>", links.String())
			return
		}
		images.Add(1)
		http.NotFound(w, r)
	}))
	defer main.Close()
	redirect := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, main.URL+"/", http.StatusMovedPermanently)
		}))
	}
	www, plain := redirect(), redirect()
	defer www.Close()
	defer plain.Close()

	if _, _, err := fetchIconsFrom(context.Background(), main.URL, www.URL, plain.URL); err == nil {
		t.Fatal("fetchIconsFrom found an icon on a site that has none")
	}
	if n := images.Load(); n > iconMaxAttempts {
		t.Errorf("%d image requests, want at most %d", n, iconMaxAttempts)
	}
}

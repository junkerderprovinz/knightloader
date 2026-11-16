// Command hostericons writes the hoster icons that ship in the binary
// (assets/hoster-icons/set): one 64-pixel PNG, or a small SVG, for every host
// the "add a login" picker offers that has no logo in the page already
// (web/src/lib/hosterLogos.ts) and no override from the owner.
//
//	go run ./scripts/hostericons -hosts http://<instance>/api/hosterauth/hosts
//	go run ./scripts/hostericons -hosts hosts.json -import <folder of <host>.png|svg>
//
// Run it from the repository root. -hosts is the picker's list as that route
// serves it, from an instance whose JD answers, since only then is it JD's
// full list. Each icon comes from the site itself through the same fetch an
// instance runs, or from Google's or DuckDuckGo's icon service where that
// one is larger: the services see only the public list of hosters here,
// never an instance's own. Icons already in the set are kept unless -refresh
// is given. With -import the folder's files are normalised into the
// overrides, which win over the set. The hosts left without an icon are
// printed, one per line.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"  // registers the GIF decoder
	_ "image/jpeg" // registers the JPEG decoder
	"image/png"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	_ "golang.org/x/image/bmp" // registers the BMP decoder
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // registers the WebP decoder

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// size is the edge of every shipped raster icon. The page shows an icon at 18
// pixels and a picker tile at up to 40, so 64 is sharp at any density.
const size = 64

// svgMaxBytes is the largest SVG shipped as it is. A larger one is usually a
// whole illustration or a raster wrapped in SVG.
const svgMaxBytes = 16 << 10

func main() {
	hostsFrom := flag.String("hosts", "", "the picker's host list: a URL of /api/hosterauth/hosts or a file it was saved to")
	token := flag.String("token", "", "an API token for -hosts, where the instance asks for one")
	importDir := flag.String("import", "", "a folder of <host>.<ext> logos to normalise into the overrides")
	refresh := flag.Bool("refresh", false, "fetch every icon again, not only the missing ones")
	out := flag.String("out", filepath.Join("assets", "hoster-icons"), "the folder holding overrides and set")
	marks := flag.String("marks", filepath.Join("web", "src", "lib", "hosterLogos.ts"), "the page's list of bundled logos")
	flag.Parse()
	if *hostsFrom == "" {
		flag.Usage()
		os.Exit(2)
	}
	log.SetFlags(0)

	hosts, err := pickerHosts(*hostsFrom, *token)
	if err != nil {
		log.Fatal(err)
	}
	bundled, err := bundledLogos(*marks)
	if err != nil {
		log.Fatal(err)
	}
	overrideDir, setDir := filepath.Join(*out, "overrides"), filepath.Join(*out, "set")
	if *importDir != "" {
		n, err := importOverrides(*importDir, overrideDir)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("%d overrides imported from %s", n, *importDir)
	}
	overrides, err := iconNames(overrideDir)
	if err != nil {
		log.Fatal(err)
	}
	kept, err := iconNames(setDir)
	if err != nil {
		log.Fatal(err)
	}

	var wanted []string
	for _, h := range hosts {
		if !covered(h, bundled) && !covered(h, overrides) {
			wanted = append(wanted, h)
		}
	}
	var fetch []string
	for _, h := range wanted {
		if _, bad := otherBrand[h]; !bad && (*refresh || kept[h] == "") {
			fetch = append(fetch, h)
		}
	}
	log.Printf("%d hosts in the picker, %d with a logo in the page, %d with an override; fetching %d", len(hosts),
		countCovered(hosts, bundled), countCovered(hosts, overrides), len(fetch))

	found := fetchAll(fetch)
	icons := map[string]icon{}
	for _, h := range wanted {
		if why, bad := otherBrand[h]; bad {
			log.Printf("left out, %s: %s", h, why)
			continue
		}
		if ic, ok := found[h]; ok {
			icons[h] = ic
		} else if name := kept[h]; name != "" && !*refresh {
			ic, err := readIcon(filepath.Join(setDir, name))
			if err != nil {
				log.Fatal(err)
			}
			icons[h] = ic
		}
	}
	for _, group := range dropShared(icons) {
		log.Printf("left out, one image under unrelated hosts: %s", strings.Join(group, ", "))
	}

	if err := writeSet(setDir, icons); err != nil {
		log.Fatal(err)
	}
	var total int
	for _, ic := range icons {
		total += len(ic.data)
	}
	var missing []string
	for _, h := range wanted {
		if _, ok := icons[h]; !ok {
			missing = append(missing, h)
		}
	}
	log.Printf("%d icons in the set, %.0f KiB; %d hosts without one", len(icons), float64(total)/1024, len(missing))
	for _, h := range missing {
		fmt.Println(h)
	}
}

// otherBrand are hosts whose site or icon service answers with somebody
// else's icon, and why. Every check here passes such an icon, so this list
// comes from looking at the set.
var otherBrand = map[string]string{
	"bootyexpo.net": "an empty frame",
	"dr-farfar.net": "Google's mark",
	"pornoid.cz":    "the web host's parking page",
	"uptomega.net":  "a file-sharing script's default, which other hosters show as well",
}

// pickerHosts reads the list /api/hosterauth/hosts serves from a URL or a
// file, as bare lower-case host names without a leading www.
func pickerHosts(from, token string) ([]string, error) {
	var body []byte
	if strings.HasPrefix(from, "http://") || strings.HasPrefix(from, "https://") {
		req, err := http.NewRequest(http.MethodGet, from, nil)
		if err != nil {
			return nil, err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s: %s", from, resp.Status)
		}
		if body, err = io.ReadAll(resp.Body); err != nil {
			return nil, err
		}
	} else {
		var err error
		if body, err = os.ReadFile(from); err != nil {
			return nil, err
		}
	}
	var list []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("%s: %w", from, err)
	}
	var out []string
	for _, h := range list {
		if h := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(h.ID)), "www."); h != "" {
			out = append(out, h)
		}
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// bundledLogos reads the names the page ships a logo for.
func bundledLogos(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, m := range regexp.MustCompile(`\['([^']+)', \{`).FindAllSubmatch(b, -1) {
		out[string(m[1])] = string(m[1])
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s lists no logos", path)
	}
	return out, nil
}

// iconNames maps each host with an icon file in dir to that file's name.
func iconNames(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		ext := filepath.Ext(name)
		if e.IsDir() || strings.HasPrefix(name, ".") || (ext != ".png" && ext != ".svg" && ext != ".webp") {
			continue
		}
		out[strings.TrimSuffix(name, ext)] = name
	}
	return out, nil
}

// covered reports whether host or a domain above it is in names, the way
// the page and the instance look an icon up.
func covered(host string, names map[string]string) bool {
	for h := host; strings.Contains(h, "."); h = h[strings.IndexByte(h, '.')+1:] {
		if names[h] != "" {
			return true
		}
	}
	return false
}

func countCovered(hosts []string, names map[string]string) int {
	n := 0
	for _, h := range hosts {
		if covered(h, names) {
			n++
		}
	}
	return n
}

// icon is one normalised icon: the bytes to ship, their extension, and the
// pixel size of what it was made from, which ranks the sources.
type icon struct {
	data   []byte
	ext    string
	source int
	pixels [32]byte
}

// fetchAll fetches the hosts a few at a time and returns those that got an
// icon.
func fetchAll(hosts []string) map[string]icon {
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out = map[string]icon{}
	)
	queue := make(chan string)
	for range 12 {
		wg.Go(func() {
			for h := range queue {
				ic, from, err := bestIcon(h)
				mu.Lock()
				if err == nil {
					out[h] = ic
					log.Printf("%-32s %s, %d px", h, from, ic.source)
				} else {
					log.Printf("%-32s none: %v", h, err)
				}
				mu.Unlock()
			}
		})
	}
	for _, h := range hosts {
		queue <- h
	}
	close(queue)
	wg.Wait()
	return out
}

// bestIcon asks every source for host's icon and keeps the largest. The site
// comes first and keeps a tie, since a service may hold an icon the site has
// since replaced.
func bestIcon(host string) (icon, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	sources := []struct {
		name  string
		fetch func() ([]byte, error)
	}{
		{"site", func() ([]byte, error) {
			b, _, err := app.FetchSiteIcon(ctx, host)
			return b, err
		}},
		{"google", func() ([]byte, error) {
			return get(ctx, "https://www.google.com/s2/favicons?sz=64&domain="+url.QueryEscape(host))
		}},
		{"duckduckgo", func() ([]byte, error) {
			return get(ctx, "https://icons.duckduckgo.com/ip3/"+url.PathEscape(host)+".ico")
		}},
	}
	var (
		best icon
		from string
		errs []error
	)
	for _, s := range sources {
		body, err := s.fetch()
		var ic icon
		if err == nil {
			ic, err = normalise(body)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.name, err))
			continue
		}
		if ic.source > best.source {
			best, from = ic, s.name
		}
		if best.source >= size {
			break
		}
	}
	if best.data == nil {
		return icon{}, "", errors.Join(errs...)
	}
	return best, from, nil
}

var serviceClient = &http.Client{Timeout: 20 * time.Second}

// get fetches an icon service's answer. Both answer an unknown host with a
// placeholder and an error status, so only a 200 counts.
func get(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := serviceClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// svgRoot matches the start of an SVG document, as the instance's fetch does.
var svgRoot = regexp.MustCompile(`^\x{FEFF}?(?:\s*<[?!][^>]*>)*\s*<svg[\s/>]`)

// svgUnclean finds what keeps an SVG from shipping as it is: anything that
// runs, loads from elsewhere or embeds a raster, and colours that follow the
// browser's dark mode rather than the page's.
var svgUnclean = regexp.MustCompile(`(?i)<script|<foreignobject|\son[a-z]+\s*=|(?:href|src)\s*=\s*["']?\s*(?:https?:|javascript:|data:)|@import|url\(\s*["']?\s*(?:https?:|data:)|prefers-color-scheme`)

// normalise turns a fetched or imported image into the icon to ship.
func normalise(body []byte) (icon, error) {
	if svgRoot.Match(body) {
		switch {
		case len(body) > svgMaxBytes:
			return icon{}, errors.New("SVG too large to ship")
		case svgUnclean.Match(body):
			return icon{}, errors.New("SVG with script, outside references or its own dark mode")
		}
		return icon{data: bytes.TrimSpace(body), ext: "svg", source: size, pixels: sha256.Sum256(body)}, nil
	}
	img, err := decode(body)
	if err != nil {
		return icon{}, err
	}
	src := image.NewNRGBA(img.Bounds())
	draw.Draw(src, src.Bounds(), img, img.Bounds().Min, draw.Src)
	box, err := visible(src)
	if err != nil {
		return icon{}, err
	}
	cut := src.SubImage(box)

	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	w, h := box.Dx(), box.Dy()
	scale := float64(size) / float64(max(w, h))
	fw, fh := max(1, int(float64(w)*scale+0.5)), max(1, int(float64(h)*scale+0.5))
	at := image.Rect((size-fw)/2, (size-fh)/2, (size-fw)/2+fw, (size-fh)/2+fh)
	draw.CatmullRom.Scale(dst, at, cut, box, draw.Over, nil)

	data, err := encodePNG(dst)
	if err != nil {
		return icon{}, err
	}
	return icon{data: data, ext: "png", source: min(img.Bounds().Dx(), img.Bounds().Dy(), size), pixels: sha256.Sum256(dst.Pix)}, nil
}

func decode(body []byte) (image.Image, error) {
	if http.DetectContentType(body) == "image/x-icon" {
		return decodeICO(body)
	}
	img, _, err := image.Decode(bytes.NewReader(body))
	return img, err
}

// visible is the box around the pixels that show. An icon with none, or one
// that is a single colour edge to edge, says nothing about its hoster.
func visible(img *image.NRGBA) (image.Rectangle, error) {
	box := image.Rectangle{}
	first, oneColour, opaque := color.NRGBA{}, true, true
	seen := false
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.NRGBAAt(x, y)
			if c.A < 255 {
				opaque = false
			}
			if c.A == 0 {
				continue
			}
			if !seen {
				first, seen = c, true
			} else if c != first {
				oneColour = false
			}
			box = box.Union(image.Rect(x, y, x+1, y+1))
		}
	}
	switch {
	case !seen:
		return box, errors.New("nothing visible")
	case oneColour && opaque:
		return box, errors.New("one colour edge to edge")
	}
	return box, nil
}

// encodePNG writes img as a palette PNG where it has few enough colours,
// which most icons have, and as a full-colour one otherwise.
func encodePNG(img *image.NRGBA) ([]byte, error) {
	var palette color.Palette
	index := map[color.NRGBA]uint8{}
	for i := 0; i < len(img.Pix); i += 4 {
		c := color.NRGBA{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
		if c.A == 0 {
			c = color.NRGBA{}
		}
		if _, ok := index[c]; ok {
			continue
		}
		if len(palette) == 256 {
			palette = nil
			break
		}
		index[c] = uint8(len(palette))
		palette = append(palette, c)
	}
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	var buf bytes.Buffer
	if palette == nil {
		err := enc.Encode(&buf, img)
		return buf.Bytes(), err
	}
	p := image.NewPaletted(img.Bounds(), palette)
	for i := 0; i < len(img.Pix); i += 4 {
		c := color.NRGBA{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
		if c.A == 0 {
			c = color.NRGBA{}
		}
		p.Pix[i/4] = index[c]
	}
	err := enc.Encode(&buf, p)
	return buf.Bytes(), err
}

func readIcon(path string) (icon, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return icon{}, err
	}
	if filepath.Ext(path) == ".svg" {
		return icon{data: b, ext: "svg", source: size, pixels: sha256.Sum256(b)}, nil
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return icon{}, fmt.Errorf("%s: %w", path, err)
	}
	px := image.NewNRGBA(img.Bounds())
	draw.Draw(px, px.Bounds(), img, img.Bounds().Min, draw.Src)
	return icon{data: b, ext: "png", source: size, pixels: sha256.Sum256(px.Pix)}, nil
}

// dropShared removes the icons one image stands for under hosts that are not
// one site: that image is a hosting script's default, a parking page's or an
// icon service's stand-in rather than a logo. It returns each group removed.
func dropShared(icons map[string]icon) [][]string {
	byImage := map[[32]byte][]string{}
	for h, ic := range icons {
		byImage[ic.pixels] = append(byImage[ic.pixels], h)
	}
	var dropped [][]string
	for _, hosts := range byImage {
		if len(hosts) < 2 || sameSite(hosts) {
			continue
		}
		slices.Sort(hosts)
		for _, h := range hosts {
			delete(icons, h)
		}
		dropped = append(dropped, hosts)
	}
	slices.SortFunc(dropped, func(a, b []string) int { return strings.Compare(a[0], b[0]) })
	return dropped
}

// sharedSecondLevel are the names under a country code that anyone registers
// below, as in co.uk. Two sites under one of them are not related.
var sharedSecondLevel = []string{"ac", "co", "com", "edu", "gov", "net", "org"}

// sameSite reports whether every host is or lies under one registered
// domain.
func sameSite(hosts []string) bool {
	split := make([][]string, len(hosts))
	for i, h := range hosts {
		split[i] = strings.Split(h, ".")
		slices.Reverse(split[i])
	}
	shared := 0
	for allHave(split, shared) {
		shared++
	}
	return shared > 2 || (shared == 2 && !slices.Contains(sharedSecondLevel, split[0][1]))
}

func allHave(split [][]string, i int) bool {
	for _, labels := range split {
		if i >= len(labels) || labels[i] != split[0][i] {
			return false
		}
	}
	return true
}

// writeSet replaces the files in dir with icons.
func writeSet(dir string, icons map[string]icon) error {
	old, err := iconNames(dir)
	if err != nil {
		return err
	}
	for _, name := range old {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	for h, ic := range icons {
		if err := os.WriteFile(filepath.Join(dir, h+"."+ic.ext), ic.data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// importOverrides normalises every <host>.<ext> logo in from into dir,
// replacing an override the host had.
func importOverrides(from, dir string) (int, error) {
	entries, err := os.ReadDir(from)
	if err != nil {
		return 0, err
	}
	have, err := iconNames(dir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		host := strings.TrimPrefix(strings.ToLower(strings.TrimSuffix(e.Name(), ext)), "www.")
		if e.IsDir() || !strings.Contains(host, ".") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			return n, err
		}
		ic, err := normalise(body)
		if err != nil {
			log.Printf("%s: %v", e.Name(), err)
			continue
		}
		if name := have[host]; name != "" {
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				return n, err
			}
		}
		if err := os.WriteFile(filepath.Join(dir, host+"."+ic.ext), ic.data, 0o644); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

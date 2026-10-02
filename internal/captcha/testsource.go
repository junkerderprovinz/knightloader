package captcha

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"math"
	mrand "math/rand/v2"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// SourceTest is Challenge.Source for a captcha TestSource drew.
const SourceTest = "test"

// TestHost is Challenge.Host for a test captcha, so a prompt that names the
// asking hoster names the app instead.
const TestHost = "KnightLoader"

// TestTTL is how long a test captcha can be answered, about what a hoster
// gives.
const TestTTL = 3 * time.Minute

const testIDPrefix = "test-"

// testAlphabet leaves out the characters a small picture turns into one
// another: 0 and O, 1, I and L, 2 and Z, 5 and S, 6 and G, 8 and B.
const testAlphabet = "ACDEFHJKMNPRTUVWXY3479"

const testLength = 5

// TestSource draws image captchas of its own, so the captcha window, the phone
// app and the paid solvers can be tried without a hoster asking for one. The
// app lists them beside the JD ones and routes an answer here by its id. Safe
// for concurrent use.
type TestSource struct {
	mu      sync.Mutex
	pending map[string]testChallenge
	now     func() time.Time
}

type testChallenge struct {
	c       Challenge
	text    string
	solvers bool
}

// NewTestSource returns a TestSource with nothing pending.
func NewTestSource() *TestSource {
	return &TestSource{pending: map[string]testChallenge{}, now: time.Now}
}

// IsTest reports whether id names a test captcha. It reads the id alone, so an
// answer that arrives after the captcha expired still comes here and not to JD.
func IsTest(id string) bool { return strings.HasPrefix(id, testIDPrefix) }

// New draws a test captcha and keeps it pending until it is answered, skipped
// or expires. solvers says whether the paid solvers may take it, since they
// bill a test like any other captcha.
func (s *TestSource) New(solvers bool) (Challenge, error) {
	text := make([]byte, testLength)
	for i := range text {
		text[i] = testAlphabet[mrand.IntN(len(testAlphabet))]
	}
	img, err := drawTestCaptcha(string(text))
	if err != nil {
		return Challenge{}, err
	}
	c := Challenge{
		ID:        testIDPrefix + rand.Text(),
		Source:    SourceTest,
		Host:      TestHost,
		Kind:      KindImage,
		Payload:   &ImagePayload{DataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(img)},
		ExpiresAt: s.now().Add(TestTTL),
		Test:      true,
	}
	s.mu.Lock()
	s.pending[c.ID] = testChallenge{c: c, text: string(text), solvers: solvers}
	s.mu.Unlock()
	return c, nil
}

// List returns every test captcha still answerable and forgets the expired
// ones, which the app then reports as timed out.
func (s *TestSource) List() []Challenge {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	out := make([]Challenge, 0, len(s.pending))
	for id, p := range s.pending {
		if !now.Before(p.c.ExpiresAt) {
			delete(s.pending, id)
			continue
		}
		out = append(out, p.c)
	}
	return out
}

// ForSolvers reports whether the paid solvers may take id.
func (s *TestSource) ForSolvers(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending[id].solvers
}

// Check compares given with the text drawn in id and forgets id. Case and
// spaces do not count, as with most hosters. live is false for a captcha that
// expired or was answered already, and want and correct are empty then.
func (s *TestSource) Check(id, given string) (want string, correct, live bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pending[id]
	if !ok {
		return "", false, false
	}
	delete(s.pending, id)
	if !s.now().Before(p.c.ExpiresAt) {
		return "", false, false
	}
	given = strings.Join(strings.Fields(given), "")
	return p.text, strings.EqualFold(given, p.text), true
}

// Abort forgets id. A captcha already gone is not an error, as for
// Source.Abort.
func (s *TestSource) Abort(id string) {
	s.mu.Lock()
	delete(s.pending, id)
	s.mu.Unlock()
}

// drawTestCaptcha renders text in the 7x13 bitmap font, scales it up through a
// wave so the letters bend, and scatters dots and lines over it. The noise is
// lighter than the ink, so a person reads it at once and an OCR pass still has
// some work to do.
func drawTestCaptcha(text string) ([]byte, error) {
	const scale = 4
	face := basicfont.Face7x13
	small := image.NewAlpha(image.Rect(0, 0, 9*len(text)+6, 19))
	d := font.Drawer{Dst: small, Src: image.Opaque, Face: face}
	for i, r := range text {
		d.Dot = fixed.P(3+9*i, 14+mrand.IntN(4)-2)
		d.DrawString(string(r))
	}

	b := small.Bounds()
	w, h := b.Dx()*scale, b.Dy()*scale
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	ground := color.RGBA{0xf4, 0xf1, 0xea, 0xff}
	inks := []color.RGBA{{0x1f, 0x3a, 0x5f, 0xff}, {0x5a, 0x1f, 0x3a, 0xff}, {0x23, 0x4d, 0x2a, 0xff}, {0x3b, 0x2a, 0x1a, 0xff}}
	ink := inks[mrand.IntN(len(inks))]
	ampX, ampY := 3+mrand.Float64()*2, 2+mrand.Float64()*2
	phaseX, phaseY := mrand.Float64()*2*math.Pi, mrand.Float64()*2*math.Pi
	for y := range h {
		for x := range w {
			sx := (float64(x) + ampX*math.Sin(float64(y)/9+phaseX)) / scale
			sy := (float64(y) + ampY*math.Sin(float64(x)/14+phaseY)) / scale
			if small.AlphaAt(int(sx), int(sy)).A > 0x80 {
				img.SetRGBA(x, y, ink)
			} else {
				img.SetRGBA(x, y, ground)
			}
		}
	}

	noise := color.RGBA{0x9a, 0x94, 0x8a, 0xff}
	for range w * h / 40 {
		img.SetRGBA(mrand.IntN(w), mrand.IntN(h), noise)
	}
	for range 4 {
		y0, y1 := mrand.IntN(h), mrand.IntN(h)
		for x := range w {
			y := y0 + (y1-y0)*x/w
			img.SetRGBA(x, y, noise)
			img.SetRGBA(x, y+1, noise)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

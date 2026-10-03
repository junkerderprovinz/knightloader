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
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
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

// testAlphabet leaves out the characters a tilted letter turns into one
// another: 0 and O, 1, I and L, 2 and Z, 5 and S, 6 and G, 8 and B, T and J,
// V and Y, K and X, and N and W beside H and M.
const testAlphabet = "ACDEFHKMPRTUV3479"

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

// testFont is the face the test captchas are set in: a bold sans whose letters
// keep apart when tilted.
var testFont = sync.OnceValues(func() (*opentype.Font, error) { return opentype.Parse(gobold.TTF) })

// The test captcha's size in pixels. The captcha window shows it as it is and
// the phone app at up to twice that, so the letters are drawn at a size both
// read without zooming.
const (
	testWidth    = 300
	testHeight   = 100
	testFontSize = 60
	testCell     = 96
)

// testNoise is the colour of the dots and lines under a test captcha's ink.
var testNoise = color.RGBA{0xc4, 0xbe, 0xb2, 0xff}

// drawTestCaptcha sets text in a bold face, turns each character a little and
// shifts it up or down, bends the line through a gentle wave and puts dots and
// lines under the ink. The noise never cuts a stroke, so a person reads it at
// once while an OCR pass still has some work to do.
func drawTestCaptcha(text string) ([]byte, error) {
	f, err := testFont()
	if err != nil {
		return nil, err
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: testFontSize, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil, err
	}
	defer face.Close()

	img := image.NewRGBA(image.Rect(0, 0, testWidth, testHeight))
	ground := color.RGBA{0xf4, 0xf1, 0xea, 0xff}
	for i := 0; i < len(img.Pix); i += 4 {
		copy(img.Pix[i:i+4], []byte{ground.R, ground.G, ground.B, ground.A})
	}

	for range testWidth * testHeight / 50 {
		img.SetRGBA(mrand.IntN(testWidth), mrand.IntN(testHeight), testNoise)
	}
	for range 3 {
		y0, y1 := mrand.IntN(testHeight), mrand.IntN(testHeight)
		for x := range testWidth {
			y := y0 + (y1-y0)*x/testWidth
			img.SetRGBA(x, y, testNoise)
			img.SetRGBA(x, y+1, testNoise)
		}
	}

	inks := []color.RGBA{{0x1f, 0x3a, 0x5f, 0xff}, {0x5a, 0x1f, 0x3a, 0xff}, {0x23, 0x4d, 0x2a, 0xff}, {0x3b, 0x2a, 0x1a, 0xff}}
	ink := inks[mrand.IntN(len(inks))]
	amp, phase := 1.5+mrand.Float64(), mrand.Float64()*2*math.Pi
	pitch := float64(testWidth-24) / float64(len(text))
	for i, r := range text {
		glyph := drawGlyph(face, r)
		angle := (mrand.Float64()*2 - 1) * 0.2
		cx := 12 + pitch*(float64(i)+0.5) + mrand.Float64()*4 - 2
		cy := testHeight/2 + mrand.Float64()*10 - 5
		stamp(img, glyph, ink, cx, cy, angle, amp, phase)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// drawGlyph sets r centred on a square testCell pixels wide.
func drawGlyph(face font.Face, r rune) *image.Alpha {
	cell := image.NewAlpha(image.Rect(0, 0, testCell, testCell))
	bounds, _ := font.BoundString(face, string(r))
	mid := fixed.Point26_6{X: (bounds.Min.X + bounds.Max.X) / 2, Y: (bounds.Min.Y + bounds.Max.Y) / 2}
	d := font.Drawer{Dst: cell, Src: image.Opaque, Face: face}
	d.Dot = fixed.P(testCell/2, testCell/2).Sub(mid)
	d.DrawString(string(r))
	return cell
}

// stamp paints glyph onto img centred at cx, cy, turned by angle radians and
// shifted sideways by a wave of amp pixels, blending its edges into what is
// underneath.
func stamp(img *image.RGBA, glyph *image.Alpha, ink color.RGBA, cx, cy, angle, amp, phase float64) {
	sin, cos := math.Sincos(angle)
	half := float64(testCell) / 2
	for y := int(cy - half); y < int(cy+half); y++ {
		for x := int(cx - half); x < int(cx+half); x++ {
			if !(image.Point{x, y}).In(img.Rect) {
				continue
			}
			dx := float64(x) - cx + amp*math.Sin(float64(y)/15+phase)
			dy := float64(y) - cy
			a := sampleAlpha(glyph, cos*dx+sin*dy+half, -sin*dx+cos*dy+half)
			if a == 0 {
				continue
			}
			under := img.RGBAAt(x, y)
			img.SetRGBA(x, y, color.RGBA{
				R: mix(under.R, ink.R, a),
				G: mix(under.G, ink.G, a),
				B: mix(under.B, ink.B, a),
				A: 0xff,
			})
		}
	}
}

// sampleAlpha reads glyph at a point between pixels, weighing the four around
// it, so a turned letter keeps smooth edges.
func sampleAlpha(glyph *image.Alpha, x, y float64) float64 {
	x0, y0 := math.Floor(x-0.5), math.Floor(y-0.5)
	fx, fy := x-0.5-x0, y-0.5-y0
	at := func(px, py float64) float64 { return float64(glyph.AlphaAt(int(px), int(py)).A) / 0xff }
	top := at(x0, y0)*(1-fx) + at(x0+1, y0)*fx
	bottom := at(x0, y0+1)*(1-fx) + at(x0+1, y0+1)*fx
	return top*(1-fy) + bottom*fy
}

func mix(under, over uint8, a float64) uint8 {
	return uint8(float64(under)*(1-a) + float64(over)*a + 0.5)
}

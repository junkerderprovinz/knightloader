package captcha

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"
)

func newTestCaptcha(t *testing.T, s *TestSource, solvers bool) (Challenge, string) {
	t.Helper()
	c, err := s.New(TestImage, solvers)
	if err != nil {
		t.Fatal(err)
	}
	return c, s.pending[c.ID].text
}

func TestATestCaptchaTakesItsTextInAnyCaseAndSpacing(t *testing.T) {
	s := NewTestSource()
	c, text := newTestCaptcha(t, s, false)

	given := " " + strings.ToLower(text[:2]) + " " + text[2:] + " "
	want, correct, live := s.Check(c.ID, given)
	if !live || !correct || want != text {
		t.Errorf("Check(%q) = %q, %v, %v; want %q, right, live", given, want, correct, live, text)
	}
}

func TestAWrongAnswerToATestCaptchaSaysWhatWasDrawn(t *testing.T) {
	s := NewTestSource()
	c, text := newTestCaptcha(t, s, false)

	want, correct, live := s.Check(c.ID, "nope")
	if !live || correct || want != text {
		t.Errorf("Check(nope) = %q, %v, %v; want %q, wrong, live", want, correct, live, text)
	}
}

func TestATestCaptchaCanBeAnsweredOnce(t *testing.T) {
	s := NewTestSource()
	c, text := newTestCaptcha(t, s, false)
	s.Check(c.ID, text)

	if _, _, live := s.Check(c.ID, text); live {
		t.Error("a test captcha took a second answer")
	}
	if got := s.List(); len(got) != 0 {
		t.Errorf("List after the answer = %d captchas, want none", len(got))
	}
}

func TestAnExpiredTestCaptchaLeavesTheListAndTakesNoAnswer(t *testing.T) {
	s := NewTestSource()
	now := time.Now()
	s.now = func() time.Time { return now }
	c, text := newTestCaptcha(t, s, false)
	late, _ := newTestCaptcha(t, s, false)

	if got := s.List(); len(got) != 2 {
		t.Fatalf("List = %d captchas, want 2", len(got))
	}
	now = now.Add(TestTTL)
	if _, _, live := s.Check(late.ID, "x"); live {
		t.Error("an expired test captcha took an answer")
	}
	if got := s.List(); len(got) != 0 {
		t.Errorf("List after the deadline = %d captchas, want none", len(got))
	}
	if _, _, live := s.Check(c.ID, text); live {
		t.Error("a test captcha the list dropped took an answer")
	}
}

func TestOnlyATestCaptchaAskedForTheSolversGoesToThem(t *testing.T) {
	s := NewTestSource()
	mine, _ := newTestCaptcha(t, s, false)
	theirs, _ := newTestCaptcha(t, s, true)

	if s.ForSolvers(mine.ID) {
		t.Error("a test captcha not meant for the solvers is offered to them")
	}
	if !s.ForSolvers(theirs.ID) {
		t.Error("a test captcha meant for the solvers is kept from them")
	}
}

func TestATestCaptchaIsAnImageThePromptsAndSolversCanUse(t *testing.T) {
	s := NewTestSource()
	c, _ := newTestCaptcha(t, s, false)

	if !IsTest(c.ID) || c.Source != SourceTest || c.Kind != KindImage || !c.Test {
		t.Errorf("challenge = %+v, want a test image challenge", c)
	}
	if !c.ExpiresAt.After(time.Now()) {
		t.Errorf("ExpiresAt = %v, want a deadline ahead", c.ExpiresAt)
	}
	p, ok := c.Payload.(*ImagePayload)
	if !ok || !ImageReadable(p.DataURL) {
		t.Fatalf("payload = %#v, want an image the solvers can read", c.Payload)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(p.DataURL, "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("the picture is no PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() < 150 || b.Dy() < 50 {
		t.Errorf("the picture is %dx%d, too small to read", b.Dx(), b.Dy())
	}
}

func TestAWidgetTestCaptchaCarriesTheVendorsAlwaysPassingKeyOnANeutralPage(t *testing.T) {
	for kind, key := range map[string]string{
		VendorTurnstile: "1x00000000000000000000AA",
		VendorHCaptcha:  "10000000-ffff-ffff-ffff-000000000001",
		VendorRecaptcha: "6LeIxAcTAAAAAJcZVRqyHh71UMIEGNQ_MXjiZKhI",
	} {
		s := NewTestSource()
		c, err := s.New(kind, false)
		if err != nil {
			t.Fatal(err)
		}
		p, ok := c.Payload.(*WidgetPayload)
		if !IsTest(c.ID) || !c.Test || c.Kind != KindWidget || !ok {
			t.Fatalf("%s: challenge = %+v, want a test widget challenge", kind, c)
		}
		if p.Vendor != kind || p.SiteKey != key || p.SiteURL != TestPage || p.ContextURL != TestPage {
			t.Errorf("%s: payload = %+v, want the test key on %s", kind, p, TestPage)
		}
	}
}

func TestAnyTokenAnswersAWidgetTestCaptcha(t *testing.T) {
	s := NewTestSource()
	c, err := s.New(VendorTurnstile, false)
	if err != nil {
		t.Fatal(err)
	}
	want, correct, live := s.Check(c.ID, "XXXX.DUMMY.TOKEN.XXXX")
	if !live || !correct || want != "Cloudflare Turnstile" {
		t.Errorf("Check = %q, %v, %v; want the vendor named, right, live", want, correct, live)
	}

	c, _ = s.New(VendorHCaptcha, false)
	if _, correct, _ := s.Check(c.ID, " "); correct {
		t.Error("a blank answer passed a widget test captcha")
	}
}

func TestATestCaptchaOfAnUnknownKindIsRefused(t *testing.T) {
	if _, err := NewTestSource().New("puzzle", false); !errors.Is(err, ErrTestKind) {
		t.Errorf("err = %v, want ErrTestKind", err)
	}
}

func TestATestCaptchaHoldsNoTwoCharactersATiltTurnsIntoEachOther(t *testing.T) {
	for _, pair := range []string{"0O", "1I", "IL", "2Z", "5S", "6G", "8B", "TJ", "VY", "KX", "HN", "HW", "MN", "MW", "NW"} {
		if strings.ContainsRune(testAlphabet, rune(pair[0])) && strings.ContainsRune(testAlphabet, rune(pair[1])) {
			t.Errorf("the alphabet holds both %c and %c", pair[0], pair[1])
		}
	}
}

func TestTheNoiseInATestCaptchaNeverCutsAStroke(t *testing.T) {
	dark := func(c color.RGBA) bool { return int(c.R)+int(c.G)+int(c.B) < 3*0x70 }
	for range 20 {
		raw, err := drawTestCaptcha("HKMPR")
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		rgba := img.(*image.RGBA)
		b := rgba.Bounds()
		for y := b.Min.Y + 1; y < b.Max.Y-1; y++ {
			for x := b.Min.X + 1; x < b.Max.X-1; x++ {
				if rgba.RGBAAt(x, y) != testNoise {
					continue
				}
				// A one-pixel gap between two strokes has ink on two opposite
				// sides; only a dot painted over a stroke has it on all four.
				if dark(rgba.RGBAAt(x-1, y)) && dark(rgba.RGBAAt(x+1, y)) &&
					dark(rgba.RGBAAt(x, y-1)) && dark(rgba.RGBAAt(x, y+1)) {
					t.Fatalf("a noise pixel at %d,%d sits inside a stroke", x, y)
				}
			}
		}
	}
}

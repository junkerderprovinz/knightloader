package captcha

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"strings"
	"testing"
	"time"
)

func newTestCaptcha(t *testing.T, s *TestSource, solvers bool) (Challenge, string) {
	t.Helper()
	c, err := s.New(solvers)
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

package main

import (
	"reflect"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type popupRig struct {
	p        *LinkPopup
	settings clipSettings
	told     []popupBatch
	shown    []string
	hidden   int
	opened   int
}

func newPopupRig() *popupRig {
	r := &popupRig{}
	r.p = &LinkPopup{
		settings:      func() clipSettings { return r.settings },
		tell:          func(b popupBatch) { r.told = append(r.told, b) },
		show:          func(place string) { r.shown = append(r.shown, place) },
		hide:          func() { r.hidden++ },
		openCollector: func() { r.opened++ },
	}
	return r
}

func (r *popupRig) last() popupBatch { return r.told[len(r.told)-1] }

func TestThePopupShowsOnlyWhenLinksWentIn(t *testing.T) {
	r := newPopupRig()
	for _, o := range []clipOutcome{
		{Kind: "none"},
		{Kind: "failed", Reason: "nas unreachable"},
		{Kind: "limited"},
		{Kind: "stopped"},
		{Kind: "staged", N: 0},
	} {
		r.p.offer(o)
	}
	if len(r.shown) != 0 || len(r.told) != 0 {
		t.Fatalf("shown %d times and told %v without a link going in", len(r.shown), r.told)
	}
	r.p.offer(clipOutcome{Kind: "staged", N: 2, Host: "files.example"})
	if len(r.shown) != 1 || !reflect.DeepEqual(r.told, []popupBatch{{Seq: 1, From: 1, N: 2, Host: "files.example", Seconds: 5, Place: "bottom-right"}}) {
		t.Fatalf("shown %d times, told %v", len(r.shown), r.told)
	}
}

func TestThePopupStaysAwayWhileSwitchedOff(t *testing.T) {
	r := newPopupRig()
	off := false
	r.settings.Popup = &off
	r.p.offer(clipOutcome{Kind: "staged", N: 1})
	if len(r.shown) != 0 || len(r.told) != 0 {
		t.Fatalf("switched off, it showed %d times", len(r.shown))
	}
}

func TestQuickCopiesAddUpInOnePopup(t *testing.T) {
	r := newPopupRig()
	r.p.offer(clipOutcome{Kind: "staged", N: 1, Host: "files.example"})
	r.p.offer(clipOutcome{Kind: "staged", N: 3, Host: "files.example"})
	if len(r.shown) != 1 {
		t.Fatalf("two quick copies showed %d popups", len(r.shown))
	}
	if want := (popupBatch{Seq: 2, From: 1, N: 4, Host: "files.example", Seconds: 5, Place: "bottom-right"}); r.last() != want {
		t.Fatalf("told %+v, want %+v", r.last(), want)
	}
	r.p.offer(clipOutcome{Kind: "staged", N: 1, Host: "other.example"})
	r.p.offer(clipOutcome{Kind: "staged", N: 1, Host: "files.example"})
	if want := (popupBatch{Seq: 4, From: 1, N: 6, Seconds: 5, Place: "bottom-right"}); r.last() != want {
		t.Fatalf("links from two hosts told %+v, want %+v", r.last(), want)
	}
}

func TestADismissFromBeforeTheLastCopyKeepsThePopupUp(t *testing.T) {
	r := newPopupRig()
	r.p.offer(clipOutcome{Kind: "staged", N: 1})
	r.p.offer(clipOutcome{Kind: "staged", N: 1})
	r.p.Dismiss(1)
	if r.hidden != 0 {
		t.Fatal("a dismissal from before the second copy hid the popup")
	}
	r.p.Dismiss(2)
	if r.hidden != 1 {
		t.Fatalf("hidden %d times, want 1", r.hidden)
	}
	r.p.offer(clipOutcome{Kind: "staged", N: 5})
	if len(r.shown) != 2 {
		t.Fatalf("a copy after the popup went showed it %d times in all, want 2", len(r.shown))
	}
	if want := (popupBatch{Seq: 3, From: 3, N: 5, Seconds: 5, Place: "bottom-right"}); r.last() != want {
		t.Fatalf("the next popup told %+v, want %+v", r.last(), want)
	}
}

func TestAClickOnThePopupOpensTheCollector(t *testing.T) {
	r := newPopupRig()
	r.p.offer(clipOutcome{Kind: "staged", N: 1})
	r.p.Open()
	if r.hidden != 1 || r.opened != 1 {
		t.Fatalf("hidden %d and opened the collector %d times", r.hidden, r.opened)
	}
	r.p.offer(clipOutcome{Kind: "staged", N: 1})
	if len(r.shown) != 2 || r.last().N != 1 {
		t.Fatalf("the copy after the click showed %d popups in all and told %+v", len(r.shown), r.last())
	}
}

func TestAPageThatLoadsLateFindsThePopupOnScreen(t *testing.T) {
	r := newPopupRig()
	if b := r.p.Current(); b != (popupBatch{}) {
		t.Fatalf("with nothing on screen the page found %+v", b)
	}
	r.p.offer(clipOutcome{Kind: "staged", N: 2})
	if b := r.p.Current(); b != r.last() {
		t.Fatalf("the page found %+v, the popup shows %+v", b, r.last())
	}
	r.p.Dismiss(r.last().Seq)
	if b := r.p.Current(); b != (popupBatch{}) {
		t.Fatalf("after the popup went the page found %+v", b)
	}
}

func TestThePopupStaysAwayFromLinksSentToAnotherInstance(t *testing.T) {
	r := newPopupRig()
	r.settings.Target = "nas"
	r.p.offer(clipOutcome{Kind: "staged", N: 1})
	if len(r.shown) != 0 {
		t.Fatal("links sent to a peer showed the popup, whose click opens this collector")
	}
}

func TestThePopupTakesItsTimeAndPlaceFromThePage(t *testing.T) {
	for _, tc := range []struct {
		seconds   *int
		place     string
		wantSecs  int
		wantPlace string
	}{
		{nil, "", 5, "bottom-right"},
		{ptr(3), "top-left", 3, "top-left"},
		{ptr(8), "bottom-center", 8, "bottom-center"},
		{ptr(15), "top-right", 15, "top-right"},
		{ptr(0), "bottom-left", 0, "bottom-left"},
		{ptr(7), "middle", 5, "bottom-right"},
	} {
		r := newPopupRig()
		r.settings.PopupSeconds, r.settings.PopupPlace = tc.seconds, tc.place
		r.p.offer(clipOutcome{Kind: "staged", N: 1})
		if b := r.last(); b.Seconds != tc.wantSecs || b.Place != tc.wantPlace {
			t.Errorf("%v seconds at %q told %+v, want %d seconds at %q", tc.seconds, tc.place, b, tc.wantSecs, tc.wantPlace)
		}
		if !reflect.DeepEqual(r.shown, []string{tc.wantPlace}) {
			t.Errorf("%q showed the window at %v", tc.place, r.shown)
		}
	}
}

func TestAPopupThatGrowsKeepsTheTimeAndPlaceItAppearedWith(t *testing.T) {
	r := newPopupRig()
	r.p.offer(clipOutcome{Kind: "staged", N: 1})
	r.settings.PopupSeconds, r.settings.PopupPlace = ptr(0), "top-left"
	r.p.offer(clipOutcome{Kind: "staged", N: 1})
	if b := r.last(); b.Seconds != 5 || b.Place != "bottom-right" {
		t.Fatalf("the grown popup told %+v", b)
	}
}

func TestThePopupSitsAtItsPlaceOnTheWorkArea(t *testing.T) {
	work := application.Rect{X: 1920, Y: 40, Width: 1920, Height: 1000}
	left := 1920 + trayGap
	right := 1920 + 1920 - popupWidth - trayGap
	centre := 1920 + (1920-popupWidth)/2
	top := 40 + trayGap
	bottom := 40 + 1000 - popupHeight - trayGap
	for place, want := range map[string][2]int{
		"bottom-right":  {right, bottom},
		"bottom-left":   {left, bottom},
		"top-right":     {right, top},
		"top-left":      {left, top},
		"bottom-center": {centre, bottom},
		"top-center":    {centre, top},
	} {
		got := popupBounds(work, place)
		if got != (application.Rect{X: want[0], Y: want[1], Width: popupWidth, Height: popupHeight}) {
			t.Errorf("%s placed at %+v, want %d,%d", place, got, want[0], want[1])
		}
	}
}

func ptr(n int) *int { return &n }

package main

import (
	"reflect"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type popupRig struct {
	p      *LinkPopup
	on     bool
	told   []popupBatch
	shown  int
	hidden int
	opened int
}

func newPopupRig() *popupRig {
	r := &popupRig{on: true}
	r.p = &LinkPopup{
		enabled:       func() bool { return r.on },
		tell:          func(b popupBatch) { r.told = append(r.told, b) },
		show:          func() { r.shown++ },
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
	if r.shown != 0 || len(r.told) != 0 {
		t.Fatalf("shown %d times and told %v without a link going in", r.shown, r.told)
	}
	r.p.offer(clipOutcome{Kind: "staged", N: 2, Host: "files.example"})
	if r.shown != 1 || !reflect.DeepEqual(r.told, []popupBatch{{Seq: 1, From: 1, N: 2, Host: "files.example"}}) {
		t.Fatalf("shown %d times, told %v", r.shown, r.told)
	}
}

func TestThePopupStaysAwayWhileSwitchedOff(t *testing.T) {
	r := newPopupRig()
	r.on = false
	r.p.offer(clipOutcome{Kind: "staged", N: 1})
	if r.shown != 0 || len(r.told) != 0 {
		t.Fatalf("switched off, it showed %d times", r.shown)
	}
}

func TestQuickCopiesAddUpInOnePopup(t *testing.T) {
	r := newPopupRig()
	r.p.offer(clipOutcome{Kind: "staged", N: 1, Host: "files.example"})
	r.p.offer(clipOutcome{Kind: "staged", N: 3, Host: "files.example"})
	if r.shown != 1 {
		t.Fatalf("two quick copies showed %d popups", r.shown)
	}
	if want := (popupBatch{Seq: 2, From: 1, N: 4, Host: "files.example"}); r.last() != want {
		t.Fatalf("told %+v, want %+v", r.last(), want)
	}
	r.p.offer(clipOutcome{Kind: "staged", N: 1, Host: "other.example"})
	r.p.offer(clipOutcome{Kind: "staged", N: 1, Host: "files.example"})
	if want := (popupBatch{Seq: 4, From: 1, N: 6}); r.last() != want {
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
	if r.shown != 2 {
		t.Fatalf("a copy after the popup went showed it %d times in all, want 2", r.shown)
	}
	if want := (popupBatch{Seq: 3, From: 3, N: 5}); r.last() != want {
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
	if r.shown != 2 || r.last().N != 1 {
		t.Fatalf("the copy after the click showed %d popups in all and told %+v", r.shown, r.last())
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

func TestThePopupSitsInTheLowerRightCornerOfTheWorkArea(t *testing.T) {
	work := application.Rect{X: 1920, Y: 0, Width: 1920, Height: 1032}
	got := popupBounds(work)
	want := application.Rect{
		X:      1920 + 1920 - popupWidth - trayGap,
		Y:      1032 - popupHeight - trayGap,
		Width:  popupWidth,
		Height: popupHeight,
	}
	if got != want {
		t.Fatalf("placed at %+v, want %+v", got, want)
	}
}

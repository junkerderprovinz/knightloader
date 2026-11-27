package feed

import (
	"context"
	"errors"
	"testing"
	"time"
)

// lengths is a Prober over a fixed table, counting what it was asked.
type lengths struct {
	of    map[string]time.Duration
	asked []string
}

func (l *lengths) Duration(_ context.Context, link string) (time.Duration, error) {
	l.asked = append(l.asked, link)
	d, ok := l.of[link]
	if !ok {
		return 0, errors.New("no such video")
	}
	return d, nil
}

func TestEachEntryIsTakenOrNamesWhyNot(t *testing.T) {
	m := Subscription{TitleFilter: "^Folge", ExcludeFilter: "(?i)trailer", MinMinutes: 10, MaxMinutes: 60}.matcher()
	for _, tc := range []struct {
		it   Item
		want Reason
	}{
		{Item{Title: "Werbung", Duration: 20 * time.Minute}, ReasonInclude},
		{Item{Title: "Folge 3 Trailer", Duration: 20 * time.Minute}, ReasonExclude},
		{Item{Title: "Folge 3", Duration: 2 * time.Minute}, ReasonShort},
		{Item{Title: "Folge 3", Duration: 3 * time.Hour}, ReasonLong},
		{Item{Title: "Folge 3", Duration: 20 * time.Minute}, ""},
		// A length nobody can tell is no reason to leave an entry out.
		{Item{Title: "Folge 4"}, ""},
	} {
		if _, got := m.judge(context.Background(), nil, tc.it); got != tc.want {
			t.Errorf("%q (%v): reason %q, want %q", tc.it.Title, tc.it.Duration, got, tc.want)
		}
	}
}

func TestALengthTheFeedLacksIsAskedForOnlyWhenItMatters(t *testing.T) {
	p := &lengths{of: map[string]time.Duration{"https://v.invalid/short": 50 * time.Second}}
	short := Item{Title: "Clip", Link: "https://v.invalid/short"}

	unbounded := Subscription{}.matcher()
	if _, r := unbounded.judge(context.Background(), p, short); r != "" || len(p.asked) != 0 {
		t.Fatalf("with no bound the length was probed (%v) or judged (%q)", p.asked, r)
	}

	bounded := Subscription{MinMinutes: 2, TitleFilter: "Clip"}.matcher()
	it, r := bounded.judge(context.Background(), p, short)
	if r != ReasonShort || it.Duration != 50*time.Second {
		t.Fatalf("a probed short entry came back %q with %v", r, it.Duration)
	}

	p.asked = nil
	if _, r := bounded.judge(context.Background(), p, Item{Title: "Werbung", Link: "https://v.invalid/short"}); r != ReasonInclude || len(p.asked) != 0 {
		t.Fatalf("an entry the title already ruled out was probed (%v)", p.asked)
	}
	if _, r := bounded.judge(context.Background(), p, Item{Title: "Clip", Link: "https://v.invalid/gone"}); r != "" {
		t.Fatalf("an entry whose probe failed was left out as %q", r)
	}
}

func TestABrokenExcludePatternKeepsEverythingOut(t *testing.T) {
	m := Subscription{ExcludeFilter: "(["}.matcher()
	if _, r := m.judge(context.Background(), nil, Item{Title: "Folge 1"}); r == "" {
		t.Fatal("an exclude filter that does not compile let an entry through")
	}
}

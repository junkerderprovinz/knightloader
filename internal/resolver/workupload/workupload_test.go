package workupload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/hoster"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/resolver/workupload/wutest"
)

func TestFileLinksAreClaimedAndWrittenTheSameWay(t *testing.T) {
	for raw, want := range map[string]string{
		"https://workupload.com/file/QpDxHzUJcL8":     "https://workupload.com/file/QpDxHzUJcL8",
		"http://www.workupload.com/file/QpDxHzUJcL8":  "https://workupload.com/file/QpDxHzUJcL8",
		"https://en.workupload.com/file/QpDxHzUJcL8":  "https://workupload.com/file/QpDxHzUJcL8",
		"https://workupload.com/start/QpDxHzUJcL8":    "https://workupload.com/file/QpDxHzUJcL8",
		"https://workupload.com/report/QpDxHzUJcL8":   "https://workupload.com/file/QpDxHzUJcL8",
		"https://workupload.com/file/y6j4HBEj?lang=x": "https://workupload.com/file/y6j4HBEj",
	} {
		got, ok := Normalize(raw)
		if !ok || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", raw, got, ok, want)
		}
	}
	for _, raw := range []string{
		"https://workupload.com/",
		"https://workupload.com/archive/2WvJvPpS",
		"https://f83.workupload.com/download/QpDxHzUJcL8",
		"https://workupload.com/file/Qp-DxHz",
		"https://example.com/file/QpDxHzUJcL8",
	} {
		if (Resolver{}).Match(raw) {
			t.Errorf("Match(%q) = true", raw)
		}
	}
}

func TestResolvingAsksNothing(t *testing.T) {
	res, err := Resolver{}.Resolve(context.Background(), resolver.Request{URL: "https://en.workupload.com/start/QpDxHzUJcL8"})
	if err != nil || res.DirectURL != "https://workupload.com/file/QpDxHzUJcL8" || res.Connections != maxConns {
		t.Errorf("Resolve = %+v, %v", res, err)
	}
}

func TestTheRobotCheckIsAnsweredAndThePageNamesSizesAndChecksEveryFile(t *testing.T) {
	srv := wutest.New(t)
	live := srv.Add("QpDxHzUJcL8", wutest.File{Name: "PS3-SYSCON-1.1.ova", Body: make([]byte, 1<<20+7)})
	locked := srv.Add("y6j4HBEj", wutest.File{Name: "AoRE-DBG.rar", Body: []byte("rar"), Password: "aore"})
	pause := InspectPause
	InspectPause = 0
	t.Cleanup(func() { InspectPause = pause })

	got := Resolver{Client: NewClient(srv.URL, srv.Client())}.Inspect(context.Background(), []string{live, "https://workupload.com/file/TUcAcRxAyzq", locked, "https://example.com/x"})
	want := []resolver.LinkInfo{
		{Availability: core.AvailOnline, Name: "PS3-SYSCON-1.1.ova", Size: 1<<20 + 7},
		{Availability: core.AvailOffline},
		{Availability: core.AvailOnline, Name: "AoRE-DBG.rar", Size: 3},
		{},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Inspect = %+v\nwant      %+v", got, want)
	}
	if srv.Solves() == 0 {
		t.Error("the robot check was never answered")
	}
}

func TestADownloadGoesThroughTheStartPageAndTheAPIPastEveryRobotCheck(t *testing.T) {
	srv := wutest.New(t)
	link := srv.Add("QpDxHzUJcL8", wutest.File{Name: "PS3-SYSCON-1.1.ova", Body: []byte("ova")})

	l, err := NewClient(srv.URL, srv.Client()).Unlock(context.Background(), link, "")
	if err != nil {
		t.Fatal(err)
	}
	if l.URL != srv.URL+"/download/QpDxHzUJcL8" || l.Conns != maxConns || l.Name != "PS3-SYSCON-1.1.ova" || l.Size != 3 {
		t.Errorf("link = %+v", l)
	}
	if l.Headers["Referer"] != srv.URL+"/file/QpDxHzUJcL8" || !strings.Contains(l.Headers["Cookie"], "PHPSESSID=") {
		t.Errorf("headers = %v, want the file page as referer and the visit's cookies", l.Headers)
	}
	if srv.Solves() < 2 {
		t.Errorf("answered the robot check %d times, want it again once it came back", srv.Solves())
	}
	if got := srv.Requests(); !slices.Contains(got, "GET /start/QpDxHzUJcL8") {
		t.Errorf("requests %v, want the start page before the API", got)
	}
}

func TestAProtectedFileOpensWithTheDownloadPassword(t *testing.T) {
	srv := wutest.New(t)
	link := srv.Add("y6j4HBEj", wutest.File{Name: "AoRE-DBG.rar", Body: []byte("rar"), Password: "aore"})
	c := NewClient(srv.URL, srv.Client())

	if _, err := c.Unlock(context.Background(), link, "aore"); err != nil {
		t.Fatal(err)
	}
	for password, want := range map[string]string{"": "download password", "wrong": "not accepted"} {
		_, err := c.Unlock(context.Background(), link, password)
		var r *hoster.Refusal
		if !errors.As(err, &r) || r.Reason != core.ReasonAuth || !strings.Contains(r.Msg, want) {
			t.Errorf("password %q: err = %v, want an auth refusal saying %q", password, err, want)
		}
	}
}

func TestAGoneFileIsGone(t *testing.T) {
	srv := wutest.New(t)
	if _, err := NewClient(srv.URL, srv.Client()).Unlock(context.Background(), "https://workupload.com/file/TUcAcRxAyzq", ""); !hoster.IsGone(err) {
		t.Errorf("err = %v, want gone", err)
	}
}

func TestADownloadTheAPITurnsDownIsAWait(t *testing.T) {
	srv := wutest.New(t)
	link := srv.Add("QpDxHzUJcL8", wutest.File{Name: "a.ova", Body: []byte("ova"), Unavailable: true})
	_, err := NewClient(srv.URL, srv.Client()).Unlock(context.Background(), link, "")
	var w *hoster.WaitError
	if !errors.As(err, &w) || w.Wait != unavailableWait || w.Reason != core.ReasonUnavailable {
		t.Errorf("err = %v, want a wait", err)
	}
}

func TestARobotCheckThatStaysUpIsAWait(t *testing.T) {
	srv := wutest.New(t)
	srv.Stubborn = true
	link := srv.Add("QpDxHzUJcL8", wutest.File{Name: "a.ova", Body: []byte("ova")})
	_, err := NewClient(srv.URL, srv.Client()).Unlock(context.Background(), link, "")
	var w *hoster.WaitError
	if !errors.As(err, &w) || w.Wait != robotWait || !strings.Contains(w.Why, "robot check") {
		t.Errorf("err = %v, want a wait for the robot check", err)
	}
}

func TestThePuzzleIsSolvedInTheOrderOfItsNumbers(t *testing.T) {
	// The hashes of "seed" followed by 7, 42 and 9001.
	got, err := solve(puzzle{Puzzle: "seed", Range: 10000, Find: []string{
		hash("seed9001"), hash("seed7"), hash("seed42"),
	}})
	if err != nil || got != "7 42 9001 " {
		t.Errorf("solve = %q, %v", got, err)
	}
	if _, err := solve(puzzle{Puzzle: "seed", Range: 5, Find: []string{hash("seed7")}}); err == nil {
		t.Error("a puzzle without an answer in its range was solved")
	}
	if _, err := solve(puzzle{Puzzle: "seed", Range: maxRange + 1, Find: []string{hash("seed7")}}); err == nil {
		t.Error("a puzzle past any range the site uses was tried")
	}
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

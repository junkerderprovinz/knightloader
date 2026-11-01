package app

import (
	"context"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/jd"
)

// stubInspector is a JD backend whose plugins answer from a fixed table.
type stubInspector struct {
	stubBackend
	answers map[string]jd.LinkInfo
	asked   []string
}

func (s *stubInspector) InspectLinks(_ context.Context, urls []string) ([]jd.LinkInfo, error) {
	s.asked = append(s.asked, urls...)
	out := make([]jd.LinkInfo, len(urls))
	for i, u := range urls {
		out[i] = s.answers[u]
	}
	return out, nil
}

// useJD wires the backend under the lock the app's own goroutines read it with.
func useJD(a *App, be backend) {
	a.bmu.Lock()
	a.jd = be
	a.bmu.Unlock()
}

func collectedLink(a *App, id, url string, mode core.DownloadMode) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tasks[id] = &core.Task{ID: id, URL: url, Name: url, Package: "Isle of Dogs", Status: core.StatusCollected, Enabled: true, Mode: mode}
}

// Links a debrid service would unlock arrive from Click'n'Load as bare
// addresses. JD's plugins name them, size them and say whether they are there.
func TestHosterLinksTakeNameSizeAndStateFromJD(t *testing.T) {
	a := newOrderApp(t)
	const part1 = "https://rg.to/file/aaa"
	const gone = "https://rg.to/file/bbb"
	const plain = "https://example.com/holiday.zip"
	collectedLink(a, "p1", part1, core.ModePremium)
	collectedLink(a, "gone", gone, core.ModePremium)
	collectedLink(a, "plain", plain, core.ModeUnknown)
	stub := &stubInspector{answers: map[string]jd.LinkInfo{
		part1: {Availability: core.AvailOnline, Name: "isle.of.dogs.part1.rar", Size: 4 << 30},
		gone:  {Availability: core.AvailOffline, Name: "bbb"},
	}}
	useJD(a, stub)

	a.inspectHosterLinks([]string{"p1", "gone", "plain"})

	a.mu.Lock()
	defer a.mu.Unlock()
	if p := a.tasks["p1"]; p.Name != "isle.of.dogs.part1.rar" || p.Size != 4<<30 || p.Online != core.AvailOnline {
		t.Errorf("online part = name %q, size %d, state %q; want JD's name, its size and online", p.Name, p.Size, p.Online)
	}
	if g := a.tasks["gone"]; g.Name != gone || g.Online != core.AvailOffline {
		t.Errorf("offline part = name %q, state %q; want the address kept and offline", g.Name, g.Online)
	}
	for _, u := range stub.asked {
		if u == plain {
			t.Error("a plain file was sent to JD; it has its own HEAD probe")
		}
	}
}

// A name the link already had, from the site or a rule, stays.
func TestJDDoesNotRenameANamedLink(t *testing.T) {
	a := newOrderApp(t)
	const part = "https://rg.to/file/aaa"
	collectedLink(a, "p1", part, core.ModePremium)
	a.mu.Lock()
	a.tasks["p1"].Name = "chosen.rar"
	a.mu.Unlock()
	useJD(a, &stubInspector{answers: map[string]jd.LinkInfo{part: {Availability: core.AvailOnline, Name: "plugin.rar", Size: 10}}})

	a.inspectHosterLinks([]string{"p1"})

	a.mu.Lock()
	defer a.mu.Unlock()
	if got := a.tasks["p1"].Name; got != "chosen.rar" {
		t.Errorf("name = %q, want the one the link already had", got)
	}
}

package proxycfg

import (
	"reflect"
	"sync"
	"testing"
)

// catchAll is a proxy entry with no filter, the shape a whole-app proxy has.
func catchAll(id, host string) Entry {
	return Entry{ID: id, Kind: KindHTTP, Host: host, Port: 8080, Enabled: true}
}

// TestPickRoundRobinsInListOrder: equal connections take turns instead of
// piling onto the first that fits.
func TestPickRoundRobinsInListOrder(t *testing.T) {
	p := NewPicker([]Entry{
		catchAll("a", "a.lan"),
		catchAll("b", "b.lan"),
		catchAll("c", "c.lan"),
	}, Options{})
	want := []string{"a", "b", "c", "a", "b", "c", "a"}
	for i, id := range want {
		got, ok := p.Pick("example.org", nil)
		if !ok {
			t.Fatalf("pick %d: no connection offered", i)
		}
		if got.ID != id {
			t.Fatalf("pick %d = %q, want %q", i, got.ID, id)
		}
	}
}

// TestPickWalksTheUsersOrderNotTheInputOrder: a list posted from a
// drag-and-drop UI arrives in DOM order, and only the order index counts.
func TestPickWalksTheUsersOrderNotTheInputOrder(t *testing.T) {
	in := []Entry{
		{ID: "third", Kind: KindHTTP, Host: "c.lan", Port: 8080, Order: 2, Enabled: true},
		{ID: "first", Kind: KindHTTP, Host: "a.lan", Port: 8080, Order: 0, Enabled: true},
		{ID: "second", Kind: KindHTTP, Host: "b.lan", Port: 8080, Order: 1, Enabled: true},
	}
	p := NewPicker(in, Options{})
	for i, want := range []string{"first", "second", "third"} {
		got, ok := p.Pick("example.org", nil)
		if !ok || got.ID != want {
			t.Fatalf("pick %d = %q,%v, want %q", i, got.ID, ok, want)
		}
	}
}

// TestPickNeverExceedsALimit: exceeding a connection's limit risks the very
// ban the user set it to avoid.
func TestPickNeverExceedsALimit(t *testing.T) {
	p := NewPicker([]Entry{
		func() Entry { e := catchAll("a", "a.lan"); e.MaxDownloads = 1; return e }(),
		func() Entry { e := catchAll("b", "b.lan"); e.MaxDownloads = 2; return e }(),
		catchAll("c", "c.lan"), // no limit of its own: the picker default applies
	}, Options{DefaultMaxDownloads: 2})

	inUse := map[string]int{}
	total := 0
	for i := 0; i < 50; i++ {
		e, ok := p.Pick("example.org", inUse)
		if !ok {
			break
		}
		inUse[e.ID]++
		total++
	}
	want := map[string]int{"a": 1, "b": 2, "c": 2}
	for id, n := range want {
		if inUse[id] != n {
			t.Errorf("entry %q was handed out %d times, want %d", id, inUse[id], n)
		}
	}
	if total != 5 {
		t.Errorf("handed out %d connections in total, want 5", total)
	}
	// Everything is busy, so the answer is wait, not direct, which would bypass
	// the configured proxies.
	e, ok := p.Pick("example.org", inUse)
	if ok {
		t.Fatalf("Pick with everything busy = %v,%v, want no connection", e, ok)
	}
	if !reflect.DeepEqual(e, Entry{}) {
		t.Errorf("Pick returned %+v alongside ok=false, want the zero entry", e)
	}
}

// TestPickPrefersAFilteredEntryOverTheCatchAll is the NAS exclusion from the
// package documentation.
func TestPickPrefersAFilteredEntryOverTheCatchAll(t *testing.T) {
	p := NewPicker([]Entry{
		catchAll("proxy", "proxy.lan"),
		{ID: "nas", Kind: KindDirect, Filter: []string{"nas.local"}, Order: 1, Enabled: true},
	}, Options{})

	for i := 0; i < 4; i++ {
		got, ok := p.Pick("nas.local", nil)
		if !ok || got.ID != "nas" {
			t.Fatalf("pick %d for the NAS = %q,%v, want the direct entry", i, got.ID, ok)
		}
		// A direct entry means no proxy URL at all.
		if u := got.URL(); u != nil {
			t.Fatalf("the direct entry produced a proxy URL %v", u)
		}
	}
	got, ok := p.Pick("example.org", nil)
	if !ok || got.ID != "proxy" {
		t.Fatalf("pick for a normal host = %q,%v, want the catch-all proxy", got.ID, ok)
	}
	if u := got.URL(); u == nil || u.String() != "http://proxy.lan:8080" {
		t.Fatalf("the catch-all produced %v, want the configured proxy URL", u)
	}
}

// TestPickIgnoresAFilterThatDoesNotMatch: an entry restricted to one hoster is
// never handed out for another, even as the only enabled entry.
func TestPickIgnoresAFilterThatDoesNotMatch(t *testing.T) {
	p := NewPicker([]Entry{
		{ID: "only-example", Kind: KindHTTP, Host: "proxy.lan", Port: 8080, Filter: []string{"example.org"}, Enabled: true},
	}, Options{})
	got, ok := p.Pick("other.net", nil)
	if !ok {
		t.Fatalf("Pick refused to answer for an unclaimed host")
	}
	if got.ID != DirectID || got.Kind != KindDirect {
		t.Fatalf("Pick = %+v, want the direct gateway", got)
	}
}

// TestPickFallsBackToDirectWhenNothingClaimsTheHost: a list that claims
// nothing lets downloads run rather than freezing the queue.
func TestPickFallsBackToDirectWhenNothingClaimsTheHost(t *testing.T) {
	cases := []struct {
		name string
		in   []Entry
	}{
		{"no connections configured", nil},
		{"every entry switched off", []Entry{{ID: "a", Kind: KindHTTP, Host: "proxy.lan", Port: 8080}}},
		{"an inert row", []Entry{{ID: "a", Kind: KindNone, Enabled: true}}},
		{"filters that all point elsewhere", []Entry{
			{ID: "a", Kind: KindHTTP, Host: "proxy.lan", Port: 8080, Filter: []string{"example.org"}, Enabled: true},
			{ID: "b", Kind: KindDirect, Filter: []string{"nas.local"}, Order: 1, Enabled: true},
		}},
		{"only rows that were dropped as unusable", []Entry{{ID: "a", Kind: KindHTTP, Port: 8080, Enabled: true}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := NewPicker(c.in, Options{})
			got, ok := p.Pick("other.net", map[string]int{})
			if !ok {
				t.Fatalf("Pick = %v,%v, want a direct download rather than a stalled queue", got, ok)
			}
			if got.Kind != KindDirect || got.ID != DirectID {
				t.Fatalf("Pick = %+v, want the direct gateway", got)
			}
			if u := got.URL(); u != nil {
				t.Fatalf("the fallback produced a proxy URL %v", u)
			}
		})
	}
}

// TestPickWaitsRatherThanLeakWhenTheClaimingEntryIsBusy: a host a proxy claims
// waits for that proxy instead of going direct.
func TestPickWaitsRatherThanLeakWhenTheClaimingEntryIsBusy(t *testing.T) {
	p := NewPicker([]Entry{
		{ID: "ex", Kind: KindHTTP, Host: "proxy.lan", Port: 8080, Filter: []string{"example.org"}, MaxDownloads: 1, Enabled: true},
		catchAll("other", "other.lan"),
	}, Options{})

	inUse := map[string]int{"ex": 1}
	if got, ok := p.Pick("dl2.example.org", inUse); ok {
		t.Fatalf("Pick = %+v,%v, want no connection while the claiming proxy is full", got, ok)
	}
	// The idle catch-all does not claim this host.
	if got, ok := p.Pick("dl2.example.org", inUse); ok && got.ID == "other" {
		t.Fatalf("Pick fell through to the catch-all proxy for a claimed host")
	}
	// A host nobody claims still runs.
	if got, ok := p.Pick("elsewhere.net", inUse); !ok || got.ID != "other" {
		t.Fatalf("Pick for an unclaimed host = %q,%v, want the catch-all", got.ID, ok)
	}
}

// TestPickSkipsSwitchedOffAndInertRows: neither is handed out, and neither
// blocks the entries behind it.
func TestPickSkipsSwitchedOffAndInertRows(t *testing.T) {
	p := NewPicker([]Entry{
		{ID: "off", Kind: KindHTTP, Host: "a.lan", Port: 8080, Order: 0},
		{ID: "inert", Kind: KindNone, Order: 1, Enabled: true},
		catchAll("live", "c.lan"),
	}, Options{})
	for i := 0; i < 3; i++ {
		got, ok := p.Pick("example.org", nil)
		if !ok || got.ID != "live" {
			t.Fatalf("pick %d = %q,%v, want the only live entry", i, got.ID, ok)
		}
	}
}

// TestNewPickerSanitizesItsInput: a proxy without an endpoint is dropped, and
// Entries returns a copy the caller cannot use to change the live picker.
func TestNewPickerSanitizesItsInput(t *testing.T) {
	p := NewPicker([]Entry{
		{ID: "broken", Kind: KindHTTP, Port: 8080, Enabled: true},
		catchAll("good", "good.lan"),
	}, Options{})
	list := p.Entries()
	if len(list) != 1 || list[0].ID != "good" {
		t.Fatalf("Entries() = %+v, want only the usable entry", list)
	}
	list[0].Host = "evil.lan"
	if p.Entries()[0].Host != "good.lan" {
		t.Fatalf("Entries() handed out the picker's own slice")
	}
	got, ok := p.Pick("example.org", nil)
	if !ok || got.ID != "good" {
		t.Fatalf("Pick = %q,%v, want the usable entry", got.ID, ok)
	}
}

// TestEntriesAndPickCopyTheFilterToo: Filter is a slice, so a struct copy
// alone would let a caller editing its entry change the running picker's
// filter.
func TestEntriesAndPickCopyTheFilterToo(t *testing.T) {
	entries := []Entry{{ID: "nas", Kind: KindDirect, Filter: []string{"nas.local"}, Enabled: true}}

	t.Run("Entries", func(t *testing.T) {
		p := NewPicker(entries, Options{})
		p.Entries()[0].Filter[0] = "evil.example"
		if f := p.Entries()[0].Filter[0]; f != "nas.local" {
			t.Fatalf("the picker's filter became %q", f)
		}
		if got, ok := p.Pick("nas.local", nil); !ok || got.ID != "nas" {
			t.Fatalf("Pick for the NAS = %q,%v, want the direct entry", got.ID, ok)
		}
	})
	t.Run("Pick", func(t *testing.T) {
		p := NewPicker(entries, Options{})
		got, _ := p.Pick("nas.local", nil)
		got.Filter[0] = "evil.example"
		if f := p.Entries()[0].Filter[0]; f != "nas.local" {
			t.Fatalf("the picker's filter became %q", f)
		}
		if again, ok := p.Pick("nas.local", nil); !ok || again.ID != "nas" {
			t.Fatalf("Pick for the NAS = %q,%v, want the direct entry", again.ID, ok)
		}
	})
	// The caller's own list is left unchanged too.
	if entries[0].Filter[0] != "nas.local" {
		t.Fatalf("NewPicker edited its caller's filter: %v", entries[0].Filter)
	}
}

// TestLimitPrefersTheEntryThenTheOption pins the precedence and the fallbacks,
// including the Options zero value.
func TestLimitPrefersTheEntryThenTheOption(t *testing.T) {
	cases := []struct {
		name string
		opt  Options
		e    Entry
		want int
	}{
		{"entry wins", Options{DefaultMaxDownloads: 3}, Entry{MaxDownloads: 7}, 7},
		{"option applies when the entry says nothing", Options{DefaultMaxDownloads: 3}, Entry{}, 3},
		{"zero option means the package default", Options{}, Entry{}, DefaultMaxDownloads},
		{"a negative option means the package default", Options{DefaultMaxDownloads: -1}, Entry{}, DefaultMaxDownloads},
		{"an absurd option is capped", Options{DefaultMaxDownloads: 5000}, Entry{}, maxDownloadsCap},
		// Limit may be called with an entry that never went through Sanitize.
		{"an absurd entry limit is capped too", Options{}, Entry{MaxDownloads: 1 << 30}, maxDownloadsCap},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NewPicker(nil, c.opt).Limit(c.e); got != c.want {
				t.Errorf("Limit() = %d, want %d", got, c.want)
			}
		})
	}
}

// TestPickNeverGoesOverALimitUnderConcurrency runs several dispatchers against
// one shared count, as app.go does. It is also the worked example of the
// Picker contract: the caller's lock makes the pick and the increment one
// step, without which two goroutines could both take a connection's last slot.
func TestPickNeverGoesOverALimitUnderConcurrency(t *testing.T) {
	limits := map[string]int{"a": 1, "b": 2, "c": 3}
	p := NewPicker([]Entry{
		func() Entry { e := catchAll("a", "a.lan"); e.MaxDownloads = limits["a"]; return e }(),
		func() Entry { e := catchAll("b", "b.lan"); e.MaxDownloads = limits["b"]; return e }(),
		func() Entry { e := catchAll("c", "c.lan"); e.MaxDownloads = limits["c"]; return e }(),
	}, Options{})

	var (
		mu    sync.Mutex
		inUse = map[string]int{}
		peak  = map[string]int{}
	)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				mu.Lock()
				e, ok := p.Pick("example.org", inUse)
				if ok {
					inUse[e.ID]++
					if inUse[e.ID] > peak[e.ID] {
						peak[e.ID] = inUse[e.ID]
					}
				}
				mu.Unlock()
				if !ok {
					continue
				}
				mu.Lock()
				inUse[e.ID]-- // the download finished
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	for id, limit := range limits {
		if peak[id] > limit {
			t.Errorf("entry %q was handed out to %d downloads at once, limit %d", id, peak[id], limit)
		}
		// Otherwise the test would pass by never handing anything out.
		if peak[id] == 0 {
			t.Errorf("entry %q was never used, so the limit was never tested", id)
		}
	}
	if len(inUse) > 0 {
		for id, n := range inUse {
			if n != 0 {
				t.Errorf("entry %q ended with %d downloads still counted", id, n)
			}
		}
	}
}

// TestPickNeverReturnsAnEntryAtItsLimit states the same guarantee as a pure
// function of the counts, independent of the caller's locking.
func TestPickNeverReturnsAnEntryAtItsLimit(t *testing.T) {
	cases := []struct {
		name  string
		inUse map[string]int
		want  string // entry ID, "" for the direct fallback, "-" for no answer
	}{
		{"nothing running", map[string]int{}, "a"},
		{"first one full", map[string]int{"a": 2}, "b"},
		{"first one over its limit already", map[string]int{"a": 99}, "b"},
		{"all but the last full", map[string]int{"a": 2, "b": 1}, "c"},
		{"everything full", map[string]int{"a": 2, "b": 1, "c": 2}, "-"},
		{"a count for an entry that is gone is ignored", map[string]int{"ghost": 99}, "a"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := NewPicker([]Entry{
				catchAll("a", "a.lan"),
				func() Entry { e := catchAll("b", "b.lan"); e.MaxDownloads = 1; return e }(),
				catchAll("c", "c.lan"),
			}, Options{DefaultMaxDownloads: 2})
			got, ok := p.Pick("example.org", c.inUse)
			if c.want == "-" {
				if ok {
					t.Fatalf("Pick = %+v,%v, want no connection", got, ok)
				}
				return
			}
			if !ok || got.ID != c.want {
				t.Fatalf("Pick = %q,%v, want %q", got.ID, ok, c.want)
			}
			if n := c.inUse[got.ID]; n >= p.Limit(got) {
				t.Fatalf("Pick returned %q which is already at %d of %d", got.ID, n, p.Limit(got))
			}
		})
	}
}

// TestPickSkipsABannedEntry: like a full entry, a banned one is walked past
// and the next in the rotation takes the download.
func TestPickSkipsABannedEntry(t *testing.T) {
	bans := NewBans()
	p := NewPicker([]Entry{catchAll("a", "a.lan"), catchAll("b", "b.lan")}, Options{Bans: bans})
	bans.Ban("a", "example.org")
	for i := 0; i < 3; i++ {
		got, ok := p.Pick("example.org", nil)
		if !ok || got.ID != "b" {
			t.Fatalf("pick %d = %q,%v, want the connection the host has not refused", i, got.ID, ok)
		}
	}
	// The ban concerns one hoster only.
	if got, ok := p.Pick("elsewhere.net", nil); !ok || got.ID != "a" {
		t.Fatalf("Pick for another host = %q,%v, want the banned-elsewhere connection", got.ID, ok)
	}
}

// TestPickWaitsRatherThanLeakWhenEveryClaimingEntryIsBanned: a host filtered
// onto a proxy that it refused must not fall through to a plain download.
func TestPickWaitsRatherThanLeakWhenEveryClaimingEntryIsBanned(t *testing.T) {
	bans := NewBans()
	p := NewPicker([]Entry{
		{ID: "claims", Kind: KindHTTP, Host: "proxy.lan", Port: 8080, Filter: []string{"example.org"}, Enabled: true},
		catchAll("other", "other.lan"),
	}, Options{Bans: bans})
	bans.Ban("claims", "dl2.example.org")

	got, ok := p.Pick("dl2.example.org", nil)
	if ok {
		t.Fatalf("Pick = %+v, want no answer rather than a download around the ban", got)
	}
	// Unrelated hosts are unaffected: the catch-all still serves them.
	if got, ok := p.Pick("elsewhere.net", nil); !ok || got.ID != "other" {
		t.Fatalf("Pick for an unclaimed host = %q,%v, want the catch-all", got.ID, ok)
	}
}

// TestPickForHonoursTheNamedConnection: a task that names a connection gets
// it, not its turn in the rotation.
func TestPickForHonoursTheNamedConnection(t *testing.T) {
	p := NewPicker([]Entry{catchAll("a", "a.lan"), catchAll("b", "b.lan")}, Options{})
	for i := 0; i < 3; i++ {
		got, ok := p.PickFor("b", "example.org", nil)
		if !ok || got.ID != "b" {
			t.Fatalf("pick %d = %q,%v, want the named connection every time", i, got.ID, ok)
		}
	}
	// The rotation did not advance.
	if got, ok := p.PickFor("", "example.org", nil); !ok || got.ID != "a" {
		t.Fatalf("the unrouted pick = %q,%v, want the head of the rotation", got.ID, ok)
	}
}

// TestPickForTheDirectGateway: no filter, limit or ban applies to a download
// that names the direct gateway.
func TestPickForTheDirectGateway(t *testing.T) {
	bans := NewBans()
	p := NewPicker([]Entry{
		{ID: "claims", Kind: KindHTTP, Host: "proxy.lan", Port: 8080, Filter: []string{"example.org"}, Enabled: true},
	}, Options{Bans: bans})
	bans.Ban(DirectID, "example.org")

	got, ok := p.PickFor(DirectID, "example.org", map[string]int{DirectID: 99})
	if !ok || got.ID != DirectID || got.Kind != KindDirect {
		t.Fatalf("PickFor(direct) = %+v,%v, want the gateway", got, ok)
	}
	if n := p.Limit(got); n <= DefaultMaxDownloads {
		t.Fatalf("Limit(gateway) = %d, want a ceiling the list does not impose", n)
	}
}

// TestPickForWaitsWhenTheNamedConnectionCannotTakeIt: a busy or refused named
// connection is never swapped for another.
func TestPickForWaitsWhenTheNamedConnectionCannotTakeIt(t *testing.T) {
	bans := NewBans()
	p := NewPicker([]Entry{catchAll("a", "a.lan"), catchAll("b", "b.lan")}, Options{
		DefaultMaxDownloads: 1,
		Bans:                bans,
	})
	if got, ok := p.PickFor("a", "example.org", map[string]int{"a": 1}); ok {
		t.Fatalf("PickFor on a full connection = %+v, want no answer", got)
	}
	bans.Ban("a", "example.org")
	if got, ok := p.PickFor("a", "example.org", nil); ok {
		t.Fatalf("PickFor on a banned connection = %+v, want no answer", got)
	}
}

// TestPickForFallsBackToTheRotationWhenTheChoiceIsGone: a task pointing at a
// deleted or switched-off row is not stranded.
func TestPickForFallsBackToTheRotationWhenTheChoiceIsGone(t *testing.T) {
	off := catchAll("off", "off.lan")
	off.Enabled = false
	p := NewPicker([]Entry{catchAll("live", "live.lan"), off}, Options{})
	for _, id := range []string{"deleted-months-ago", "off"} {
		got, ok := p.PickFor(id, "example.org", nil)
		if !ok || got.ID != "live" {
			t.Fatalf("PickFor(%q) = %q,%v, want the rotation's own answer", id, got.ID, ok)
		}
	}
	// The fallback still honours host filters.
	q := NewPicker([]Entry{
		{ID: "nas", Kind: KindDirect, Filter: []string{"nas.local"}, Enabled: true},
		catchAll("proxy", "proxy.lan"),
	}, Options{})
	if got, ok := q.PickFor("deleted", "nas.local", nil); !ok || got.ID != "nas" {
		t.Fatalf("PickFor for the NAS = %q,%v, want the direct row that claims it", got.ID, ok)
	}
}

// TestARowCannotClaimTheGatewaysID: a row holding "direct" would take over
// tasks pinned to the gateway.
func TestARowCannotClaimTheGatewaysID(t *testing.T) {
	p := NewPicker([]Entry{
		{ID: DirectID, Kind: KindHTTP, Host: "impostor.lan", Port: 8080, Enabled: true},
	}, Options{})
	if id := p.Entries()[0].ID; id == DirectID {
		t.Fatal("a proxy row kept the direct gateway's id")
	}
	got, ok := p.PickFor(DirectID, "example.org", nil)
	if !ok || got.Kind != KindDirect || got.Host != "" {
		t.Fatalf("PickFor(direct) = %+v,%v, want the gateway and not the row that tried to be it", got, ok)
	}
}

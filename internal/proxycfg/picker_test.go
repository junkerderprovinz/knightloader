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

// TestPickRoundRobinsInListOrder pins the spreading the list exists for: three
// equal connections must take turns, not pile onto the first one that fits.
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

// TestPickWalksTheUsersOrderNotTheInputOrder makes sure the picker follows the
// list the user is looking at. A list posted back from a drag-and-drop UI
// arrives in whatever sequence the DOM had; only the order index is the answer.
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

// TestPickNeverExceedsALimit is the guarantee a caller cannot check for itself.
// Handing one connection out beyond its limit is exactly the ban the user set
// the limit to avoid.
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
	// Everything is busy, so the answer is "wait", not "go direct": going direct
	// would route around the proxies the user configured.
	e, ok := p.Pick("example.org", inUse)
	if ok {
		t.Fatalf("Pick with everything busy = %v,%v, want no connection", e, ok)
	}
	if !reflect.DeepEqual(e, Entry{}) {
		t.Errorf("Pick returned %+v alongside ok=false, want the zero entry", e)
	}
}

// TestPickPrefersAFilteredEntryOverTheCatchAll is the NAS exclusion from the
// package comment. Without the preference the whole-app proxy would keep taking
// its turn on nas.local and half the LAN transfers would leave the house.
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
		// A direct entry means no proxy at all, which is what the caller has to
		// see for the exclusion to do anything.
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

// TestPickIgnoresAFilterThatDoesNotMatch pins the other half of the filter: an
// entry restricted to one hoster must never be handed out for another, even when
// it is the only enabled entry left.
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

// TestPickFallsBackToDirectWhenNothingClaimsTheHost is the failure that would
// hurt most: a list that claims nothing must let downloads run. A mistyped
// filter freezing the entire queue is far worse than a download going out
// unproxied, and the caller cannot tell the two apart on its own.
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

// TestPickWaitsRatherThanLeakWhenTheClaimingEntryIsBusy separates the two
// negative answers. A host that a proxy claims must wait for that proxy; falling
// back to direct here is the leak the whole feature exists to prevent.
func TestPickWaitsRatherThanLeakWhenTheClaimingEntryIsBusy(t *testing.T) {
	p := NewPicker([]Entry{
		{ID: "ex", Kind: KindHTTP, Host: "proxy.lan", Port: 8080, Filter: []string{"example.org"}, MaxDownloads: 1, Enabled: true},
		catchAll("other", "other.lan"),
	}, Options{})

	inUse := map[string]int{"ex": 1}
	if got, ok := p.Pick("dl2.example.org", inUse); ok {
		t.Fatalf("Pick = %+v,%v, want no connection while the claiming proxy is full", got, ok)
	}
	// The catch-all is idle, but it does not claim this host and must not be
	// used for it.
	if got, ok := p.Pick("dl2.example.org", inUse); ok && got.ID == "other" {
		t.Fatalf("Pick fell through to the catch-all proxy for a claimed host")
	}
	// A host nobody claims still runs, so one busy filtered entry does not stall
	// everything else.
	if got, ok := p.Pick("elsewhere.net", inUse); !ok || got.ID != "other" {
		t.Fatalf("Pick for an unclaimed host = %q,%v, want the catch-all", got.ID, ok)
	}
}

// TestPickSkipsSwitchedOffAndInertRows checks the two user-facing switches:
// neither may ever be handed out, and neither may block the entries behind it.
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

// TestNewPickerSanitizesItsInput means a caller that forgot cannot round-robin
// onto a proxy with no endpoint, and that Entries is a copy: a caller editing
// what it got back must not silently re-point a live picker.
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

// TestEntriesAndPickCopyTheFilterToo is the half of the copy that a struct copy
// does not cover. Host is a string and copies itself; Filter is a slice, so a
// caller that edits the list it was handed - an API handler redacting a row, a
// UI writing a rename back - would otherwise reach straight into the running
// picker, from another goroutine and with no write the picker can see. The entry
// then stops claiming the host it was written for and the next download for that
// hoster goes out over the connection the filter existed to avoid.
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
	// The list the picker was built from is the caller's and must come back
	// unharmed too, or the second picker built from it walks something else.
	if entries[0].Filter[0] != "nas.local" {
		t.Fatalf("NewPicker edited its caller's filter: %v", entries[0].Filter)
	}
}

// TestLimitPrefersTheEntryThenTheOption pins the precedence and the fallbacks,
// including an Options zero value, which is what a caller that has no opinion
// passes.
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
		// Limit is exported and will be called with an entry that came off a
		// request rather than out of Sanitize, and a limit above anything the app
		// will ever run at once is not a limit.
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

// TestPickNeverGoesOverALimitUnderConcurrency is the guarantee under the
// conditions app.go runs in: several dispatchers picking against one shared
// count, downloads finishing and giving their slot back. It is also the worked
// example of the contract on the Picker type, because Pick cannot reserve
// anything in a map it does not own: the caller's lock is what makes the pick
// and the increment one step. Drop that lock and two goroutines read the same
// count and both take the last slot on a connection, which is the ban the user
// set the limit to avoid; the shared map would also be written from two
// goroutines at once, which the runtime kills the process for.
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
		// Without this the test could pass by never handing anything out at all.
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

// TestPickNeverReturnsAnEntryAtItsLimit is the same guarantee stated as the pure
// function it is, so it holds whatever the caller's locking looks like: given a
// count, an entry already at that count is not offered.
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
			// A refused pick must not have moved the caller's counts: the picker
			// is the only thing that could have, and app.go's map is the truth
			// about what is running.
			if n := c.inUse[got.ID]; n >= p.Limit(got) {
				t.Fatalf("Pick returned %q which is already at %d of %d", got.ID, n, p.Limit(got))
			}
		})
	}
}

// TestPickSkipsABannedEntry. A refusal is the same answer to the caller as a
// limit - not this connection, not now - so a banned entry is walked past and
// the next one in the rotation takes the download.
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
	// The ban is about one hoster and must not have cost the connection its place
	// in the rotation everywhere else.
	if got, ok := p.Pick("elsewhere.net", nil); !ok || got.ID != "a" {
		t.Fatalf("Pick for another host = %q,%v, want the banned-elsewhere connection", got.ID, ok)
	}
}

// TestPickWaitsRatherThanLeakWhenEveryClaimingEntryIsBanned is the ban list's
// version of the leak this package exists to prevent. A host filtered onto one
// proxy, and that proxy refused, must not fall through to a plain download: the
// traffic the filter was written to route would go out over the very connection
// the user was hiding.
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

// TestPickForHonoursTheNamedConnection is per-download routing seen from the
// picker: a task that names a connection gets that connection, not its turn in
// the rotation.
func TestPickForHonoursTheNamedConnection(t *testing.T) {
	p := NewPicker([]Entry{catchAll("a", "a.lan"), catchAll("b", "b.lan")}, Options{})
	for i := 0; i < 3; i++ {
		got, ok := p.PickFor("b", "example.org", nil)
		if !ok || got.ID != "b" {
			t.Fatalf("pick %d = %q,%v, want the named connection every time", i, got.ID, ok)
		}
	}
	// And the rotation was not advanced by any of it, so an unrouted download
	// still starts where the list starts.
	if got, ok := p.PickFor("", "example.org", nil); !ok || got.ID != "a" {
		t.Fatalf("the unrouted pick = %q,%v, want the head of the rotation", got.ID, ok)
	}
}

// TestPickForTheDirectGateway. "No proxy" is a choice in the list, so naming it
// is honoured outright: no filter, no limit and no ban stand between a download
// and the machine's own connection.
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

// TestPickForWaitsWhenTheNamedConnectionCannotTakeIt. The user named this
// connection; swapping in another one because this one is busy or refused is
// routing the download around the choice that was made for it.
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

// TestPickForFallsBackToTheRotationWhenTheChoiceIsGone. A preference is not a
// lock: a task pointing at a row somebody deleted, or switched off for the
// evening, must not be stranded for ever by it.
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
	// The filters the rotation honours are still honoured, so the fallback is not
	// a way past an exclusion the user wrote.
	q := NewPicker([]Entry{
		{ID: "nas", Kind: KindDirect, Filter: []string{"nas.local"}, Enabled: true},
		catchAll("proxy", "proxy.lan"),
	}, Options{})
	if got, ok := q.PickFor("deleted", "nas.local", nil); !ok || got.ID != "nas" {
		t.Fatalf("PickFor for the NAS = %q,%v, want the direct row that claims it", got.ID, ok)
	}
}

// TestARowCannotClaimTheGatewaysID. A row holding "direct" would shadow the
// gateway everywhere a connection is named by id, and a task pinned to direct
// would start going out over whatever that row points at.
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

package proxycfg

import (
	"reflect"
	"testing"
)

// TestBanIsPerHostNotPerConnection is the shape of the whole feature: one hoster
// refusing a proxy must not take that proxy away from every other hoster.
func TestBanIsPerHostNotPerConnection(t *testing.T) {
	b := NewBans()
	b.Ban("a", "rapidgator.net")
	if !b.Banned("a", "rapidgator.net") {
		t.Fatal("the host that refused the connection is not on the ban list")
	}
	if b.Banned("a", "example.org") {
		t.Fatal("a refusal by one host took the connection away from another")
	}
	if b.Banned("b", "rapidgator.net") {
		t.Fatal("a refusal of one connection was applied to another")
	}
}

// TestBanFoldsTheHostTheSameWayFiltersDo. A ban recorded from a URL and a ban
// asked about by host name have to be the same ban, or the list silently holds
// entries nothing ever matches.
func TestBanFoldsTheHostTheSameWayFiltersDo(t *testing.T) {
	b := NewBans()
	b.Ban("a", "  DL2.Example.ORG:443 ")
	if !b.Banned("a", "dl2.example.org") {
		t.Fatal("a ban recorded with a port and mixed case did not match the plain host")
	}
	if got := b.Hosts("a"); !reflect.DeepEqual(got, []string{"dl2.example.org"}) {
		t.Fatalf("Hosts = %v, want the folded host", got)
	}
}

// TestTheGatewayIsNeverBanned. The direct gateway is the answer of last resort;
// banning it leaves a host with nowhere at all to go, which stalls the queue
// rather than protecting anything.
func TestTheGatewayIsNeverBanned(t *testing.T) {
	b := NewBans()
	b.Ban(DirectID, "example.org")
	b.Ban("", "example.org")
	if b.Banned(DirectID, "example.org") {
		t.Fatal("the direct gateway was banned")
	}
	if b.Banned("", "example.org") {
		t.Fatal("a blank connection id was banned")
	}
}

// TestClearForgetsOneConnectionOnly is the "try this proxy again" button.
func TestClearForgetsOneConnectionOnly(t *testing.T) {
	b := NewBans()
	b.Ban("a", "example.org")
	b.Ban("b", "example.org")
	b.Clear("a")
	if b.Banned("a", "example.org") {
		t.Fatal("Clear left the connection banned")
	}
	if !b.Banned("b", "example.org") {
		t.Fatal("Clear reached a connection it was not asked about")
	}
	b.ClearAll()
	if b.Banned("b", "example.org") {
		t.Fatal("ClearAll left a ban behind")
	}
}

// TestSwitchingARowBackOnClearsItsBans is the false->true edge of the Use
// switch, and it is the reason observe exists. A connection somebody switched
// off and on again has been given a fresh start; coming back still refused by
// yesterday's hosts is an inheritance that is invisible on the page, because the
// row reads as on and downloads still avoid it.
func TestSwitchingARowBackOnClearsItsBans(t *testing.T) {
	on := Entry{ID: "a", Kind: KindHTTP, Host: "proxy.lan", Port: 8080, Enabled: true}
	off := on
	off.Enabled = false

	bans := NewBans()
	NewPicker([]Entry{on}, Options{Bans: bans})
	bans.Ban("a", "example.org")

	// Saved with the switch off: the bans stay, because nothing has been retried.
	NewPicker([]Entry{off}, Options{Bans: bans})
	if !bans.Banned("a", "example.org") {
		t.Fatal("switching a row off cleared its bans; only switching it back on may")
	}

	NewPicker([]Entry{on}, Options{Bans: bans})
	if bans.Banned("a", "example.org") {
		t.Fatal("a row switched back on is still carrying the refusals it had before")
	}
}

// TestSavingTheSameListTwiceKeepsTheBans. Every settings save rebuilds the
// picker, so an edit anywhere on the page runs observe over every row. If a
// save that changed nothing about a row cleared its bans, the ban list would
// last exactly until the next time somebody touched the accent colour.
func TestSavingTheSameListTwiceKeepsTheBans(t *testing.T) {
	row := Entry{ID: "a", Kind: KindHTTP, Host: "proxy.lan", Port: 8080, Enabled: true}
	bans := NewBans()
	NewPicker([]Entry{row}, Options{Bans: bans})
	bans.Ban("a", "example.org")
	for i := 0; i < 3; i++ {
		NewPicker([]Entry{row}, Options{Bans: bans})
	}
	if !bans.Banned("a", "example.org") {
		t.Fatal("re-saving an unchanged list threw the ban away")
	}
}

// TestEditingARowToADifferentProxyClearsItsBans. The refusals belonged to the
// machine the row used to name, not to the row.
func TestEditingARowToADifferentProxyClearsItsBans(t *testing.T) {
	row := Entry{ID: "a", Kind: KindHTTP, Host: "old.lan", Port: 8080, Enabled: true}
	bans := NewBans()
	NewPicker([]Entry{row}, Options{Bans: bans})
	bans.Ban("a", "example.org")

	moved := row
	moved.Host = "new.lan"
	NewPicker([]Entry{moved}, Options{Bans: bans})
	if bans.Banned("a", "example.org") {
		t.Fatal("a row re-pointed at another proxy inherited the old one's refusals")
	}
}

// TestARecycledIDIsNotBornBanned is the trap this whole mechanism is most likely
// to be caught by. identify hands out the lowest free decimal id, so deleting a
// row and adding one gives the newcomer the id the deleted row had - and without
// observe dropping the bans of a row that left the list, the new connection
// starts life refused by hosts it has never spoken to.
func TestARecycledIDIsNotBornBanned(t *testing.T) {
	first := Entry{Kind: KindHTTP, Host: "first.lan", Port: 8080, Enabled: true}
	bans := NewBans()
	p := NewPicker([]Entry{first}, Options{Bans: bans})
	id := p.Entries()[0].ID
	bans.Ban(id, "example.org")

	second := Entry{Kind: KindSOCKS5, Host: "second.lan", Port: 1080, Enabled: true}
	p2 := NewPicker([]Entry{second}, Options{Bans: bans})
	if got := p2.Entries()[0].ID; got != id {
		t.Skipf("identify no longer recycles ids (%q then %q); this trap is gone", id, got)
	}
	if bans.Banned(id, "example.org") {
		t.Fatal("a new connection inherited the bans of the deleted row whose id it reused")
	}
}

// TestNilBansIsAnEmptyBanList. The picker is older than the ban list, and a
// caller that has not been taught about one must behave as it always did rather
// than panic for not knowing.
func TestNilBansIsAnEmptyBanList(t *testing.T) {
	var b *Bans
	b.Ban("a", "example.org")
	b.Clear("a")
	b.ClearAll()
	b.observe([]Entry{{ID: "a", Kind: KindHTTP, Host: "proxy.lan", Port: 8080, Enabled: true}})
	if b.Banned("a", "example.org") {
		t.Fatal("a nil ban list reported a ban")
	}
	if got := b.Hosts("a"); got != nil {
		t.Fatalf("Hosts on a nil ban list = %v, want nil", got)
	}
}

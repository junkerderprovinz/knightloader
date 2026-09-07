package hostheaders

import (
	"testing"
)

func TestSaveGetRemoveRoundTrip(t *testing.T) {
	s := NewStore(mustAccounts(t))
	set, err := Normalize(Set{
		Origin:  "https://cloud.example.org/s/abc/download",
		Headers: []Header{{Name: "Authorization", Value: secretBasic}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save("Nextcloud", set); err != nil {
		t.Fatal(err)
	}
	// The id is normalised on the way in, so a rule that says "nextcloud"
	// finds a profile saved as "Nextcloud".
	if ids := s.IDs(); len(ids) != 1 || ids[0] != "nextcloud" {
		t.Fatalf("IDs = %v, want [nextcloud]", ids)
	}
	if got := s.Get("nextcloud"); got.Origin != "https://cloud.example.org:443" {
		t.Fatalf("Origin = %q, want the origin of the URL that was saved", got.Origin)
	}
	if got := s.Get("nextcloud").Attach("https://cloud.example.org/remote.php/x"); got["Authorization"] != secretBasic {
		t.Error("the stored header did not come back")
	}
	if err := s.Remove("nextcloud"); err != nil {
		t.Fatal(err)
	}
	if ids := s.IDs(); len(ids) != 0 {
		t.Fatalf("IDs = %v after Remove, want none", ids)
	}
	if got := s.Get("nextcloud"); !got.IsZero() {
		t.Errorf("Get after Remove = %v, want the zero Set", got)
	}
}

// TestTheOriginIndexFollowsEveryWrite is the one thing the cache can get
// wrong: Match answers from it while the app's lock is held, so an index that
// outlived the profile it describes would keep claiming links for a credential
// that is no longer there.
func TestTheOriginIndexFollowsEveryWrite(t *testing.T) {
	s := NewStore(mustAccounts(t))
	link := "https://box.lan:8080/files/x.zip"
	if s.Covers(link) {
		t.Fatal("an empty store claims a link")
	}
	set, _ := Normalize(Set{Origin: "http://box.lan:8080", Headers: []Header{{Name: "X-A", Value: "v"}}})
	if err := s.Save("box", set); err != nil {
		t.Fatal(err)
	}
	// http and https on the same host and port are two origins, so the https
	// link is still not covered.
	if s.Covers(link) {
		t.Error("an http profile claims an https link")
	}
	if !s.Covers("http://box.lan:8080/files/x.zip") {
		t.Error("the profile does not claim its own origin")
	}
	if id, got := s.ForURL("http://box.lan:8080/x"); id != "box" || got.IsZero() {
		t.Errorf("ForURL = %q, %v, want the box profile", id, got)
	}
	if err := s.Remove("box"); err != nil {
		t.Fatal(err)
	}
	if s.Covers("http://box.lan:8080/files/x.zip") {
		t.Error("a removed profile still claims its origin")
	}
}

// TestTwoProfilesOnOneOriginResolveTheSameWayEveryTime. Two profiles for one
// origin is a configuration mistake with no right answer, and the wrong way to
// handle it is to let map order pick - a link would then be fetched with a
// different credential after a restart, which is the kind of "it worked
// yesterday" that costs an evening.
func TestTwoProfilesOnOneOriginResolveTheSameWayEveryTime(t *testing.T) {
	s := NewStore(mustAccounts(t))
	for _, id := range []string{"zebra", "alpha", "middle"} {
		set, _ := Normalize(Set{Origin: "https://box.lan", Headers: []Header{{Name: "X-Which", Value: id}}})
		if err := s.Save(id, set); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 5; i++ {
		s.invalidate() // force a rebuild, as a restart would
		id, _ := s.ForURL("https://box.lan/x")
		if id != "alpha" {
			t.Fatalf("ForURL picked %q, want the first id in sort order every time", id)
		}
	}
}

// TestImportTakesTheOriginFromTheFormWhenThePasteHasNone is what makes a bare
// cookie block usable: the user was just looking at the site, and asking them
// to also type it is the step that gets guessed wrong.
func TestImportTakesTheOriginFromTheFormWhenThePasteHasNone(t *testing.T) {
	s := NewStore(mustAccounts(t))
	set, err := s.Import("nc", "https://cloud.example.org/index.php/apps/files", "nc_session=zzz; oc_pass=yyy")
	if err != nil {
		t.Fatal(err)
	}
	if set.Origin != "https://cloud.example.org:443" {
		t.Errorf("Origin = %q, want the one from the form", set.Origin)
	}
	if got := s.Get("nc").Attach("https://cloud.example.org/remote.php/x"); got["Cookie"] == "" {
		t.Error("the imported cookies did not survive the save")
	}
}

// TestImportPrefersThePasteOverTheForm: a curl line already names the address
// it was copied from, and that is the more reliable of the two.
func TestImportPrefersThePasteOverTheForm(t *testing.T) {
	s := NewStore(mustAccounts(t))
	set, err := s.Import("forum", "https://typed-by-hand.example.net",
		`curl 'https://forum.example.org/x' -H 'x-forum-token: abc'`)
	if err != nil {
		t.Fatal(err)
	}
	if set.Origin != "https://forum.example.org:443" {
		t.Errorf("Origin = %q, want the one the curl line names", set.Origin)
	}
}

func TestSaveRefusesANameNothingCouldAddress(t *testing.T) {
	s := NewStore(mustAccounts(t))
	set, _ := Normalize(Set{Origin: "https://box.lan", Headers: []Header{{Name: "X-A", Value: "v"}}})
	for _, id := range []string{"", "  ", "has space", "a/b"} {
		if err := s.Save(id, set); err == nil {
			t.Errorf("Save accepted the profile name %q", id)
		}
	}
}

// TestAStoreWithNoAccountsStoreFailsLoudlyOnAWriteAndQuietlyOnARead. A save
// that reports success and stores nothing is how somebody finds out weeks
// later that their profile was never there; a read on a download path must not
// produce an error string, because that string ends up in the diagnostics
// bundle.
func TestAStoreWithNoAccountsStoreFailsLoudlyOnAWriteAndQuietlyOnARead(t *testing.T) {
	var s *Store
	if err := s.Save("x", Set{Origin: "https://box.lan", Headers: []Header{{Name: "A", Value: "b"}}}); err == nil {
		t.Error("Save on a store with no credential store reported success")
	}
	if got := s.Get("x"); !got.IsZero() {
		t.Errorf("Get = %v, want the zero Set", got)
	}
	if s.Covers("https://box.lan/x") {
		t.Error("Covers said yes with nothing behind it")
	}
	if ids := s.IDs(); ids != nil {
		t.Errorf("IDs = %v, want nil", ids)
	}
}

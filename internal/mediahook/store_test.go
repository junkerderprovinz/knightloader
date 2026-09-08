package mediahook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
)

// planted is the one string every assertion about leaking hunts for: a single
// distinctive needle, so a hit anywhere is unambiguous and a miss is not a
// coincidence. The same arrangement hostheaders' own leak_test.go uses.
const planted = "SECRET-emby-9f3a-nothing-may-print-this"

func mustAccounts(t *testing.T) (*accounts.Store, string) {
	t.Helper()
	dir := t.TempDir()
	a, err := accounts.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return a, dir
}

func TestSetValueGetRemoveRoundTrip(t *testing.T) {
	acc, _ := mustAccounts(t)
	s := NewStore(acc)

	if err := s.SetValue("Jellyfin", planted); err != nil {
		t.Fatal(err)
	}
	// Folded on the way in, so a drawer that says "jellyfin" finds a value
	// stored as "Jellyfin".
	if ids := s.IDs(); len(ids) != 1 || ids[0] != "jellyfin" {
		t.Fatalf("IDs = %v, want [jellyfin]", ids)
	}
	if !s.Has("JELLYFIN") {
		t.Error("Has does not fold the id the way SetValue did")
	}
	if got := s.Value("jellyfin"); got != planted {
		t.Errorf("Value = %q, want the stored value", got)
	}
	if err := s.Remove("jellyfin"); err != nil {
		t.Fatal(err)
	}
	if ids := s.IDs(); len(ids) != 0 {
		t.Fatalf("IDs = %v after Remove, want none", ids)
	}
	if got := s.Value("jellyfin"); got != "" {
		t.Errorf("Value after Remove = %q, want empty", got)
	}
}

// TestAnEmptyValueIsADelete pins the reading every secret in this app gives an
// empty string, and the one that makes the placeholder necessary: without it a
// stored token could never be removed through a settings form at all.
func TestAnEmptyValueIsADelete(t *testing.T) {
	acc, _ := mustAccounts(t)
	s := NewStore(acc)
	if err := s.SetValue("jellyfin", planted); err != nil {
		t.Fatal(err)
	}
	if err := s.SetValue("jellyfin", ""); err != nil {
		t.Fatal(err)
	}
	if s.Has("jellyfin") {
		t.Error("an empty value left the entry in the store")
	}
}

// TestTheValueNeverReachesTheAccountsFileInTheClear is the whole reason this
// store exists rather than a field on the settings row. It reads the file the
// store actually wrote, because that file is what a backup copies and what
// somebody looks at when a bug report goes wrong.
func TestTheValueNeverReachesTheAccountsFileInTheClear(t *testing.T) {
	acc, dir := mustAccounts(t)
	s := NewStore(acc)
	if err := s.SetValue("jellyfin", planted); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), planted) {
		t.Fatal("the header value is in accounts.json in the clear")
	}
	// And it does come back through the store, so the assertion above is about
	// encryption rather than about nothing having been written.
	if got := s.Value("jellyfin"); got != planted {
		t.Fatalf("Value = %q, want the stored value back", got)
	}
}

// TestAStoreWithNoAccountsAnswersRatherThanCrashing covers the App assembled by
// hand in a test and the embedding that wired nothing. The reads are silent and
// the writes are loud, which is the split hostheaders.ErrNoStore draws: a save
// that reports success and stores nothing is how somebody finds out weeks later
// that their token was never there.
func TestAStoreWithNoAccountsAnswersRatherThanCrashing(t *testing.T) {
	var s *Store
	if got := s.Value("jellyfin"); got != "" {
		t.Errorf("Value on a nil store = %q", got)
	}
	if s.Has("jellyfin") || len(s.IDs()) != 0 {
		t.Error("a nil store claims to hold something")
	}
	empty := NewStore(nil)
	if err := empty.SetValue("jellyfin", planted); err == nil {
		t.Error("SetValue with no credential store reported success")
	}
	if err := empty.Remove("jellyfin"); err == nil {
		t.Error("Remove with no credential store reported success")
	}
}

// TestAnUnusableIDStoresNothing keeps the two halves of the id rule together: an
// id HookID refuses is one no drawer could point at, so storing a value under it
// would be a credential nothing can ever read and nothing can ever remove.
func TestAnUnusableIDStoresNothing(t *testing.T) {
	acc, _ := mustAccounts(t)
	s := NewStore(acc)
	if err := s.SetValue("jellyfin lan", planted); err == nil {
		t.Fatal("a value was stored under a name nothing could point at")
	}
	if len(s.IDs()) != 0 {
		t.Errorf("IDs = %v, want none", s.IDs())
	}
}

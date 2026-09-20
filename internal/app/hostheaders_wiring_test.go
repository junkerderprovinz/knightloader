package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/resolver/hostheaders"
)

// The header-profile resolver is registered; unregistered, it would never be
// asked and every link would go to Direct.
func TestHostHeaderProfilesAreRegistered(t *testing.T) {
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	for _, id := range a.Registry.IDs() {
		if id == hostheaders.ResolverID {
			return
		}
	}
	t.Fatalf("resolver %q is not registered, so every stored header profile is unreachable; registry holds %v",
		hostheaders.ResolverID, a.Registry.IDs())
}

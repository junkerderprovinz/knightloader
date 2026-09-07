package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/resolver/hostheaders"
)

// TestHostHeaderProfilesAreRegistered guards the single line that arms the
// header profiles.
//
// Registry.All walks only what Register has seen, so without that line Match
// and Resolve are never called and every link goes to Direct exactly as before.
// The package would be complete, tested and unreachable - the same shape as the
// yt-dlp cookie jars that had nowhere to be handed over, and as the waiting
// reason that never reached a stopped queue. Three times in one day is enough
// to write the guard rather than the comment.
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

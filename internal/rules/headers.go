package rules

// Header profiles live in internal/resolver/hostheaders, sealed in the account
// store, and this package does not check that a named profile exists. Rule sets
// are compiled at boot before the store is read and again by the editor's dry
// run, and a rule naming a profile not yet created is being set up: refusing
// it would drop the rule's folder and package name too, while the resolver
// safely sends no headers for a missing profile. Only the shape of the name is
// checked here, the same shape hostheaders.ProfileID enforces; headers_test.go
// runs the same cases through both.

import (
	"fmt"
	"strings"
)

// MaxHeaderProfile bounds the profile name. It matches
// hostheaders.MaxProfileID, so a longer name could never address a profile.
const MaxHeaderProfile = 64

// headerProfileProblem reports why a header profile name cannot be used, or ""
// when it can. An empty name means the rule has no opinion.
//
// Only letters, digits, '-', '_' and '.' are allowed, because the name is
// typed twice, in the rule editor and in the profile list, and every other
// character is another way for the two to differ invisibly.
func headerProfileProblem(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		return ""
	}
	if len(name) > MaxHeaderProfile {
		return fmt.Sprintf("the header profile name is %d characters, the limit is %d", len(name), MaxHeaderProfile)
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return fmt.Sprintf(
				"the header profile name %q holds a character that is not a letter, a digit, -, _ or .", name)
		}
	}
	return ""
}

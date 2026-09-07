package rules

// headers.go: the one thing this package checks about Action.Headers.
//
// The profile itself lives in internal/resolver/hostheaders, sealed in the
// encrypted account store, and this package deliberately does not import it.
// Two reasons, and the second is the one that matters:
//
//   - A rule set is compiled at boot, before the account store has been read,
//     and it is compiled again by the dry run behind the editor's test box. A
//     Compile that could only answer with a credential store in hand would
//     make the test box need one too.
//   - A rule naming a profile that does not exist YET is a rule somebody is in
//     the middle of setting up. Refusing it would drop the whole rule (Compile
//     drops a rule with any problem at all, on purpose), so the folder and the
//     package name that rule also sets would stop being applied because a
//     credential had not been pasted yet. The resolver's own answer to a
//     missing profile is "send no headers", which is the safe half of the
//     failure, and it is a far better one than a rule silently vanishing.
//
// So the check here is about the SHAPE of the name only, and it is the same
// shape hostheaders.ProfileID enforces on the other side. The two are kept in
// step by rules_headers_test.go, which walks the same cases through both.

import (
	"fmt"
	"strings"
)

// MaxHeaderProfile bounds the profile name. It matches
// hostheaders.MaxProfileID; a longer name simply cannot address a stored
// profile, so accepting one here would produce a rule that compiles cleanly
// and can never do anything.
const MaxHeaderProfile = 64

// headerProfileProblem reports why a header profile name cannot be used, or ""
// when it can. An empty name is not a problem: it is the ordinary "this rule
// has no opinion about headers".
//
// The rules are letters, digits and the three separators, lower case decided
// on the other side. They are narrow on purpose - this string is typed by a
// person in the rule editor and again in the profile list, and the two have to
// match exactly. Every character not on the list is one more way for those two
// spellings to differ invisibly, and a rule pointing at a profile that is not
// quite the one on screen looks exactly like the feature not working.
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

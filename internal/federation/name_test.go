package federation

import "testing"

// TestSanitiseNameProducesAnAddressableName pins the rule SanitiseName exists
// to satisfy: whatever comes out either matches nameRe or is empty, and an
// empty answer only for input with nothing usable in it.
//
// The cases that matter are the ones a real instance actually has: a German
// name with an umlaut, a hostname longer than the limit, a name starting with
// something the rule forbids first position.
func TestSanitiseNameProducesAnAddressableName(t *testing.T) {
	cases := []struct{ in, want string }{
		// Already fine - must be left exactly alone, or a working setup
		// silently renames itself on upgrade.
		{"Bottich", "Bottich"},
		{"Zathallia", "Zathallia"},
		{"DESKTOP-A1B2C3", "DESKTOP-A1B2C3"},
		{"kl.home.arpa", "kl.home.arpa"},

		// Accents fold to the letter they decorate, so the owner still
		// recognises the result.
		{"Bürglers Keller", "Burglers Keller"},
		{"Café", "Cafe"},
		{"Ærø", "AEro"}, // no combining mark to strip; standIn carries these

		// Too long: cut to the limit, and never cut onto a trailing space.
		// Cut at the limit, not at a word boundary: predictable beats pretty,
		// and the name is still recognisably the one it came from.
		{"Mein KnightLoader auf dem grossen NAS", "Mein KnightLoader auf dem grosse"},
		{"Strømmen", "Strommen"},
		{"Weißbier", "Weissbier"},
		{"Łódź", "Lodz"},

		// The rule wants the first character alphanumeric.
		{"-leading-hyphen", "leading-hyphen"},
		{"  .spaced", "spaced"},

		// Nothing usable at all - answered honestly rather than invented.
		{"日本のサーバー", ""},
		{"", ""},
		{"---", ""},
		{"🙂", ""},
	}
	for _, c := range cases {
		got := SanitiseName(c.in)
		if got != c.want {
			t.Errorf("SanitiseName(%q) = %q, want %q", c.in, got, c.want)
		}
		if got != "" && !nameRe.MatchString(got) {
			t.Errorf("SanitiseName(%q) = %q, which nameRe still refuses", c.in, got)
		}
	}
}

// TestSanitiseNameIsIdempotent: it runs on the announce side and again on the
// receiving side, so a second pass must not keep changing the answer.
func TestSanitiseNameIsIdempotent(t *testing.T) {
	for _, in := range []string{"Bürglers Keller", "Mein KnightLoader auf dem grossen NAS", "-leading", "Bottich", "日本"} {
		once := SanitiseName(in)
		if twice := SanitiseName(once); twice != once {
			t.Errorf("SanitiseName(%q) = %q, then %q - not idempotent", in, once, twice)
		}
	}
}

package federation

import "testing"

// TestSanitiseNameProducesAnAddressableName checks that the result matches
// nameRe or is empty, and is empty only when nothing usable was given.
func TestSanitiseNameProducesAnAddressableName(t *testing.T) {
	cases := []struct{ in, want string }{
		// Valid names must pass through unchanged.
		{"Bottich", "Bottich"},
		{"Zathallia", "Zathallia"},
		{"DESKTOP-A1B2C3", "DESKTOP-A1B2C3"},
		{"kl.home.arpa", "kl.home.arpa"},

		// Accents fold to their base letter.
		{"Bürglers Keller", "Burglers Keller"},
		{"Café", "Cafe"},
		{"Ærø", "AEro"}, // no combining mark to strip; standIn carries these

		// Cut at the limit, not at a word boundary, and never onto a trailing
		// space.
		{"Mein KnightLoader auf dem grossen NAS", "Mein KnightLoader auf dem grosse"},
		{"Strømmen", "Strommen"},
		{"Weißbier", "Weissbier"},
		{"Łódź", "Lodz"},

		// The first character must be alphanumeric.
		{"-leading-hyphen", "leading-hyphen"},
		{"  .spaced", "spaced"},

		// Nothing usable.
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

// TestSanitiseNameIsIdempotent: it runs on both the announcing and the
// receiving side.
func TestSanitiseNameIsIdempotent(t *testing.T) {
	for _, in := range []string{"Bürglers Keller", "Mein KnightLoader auf dem grossen NAS", "-leading", "Bottich", "日本"} {
		once := SanitiseName(in)
		if twice := SanitiseName(once); twice != once {
			t.Errorf("SanitiseName(%q) = %q, then %q; not idempotent", in, once, twice)
		}
	}
}

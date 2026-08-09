package resolver

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// TestAnswersNeverShortensTheBatch pins the one thing every caller of a Checker
// depends on and none of them can verify: the answer is as long as the question.
// A service that drops an entry it did not recognise would otherwise slide every
// verdict after the gap onto the wrong link, and the row that reads "offline" is
// then a file that is perfectly fine.
func TestAnswersNeverShortensTheBatch(t *testing.T) {
	cases := []struct {
		name string
		got  []core.Availability
		want []core.Availability
	}{
		{
			name: "a service that answered nothing at all",
			got:  nil,
			want: []core.Availability{core.AvailUncheckable, core.AvailUncheckable, core.AvailUncheckable},
		},
		{
			name: "a service that skipped the last link",
			got:  []core.Availability{core.AvailOnline, core.AvailOffline},
			want: []core.Availability{core.AvailOnline, core.AvailOffline, core.AvailUncheckable},
		},
		{
			name: "a service that invented a fourth answer",
			got: []core.Availability{
				core.AvailOnline, core.AvailOnline, core.AvailOnline, core.AvailOffline,
			},
			want: []core.Availability{core.AvailOnline, core.AvailOnline, core.AvailOnline},
		},
		{
			// An empty string means "not checked", and a link that went out in a
			// check request has been checked whatever came back. Left as "" it
			// rejoins the links nobody has looked at and vanishes from the answer
			// the user just asked for.
			name: "a service that answered with the empty string",
			got:  []core.Availability{core.AvailOnline, "", core.AvailOffline},
			want: []core.Availability{core.AvailOnline, core.AvailUncheckable, core.AvailOffline},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := Answers(c.got, 3)
			if len(out) != 3 {
				t.Fatalf("Answers returned %d verdicts for 3 links", len(out))
			}
			for i := range c.want {
				if out[i] != c.want[i] {
					t.Errorf("verdict %d = %q, want %q", i, out[i], c.want[i])
				}
			}
		})
	}
}

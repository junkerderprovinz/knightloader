package wireshape

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// known is every non-pointer time.Time in this tree that still carries
// `omitempty`, listed on purpose.
//
// THIS LIST MAY ONLY EVER GET SHORTER. It is not an exemption list and none of
// these is correct; each one is a field whose tag says it can be absent and
// which is in fact always sent, as the year one. They are here rather than
// fixed because flipping a tag to `omitzero` CHANGES THE WIRE - the field stops
// being sent at all - and every one of these is read by the web app, the mobile
// app or both, so each is its own decision with its own reading of who breaks.
// internal/startupcheck.Report.FinishedAt was the one that could be flipped
// without a reader anywhere to break, and it was.
//
// The test below fails on anything NOT in this list, which is the whole point:
// the debt is fixed in size, and the eleventh one cannot be added quietly.
// It also fails on anything in the list that is no longer there, so the list
// cannot rot into a lie of its own.
var known = []string{
	// Task is the one on every row of the downloads table. All four of these are
	// read by web/src/components/columns.tsx, TimesCard.tsx, ListToolbar.tsx and
	// RetryCountdown.tsx, and by the mobile app, and every one of those readers
	// goes through happened()/goTimeMs(), which answer the same for an absent
	// field and for the year one. Flipping the tags is very probably invisible -
	// and "very probably" over four fields on the busiest payload in the program
	// is a change that wants its own measurement, not a tidy-up in a guard's
	// commit.
	"internal/core/task.go Task.NextTry",
	"internal/core/task.go Task.StalledSince",
	"internal/core/task.go Task.FinishedAt",
	"internal/core/task.go Task.ChangedAt",
	// The captcha challenge's deadline. web/src/components/CaptchaModal.tsx reads
	// it through expiryMs(), which handles absence; the extension has its own
	// reader that has not been looked at here.
	"internal/captcha/challenge.go Challenge.ExpiresAt",
	// An extraction's own clock, shown on the extraction card.
	"internal/app/app_extract.go ExtractJob.StartedAt",
	"internal/app/app_extract.go ExtractJob.EndedAt",
	// Account health. BenchedUntil in particular is a deadline somebody may well
	// come to test directly, which is exactly the shape of the two bugs that
	// started all of this.
	"internal/accounts/health.go Record.BenchedUntil",
	"internal/accounts/health.go Record.CheckedAt",
}

func TestNoNewZeroTimeOmitempty(t *testing.T) {
	found, err := ZeroTimeOmitempty(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("scanning the tree: %v", err)
	}

	inKnown := map[string]bool{}
	for _, k := range known {
		inKnown[k] = true
	}
	seen := map[string]bool{}
	for _, f := range found {
		seen[f.Key()] = true
		if inKnown[f.Key()] {
			continue
		}
		t.Errorf(
			"%s:%d %s.%s is a time.Time tagged `omitempty`, and omitempty does nothing to a struct.\n"+
				"\tIt will be sent as %q:\"0001-01-01T00:00:00Z\" when nobody has set it, which is a non-empty\n"+
				"\tstring and therefore true to anything that tests it. Tag it `omitzero` instead, which is\n"+
				"\twhat omitempty was meant to do here - and if that is not possible because something reads\n"+
				"\tthe field and would break, add it to `known` in this file with the reason.",
			f.File, f.Line, f.Type, f.Name, f.JSON,
		)
	}
	for _, k := range known {
		if !seen[k] {
			t.Errorf("`known` in this file lists %s, and the tree no longer has it - delete the line", k)
		}
	}
}

// TestScannerCatchesEverySpelling is the guard on the guard.
//
// A check that is green because it cannot see anything is worse than no check,
// so the shapes it has to catch and the shapes it has to leave alone are both
// written out and both asserted. The fixtures go into a temp directory rather
// than into testdata/, so there is no file in the repository whose whole job is
// to be wrong.
func TestScannerCatchesEverySpelling(t *testing.T) {
	dir := t.TempDir()

	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	write("caught.go", `package fixture

import "time"

type Caught struct {
	Plain      time.Time `+"`"+`json:"plain,omitempty"`+"`"+`
	AfterOther time.Time `+"`"+`json:"afterOther,string,omitempty"`+"`"+`
	BeforeOther time.Time `+"`"+`json:"beforeOther,omitempty,string"`+"`"+`
	Inline struct {
		Nested time.Time `+"`"+`json:"nested,omitempty"`+"`"+`
	} `+"`"+`json:"inline"`+"`"+`
}
`)

	write("quiet.go", `package fixture

import "time"

type Pointer struct {
	OK *time.Time `+"`"+`json:"ok,omitempty"`+"`"+`
}

type Tagged struct {
	Zero    time.Time `+"`"+`json:"zero,omitzero"`+"`"+`
	Bare    time.Time `+"`"+`json:"bare"`+"`"+`
	Skipped time.Time `+"`"+`json:"-,omitempty"`+"`"+`
	Untagged time.Time
}
`)

	// A Time that is not time.Time. The check is about encoding/json's treatment
	// of a struct it knows, and a namesake from somewhere else is not it.
	write("namesake.go", `package fixture

import fake "example.com/notTime"

type Namesake struct {
	Other fake.Time `+"`"+`json:"other,omitempty"`+"`"+`
}
`)

	// Skipped for being a test: a struct written to exercise a decoder is not a
	// shape this program promises anybody.
	write("fixture_test.go", `package fixture

import "time"

type InATest struct {
	Ignored time.Time `+"`"+`json:"ignored,omitempty"`+"`"+`
}
`)

	found, err := ZeroTimeOmitempty(dir)
	if err != nil {
		t.Fatalf("scanning the fixtures: %v", err)
	}
	var got []string
	for _, f := range found {
		got = append(got, f.Type+"."+f.Name)
	}
	sort.Strings(got)

	want := []string{"Caught.AfterOther", "Caught.BeforeOther", "Caught.Inline.Nested", "Caught.Plain"}
	if len(got) != len(want) {
		t.Fatalf("caught %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("caught %v, want %v", got, want)
		}
	}
}

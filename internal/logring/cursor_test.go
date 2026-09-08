package logring

import (
	"fmt"
	"testing"
)

// TestSinceReturnsOnlyWhatIsNewer is the whole reason the cursor exists. A
// follow view polls every two seconds; without this it could only re-fetch all
// five hundred lines and diff them, which cannot tell a repeated line from a
// new one.
func TestSinceReturnsOnlyWhatIsNewer(t *testing.T) {
	r := New(10)
	r.Write([]byte("one\ntwo\nthree\n"))

	all, dropped, newest := r.Since(0, 0)
	if len(all) != 3 || dropped != 0 || newest != 3 {
		t.Fatalf("Since(0) = %d entries, dropped %d, newest %d; want 3, 0, 3", len(all), dropped, newest)
	}
	if all[0].Seq != 1 || all[2].Line != "three" {
		t.Fatalf("Since(0) = %+v, want seq 1..3 oldest first", all)
	}

	rest, dropped, newest := r.Since(newest, 0)
	if len(rest) != 0 || dropped != 0 || newest != 3 {
		t.Errorf("a second poll with nothing new returned %d entries, dropped %d, newest %d", len(rest), dropped, newest)
	}

	r.Write([]byte("four\n"))
	rest, dropped, _ = r.Since(3, 0)
	if len(rest) != 1 || rest[0].Line != "four" || dropped != 0 {
		t.Errorf("Since(3) after one more line = %+v, dropped %d; want just \"four\"", rest, dropped)
	}
}

// TestSinceCountsWhatFellOut is the half that makes following honest. A busy
// instance can log more than the ring holds between two polls, and a view that
// silently joined the two halves would show a continuous log with a hole in it.
func TestSinceCountsWhatFellOut(t *testing.T) {
	r := New(5)
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(r, "line %d\n", i)
	}
	// The caller has seen up to seq 2. Now eight more arrive, so seqs 3..5 are
	// pushed out of a five-line ring before the caller ever asks again.
	for i := 6; i <= 13; i++ {
		fmt.Fprintf(r, "line %d\n", i)
	}
	got, dropped, newest := r.Since(2, 0)
	if newest != 13 {
		t.Fatalf("newest = %d, want 13", newest)
	}
	// The ring holds 9..13; the caller had seen 1 and 2, so 3..8 are gone: six.
	if dropped != 6 {
		t.Errorf("dropped = %d, want 6 - the lines between the cursor and the oldest survivor", dropped)
	}
	if len(got) != 5 || got[0].Line != "line 9" {
		t.Errorf("Since(2) = %+v, want the five survivors starting at line 9", got)
	}
}

// TestSinceTakesTheOldestWhenLimited. Taking the newest would throw away the
// lines in between with nothing to say so; taking the oldest lets the cursor
// advance and the next poll picks up the rest.
func TestSinceTakesTheOldestWhenLimited(t *testing.T) {
	r := New(10)
	for i := 1; i <= 6; i++ {
		fmt.Fprintf(r, "line %d\n", i)
	}
	got, _, _ := r.Since(0, 2)
	if len(got) != 2 || got[0].Line != "line 1" || got[1].Line != "line 2" {
		t.Fatalf("Since(0, 2) = %+v, want the two OLDEST lines", got)
	}
	next, _, _ := r.Since(got[len(got)-1].Seq, 2)
	if len(next) != 2 || next[0].Line != "line 3" {
		t.Errorf("the follow-up poll = %+v, want line 3 onwards", next)
	}
}

// TestSinceSurvivesACursorFromADeadProcess. Sequence numbers reset on restart,
// so a page left open across one holds a number larger than anything this
// process has. Answering "nothing new" would leave that page blank for ever.
func TestSinceSurvivesACursorFromADeadProcess(t *testing.T) {
	r := New(10)
	r.Write([]byte("after the restart\n"))
	got, dropped, newest := r.Since(9999, 0)
	if len(got) != 1 || got[0].Line != "after the restart" {
		t.Errorf("Since(9999) = %+v, want the whole buffer back", got)
	}
	if dropped != 0 {
		t.Errorf("dropped = %d, want 0 - this process never had those lines to lose", dropped)
	}
	if newest != 1 {
		t.Errorf("newest = %d, want 1", newest)
	}
}

// TestSinceNeverAnswersNil. The result is JSON-encoded straight into a
// response, and a null there is a .map() that throws on a page which had
// nothing to draw.
func TestSinceNeverAnswersNil(t *testing.T) {
	r := New(10)
	got, _, _ := r.Since(0, 0)
	if got == nil {
		t.Error("Since on an empty ring returned nil, which encodes as JSON null")
	}
	r.Write([]byte("one\n"))
	got, _, _ = r.Since(1, 0)
	if got == nil {
		t.Error("Since with nothing newer returned nil, which encodes as JSON null")
	}
}

// TestLinesIsUnchangedByTheCursor. The diagnostics bundle and its own tests
// read Lines(), and a bundle whose log section had become an array of objects
// would break every reader that already knows the old shape.
func TestLinesIsUnchangedByTheCursor(t *testing.T) {
	r := New(10)
	r.Write([]byte("alpha\nbeta\n"))
	got := r.Lines()
	if len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Errorf("Lines() = %v, want the plain strings oldest first", got)
	}
}

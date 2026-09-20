package logring

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"
)

func TestRingKeepsLinesInOrder(t *testing.T) {
	r := New(10)
	r.Write([]byte("first\n"))
	r.Write([]byte("second\n"))
	r.Write([]byte("third\n"))
	got := r.Lines()
	want := []string{"first", "second", "third"}
	if len(got) != len(want) {
		t.Fatalf("Lines() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Lines()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// Bounded memory, with the most recent tail surviving rather than an
// arbitrary subset.
func TestRingDropsOldestOverCapacity(t *testing.T) {
	r := New(3)
	for i := 0; i < 10; i++ {
		fmt.Fprintf(r, "line %d\n", i)
	}
	got := r.Lines()
	want := []string{"line 7", "line 8", "line 9"}
	if len(got) != len(want) {
		t.Fatalf("Lines() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Lines()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// log.Logger.Output is not the only possible caller: a single Write carrying
// an embedded '\n', from a multi-line Printf or several records coalesced
// upstream, has to land as separate entries, or a stack trace collapses into
// one unreadable ring slot.
func TestRingSplitsOneWriteIntoManyLines(t *testing.T) {
	r := New(10)
	r.Write([]byte("alpha\nbeta\ngamma\n"))
	got := r.Lines()
	want := []string{"alpha", "beta", "gamma"}
	if len(got) != len(want) {
		t.Fatalf("Lines() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Lines()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// A blank line is not a record worth a ring slot. log.Logger never sends one,
// but nothing stops another caller from trying.
func TestRingWriteOfNothingStoresNothing(t *testing.T) {
	r := New(10)
	r.Write([]byte("\n"))
	r.Write([]byte(""))
	if got := r.Lines(); len(got) != 0 {
		t.Errorf("Lines() = %v, want none", got)
	}
}

// io.Writer's contract is that n equals len(p) on a nil error, so a caller
// comparing the two, as several standard library writers do, must not see a
// short write for a line this dropped entirely.
func TestRingWriteReportsTheFullByteCount(t *testing.T) {
	r := New(10)
	p := []byte("\n")
	n, err := r.Write(p)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(p) {
		t.Errorf("Write returned n=%d, want %d", n, len(p))
	}
}

// The caller JSON-encodes this straight into an HTTP response, so a slice
// aliasing the ring's own backing array would race the next Write and let the
// caller corrupt state it does not own.
func TestRingLinesReturnsACopy(t *testing.T) {
	r := New(10)
	r.Write([]byte("original\n"))
	got := r.Lines()
	got[0] = "tampered"
	if again := r.Lines(); again[0] != "original" {
		t.Errorf("mutating the returned slice changed the ring: %q", again[0])
	}
}

// A smoke test for the lock rather than a substitute for -race, which needs a
// C toolchain this tree does not build with. It still shows that concurrent
// writers never push the ring past its capacity and never panic.
func TestRingConcurrentWrites(t *testing.T) {
	r := New(50)
	var wg sync.WaitGroup
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				fmt.Fprintf(r, "g%d-%d\n", g, i)
			}
		}(g)
	}
	wg.Wait()
	if got := len(r.Lines()); got != 50 {
		t.Errorf("Lines() has %d entries, want exactly the 50-line capacity", got)
	}
}

// An ordinary log.Printf call, like the ones the rest of the tree makes, has
// to reach Lines with no plumbing at the call site. The tap was installed by
// this package's init before this test ran, see the package doc comment.
func TestDefaultTapCapturesStandardLog(t *testing.T) {
	marker := "logring-marker-la9x2q"
	log.Print(marker)

	lines := Lines()
	found := false
	for _, l := range lines {
		if strings.Contains(l, marker) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("log.Print(%q) did not reach logring.Lines(): %v", marker, lines)
	}
}

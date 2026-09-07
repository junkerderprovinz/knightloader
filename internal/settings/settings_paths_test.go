package settings

import (
	"path/filepath"
	"testing"
)

// TestFixedPrefixDoesNotInventARoot pins the platform trap that made the same
// template legal on Linux and illegal on Windows.
//
// fixedPrefix used to answer with the bare separator whenever nothing fixed
// stood in front of the first placeholder. filepath.IsAbs("/") is true and
// filepath.IsAbs(`\`) is false, so "<jd:packagename>/unpacked" was accepted by
// the container build and refused by the desktop one. The test that found it
// was green on the machine it was written on and red in CI, which is the only
// way this class of bug ever shows up.
func TestFixedPrefixDoesNotInventARoot(t *testing.T) {
	sep := string(filepath.Separator)

	// Nothing fixed at all: the answer is the empty string, on every platform.
	//
	// Asserted on the VALUE and not through filepath.IsAbs, which is the whole
	// lesson of this bug: IsAbs is exactly the function whose answer differs
	// between the two, so a test written through it can only ever fail on one of
	// them. Written that way first, this test passed on Windows with the fix
	// removed.
	if got := fixedPrefix("<jd:packagename>" + sep + "unpacked"); got != "" {
		t.Errorf("fixedPrefix = %q for a template with no fixed part, want the empty string; returning a bare separator here is absolute on Linux and relative on Windows", got)
	}
	// A real root followed only by placeholders keeps its root, which is the
	// case the fallback was written for and still has to serve.
	if got := fixedPrefix(sep + "<jd:packagename>"); got != sep {
		t.Errorf("fixedPrefix = %q for a rooted template, want the root %q", got, sep)
	}
	// A fixed head is returned unchanged.
	head := filepath.Join(sep+"serien", "neu")
	if got := fixedPrefix(head + sep + "<jd:packagename>"); got != head {
		t.Errorf("fixedPrefix = %q, want the fixed head %q", got, head)
	}
}

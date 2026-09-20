package settings

import (
	"path/filepath"
	"testing"
)

// The platform trap that makes the same template legal on Linux and illegal on
// Windows. Answering with the bare separator whenever nothing fixed stands in
// front of the first placeholder means "<jd:packagename>/unpacked" is accepted
// by the container build and refused by the desktop one, because
// filepath.IsAbs("/") is true and filepath.IsAbs(`\`) is false.
func TestFixedPrefixDoesNotInventARoot(t *testing.T) {
	sep := string(filepath.Separator)

	// Nothing fixed at all: the empty string, on every platform. Asserted on
	// the value rather than through filepath.IsAbs, which is the function whose
	// answer differs between the two, so a test written through it can only
	// fail on one of them.
	if got := fixedPrefix("<jd:packagename>" + sep + "unpacked"); got != "" {
		t.Errorf("fixedPrefix = %q for a template with no fixed part, want the empty string; returning a bare separator here is absolute on Linux and relative on Windows", got)
	}
	// A real root followed only by placeholders keeps its root.
	if got := fixedPrefix(sep + "<jd:packagename>"); got != sep {
		t.Errorf("fixedPrefix = %q for a rooted template, want the root %q", got, sep)
	}
	// A fixed head is returned unchanged.
	head := filepath.Join(sep+"serien", "neu")
	if got := fixedPrefix(head + sep + "<jd:packagename>"); got != head {
		t.Errorf("fixedPrefix = %q, want the fixed head %q", got, head)
	}
}

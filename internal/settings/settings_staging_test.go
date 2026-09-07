package settings

import (
	"path/filepath"
	"testing"
)

// TestAFreshInstallWritesStraightToItsDestination is the default this pair of
// fields lives or dies by. Anything other than empty here means somebody who
// installed an update starts copying every download across a filesystem
// boundary, gigabytes at a time, for a problem they may not have.
func TestAFreshInstallWritesStraightToItsDestination(t *testing.T) {
	d := Defaults()
	if d.WorkDir != "" {
		t.Errorf("WorkDir = %q, want empty so downloads are written where they always were", d.WorkDir)
	}
	if d.ExtractMoveTo != "" {
		t.Errorf("ExtractMoveTo = %q, want empty so an extraction stays where it unpacked", d.ExtractMoveTo)
	}
}

// TestARelativeStagingPathIsDropped. A relative path resolves against whatever
// the process's working directory happens to be, and for a working folder that
// would put a download's bytes somewhere nobody can name - which for the folder
// this build then MOVES OUT OF is worse than for one it only writes into.
func TestARelativeStagingPathIsDropped(t *testing.T) {
	got := sanitizeStaging(Settings{WorkDir: " incomplete ", ExtractMoveTo: "unpacked"})
	if got.WorkDir != "" {
		t.Errorf("WorkDir = %q, want it dropped", got.WorkDir)
	}
	if got.ExtractMoveTo != "" {
		t.Errorf("ExtractMoveTo = %q, want it dropped", got.ExtractMoveTo)
	}
}

// TestAWorkingFolderIsNotATemplate is the one place these two fields
// deliberately disagree. ExtractMoveTo may hold placeholders and is checked
// through its fixed prefix like every other folder template; a working folder
// may not, because its whole job is to be ONE folder shared by the downloads
// heading for one destination, and a per-package or per-date template would
// scatter the parts of a multi-volume archive across several of them.
func TestAWorkingFolderIsNotATemplate(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "incomplete")
	got := sanitizeStaging(Settings{
		WorkDir:       abs,
		ExtractMoveTo: filepath.Join(t.TempDir(), "serien") + string(filepath.Separator) + "<jd:packagename>",
	})
	if got.WorkDir != abs {
		t.Errorf("WorkDir = %q, want %q", got.WorkDir, abs)
	}
	if got.ExtractMoveTo == "" {
		t.Error("an absolute template with a placeholder in its tail was refused")
	}

	tpl := "<jd:packagename>" + string(filepath.Separator) + "unpacked"
	if got := sanitizeStaging(Settings{WorkDir: tpl, ExtractMoveTo: tpl}); got.WorkDir != "" || got.ExtractMoveTo != "" {
		t.Errorf("a template with nothing absolute in front of it survived: %q / %q", got.WorkDir, got.ExtractMoveTo)
	}
}

// TestStagingGoesThroughTheOneSanitizePath. A hook that is written but never
// listed in sanitize is a setting that validates in its own test and not in the
// app, which is the failure this whole file layout exists to prevent.
func TestStagingGoesThroughTheOneSanitizePath(t *testing.T) {
	got := sanitize(Settings{WorkDir: "incomplete", ExtractMoveTo: " "})
	if got.WorkDir != "" || got.ExtractMoveTo != "" {
		t.Fatalf("sanitize left %q / %q; sanitizeStaging is not in the list", got.WorkDir, got.ExtractMoveTo)
	}
}

package settings

// Where a download is written while it is still arriving, and where the
// unpacked content ends up afterwards. Both fields are empty on a fresh install
// and on every install that upgrades into them, and empty means exactly what
// this build did before they existed - see their own doc comments on Settings.

import (
	"path/filepath"
	"strings"
)

func sanitizeStaging(n Settings) Settings {
	n.WorkDir = strings.TrimSpace(n.WorkDir)
	// A relative working folder has the same problem as a relative download
	// folder: it resolves against whatever the process's working directory
	// happens to be, which is not something a user can reason about. Dropping
	// it falls back to writing straight to the destination, which is what every
	// install did before this field existed.
	//
	// It is checked as written and NOT through fixedPrefix, unlike ExtractMoveTo
	// below: a working folder holds no placeholders on purpose. Its whole job is
	// to be one folder that several downloads heading for one destination share,
	// and a template would split that folder per package or per date and leave
	// every multi-volume archive with its parts in four different places.
	if n.WorkDir != "" && !filepath.IsAbs(n.WorkDir) {
		n.WorkDir = ""
	}
	n.ExtractMoveTo = strings.TrimSpace(n.ExtractMoveTo)
	// Checked against the fixed prefix, matching ExtractTo in
	// settings_archives.go: "/serien/<jd:packagename>" is absolute and the
	// angle brackets in its tail must not make it read as relative.
	if n.ExtractMoveTo != "" && !filepath.IsAbs(fixedPrefix(n.ExtractMoveTo)) {
		n.ExtractMoveTo = ""
	}
	return n
}

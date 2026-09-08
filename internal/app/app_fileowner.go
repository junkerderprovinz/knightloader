package app

// Which folders this instance writes into, in ONE place.
//
// The list already existed twice by the time this file was written, in two
// shapes that agree by accident rather than by construction:
// sampleDiskReport builds it to measure free space, and settings.Validate is
// called folder by folder from five different route handlers to check each one
// as it is saved. Neither is reusable as the answer to "every folder whose
// ownership matters", the first because it is welded into a byte-count readout
// and the second because it is a per-field validator that creates what it
// checks. A third private copy inside the ownership check would have been the
// fourth list of the same folders in this repository, so it is a method
// instead, and the test beside it pins it against the disk report's list so the
// two cannot drift apart quietly.
//
// THE WATCH FOLDER IS IN THIS LIST AND DELIBERATELY NOT IN THE DISK REPORT'S.
// That is not an inconsistency to be tidied up: the disk report is about how
// many bytes will be written where, and nothing is ever downloaded into the
// watch folder, so a free-space row for it would be noise. Ownership is a
// different question with a different answer - KL DELETES the .crawljob it has
// consumed (internal/watch/watcher.go), and a delete needs write permission on
// the FOLDER rather than on the file, which is exactly the permission a share
// mounted for a different account withholds. A drop folder that silently
// re-imports the same job every poll because the delete failed is one of the
// nastier shapes this bug takes, and it is invisible from anywhere else.
//
// NOTHING HERE STATS ANYTHING. The list is built from the configuration and
// handed back; measuring is internal/fileowner's job and happens behind a POST
// because it writes. Keeping the two apart is what lets the diagnostics bundle
// use the same list with a read-only stat.

import (
	"path/filepath"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// roleWatch is the drop folder's role id. The other three (roleDownloads,
// roleWork, roleCategory) are shared with the disk report and live in
// app_diskreport.go beside the comment that explains why a role travels as an
// id and never as English prose; this one is here because the disk report has
// no row for it and adding an unused constant to that file would suggest it
// does.
const roleWatch = "watch"

// TargetFolder is one folder this instance writes into, and why.
type TargetFolder struct {
	// Dir is a real path, cut back from any placeholder template with
	// settings.FixedPrefix and cleaned. It may be a folder that does not exist
	// yet.
	Dir string `json:"dir"`
	// Role is why it is in the list - one of the role ids above. Whatever draws
	// it looks the label up in its own language.
	Role string `json:"role"`
}

// TargetFolders is every folder this instance writes into, deduplicated, with
// the first role that named a folder winning.
//
// FIRST ROLE WINS, and the order below is therefore the order of the answer:
// the download folder, the working folder, the categories, the drop folder. An
// install whose category folder IS the download folder gets one row saying
// "download folder" rather than two rows about one directory, which is the same
// call sampleDiskReport makes and for the same reason - one folder, one row.
//
// A relative folder is dropped rather than resolved. It would resolve against
// whatever the process's working directory happens to be, which is not
// something an operator can reason about and is why sanitizePaths refuses to
// store one in the first place. A template head that comes back empty
// ("<jd:packagename>/unpacked" has no fixed part at all) is dropped by the same
// test.
func (a *App) TargetFolders() []TargetFolder {
	cfg := a.Settings.Get()
	var out []TargetFolder
	seen := map[string]bool{}
	add := func(dir, role string) {
		// Cut back to the part of a template that is a real path BEFORE anything
		// is done with it. "/downloads/<jd:date>/<jd:packagename>" never exists,
		// so probing it as written would report a missing folder for a perfectly
		// healthy download folder. settings.FixedPrefix is the app's own rule for
		// where a template stops being a path, exported precisely so that a third
		// copy of it would not be written (see its doc comment).
		dir = filepath.Clean(settings.FixedPrefix(strings.TrimSpace(dir)))
		if dir == "." || !filepath.IsAbs(dir) || seen[dir] {
			return
		}
		seen[dir] = true
		out = append(out, TargetFolder{Dir: dir, Role: role})
	}
	// defaultDir rather than cfg.DownloadDir: an install that has never set one
	// downloads into the built-in folder under the data directory, and that
	// folder is as capable of belonging to the wrong account as any other. Read
	// through the same accessor dirFor and the disk report use, so all three
	// agree about where a download with no override actually lands.
	add(a.defaultDir(), roleDownloads)
	add(cfg.WorkDir, roleWork)
	for _, c := range cfg.Categories {
		add(c.Dir, roleCategory)
	}
	add(cfg.WatchDir, roleWatch)
	// Never nil: a nil slice encodes as JSON null and the page that walks over it
	// throws rather than drawing nothing. The neighbouring DiskReport initialises
	// its own list for exactly this.
	if out == nil {
		out = []TargetFolder{}
	}
	return out
}

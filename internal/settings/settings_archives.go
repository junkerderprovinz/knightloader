package settings

// What happens to an archive: where it unpacks, what replaces what, and what
// becomes of the archive afterwards. The vocabulary is not defined here - every
// value is folded onto one internal/extract accepts, so a word this file
// approved and that package refuses cannot exist.

import (
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/extract"
)

// maxTrashDays caps the retention. A year is already far past the point where
// "I might still need it" is doing any work, and an uncapped number lets a
// typo park a disk's worth of archives in a hidden folder indefinitely.
const maxTrashDays = 365

func sanitizeArchives(n Settings) Settings {
	n.ExtractTo = strings.TrimSpace(n.ExtractTo)
	// A relative extraction folder has the same problem as a relative download
	// folder: it resolves against whatever the process's working directory
	// happens to be. Dropping it falls back to beside the archive, which is
	// always somewhere real.
	//
	// The check is against the fixed prefix, because this folder may be a
	// template: "/unpacked/<jd:packagename>" is absolute and "<jd:date>" alone
	// is not, and testing the raw string would refuse the first one for the
	// angle brackets in its tail.
	if relative(n.ExtractTo, true) {
		n.ExtractTo = ""
	}
	// Both go through the parser the extractor uses, so an unknown word becomes
	// the same thing here as it would there. Storing the folded value rather
	// than the raw one means the settings file says what the app will do.
	n.ArchiveDisposal = string(extract.ParseDisposal(n.ArchiveDisposal))
	n.ExtractCollision = string(extract.ParseCollision(n.ExtractCollision))
	if n.TrashRetentionDays < 0 {
		n.TrashRetentionDays = 0
	}
	if n.TrashRetentionDays > maxTrashDays {
		n.TrashRetentionDays = maxTrashDays
	}
	return n
}

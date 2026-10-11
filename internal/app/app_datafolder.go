package app

import "github.com/junkerderprovinz/knightloader/internal/settings"

// dataFolderOf refuses the first of dirs that lies in the data folder or holds
// it, and returns nil when none does. Nothing a download, an extraction, a
// rename or a move writes may land there, beside the password hash, the API
// tokens, the settings and the tools KnightLoader runs (see
// settings.OverlapsData).
func (a *App) dataFolderOf(dirs ...string) *dataFolderError {
	for _, d := range dirs {
		switch {
		case d == "":
		case settings.InsideData(a.DataDir, d):
			return &dataFolderError{dir: d}
		case settings.HoldsData(a.DataDir, d):
			return &dataFolderError{dir: d, holds: true}
		}
	}
	return nil
}

// barFolder is dataFolderOf for one folder, in the shape a check that takes an
// error wants.
func (a *App) barFolder(dir string) error {
	if refusal := a.dataFolderOf(dir); refusal != nil {
		return refusal
	}
	return nil
}

// barFile refuses a file or folder about to be written at path when path
// lands in the data folder, through whatever folders and links lead there.
func (a *App) barFile(path string) error {
	if settings.InsideData(a.DataDir, path) {
		return &dataFolderError{dir: path}
	}
	return nil
}

// dataFolderError is a write refused because dir lies in the data folder, or
// with holds set, because dir holds it. It reads as core.CodeDataFolder or
// core.CodeHoldsDataFolder (see diskCode).
type dataFolderError struct {
	dir   string
	holds bool
}

func (e *dataFolderError) Error() string {
	if e.holds {
		return e.dir + " holds KnightLoader's data folder; choose a folder that does not"
	}
	return e.dir + " is inside KnightLoader's data folder; choose a folder outside it"
}

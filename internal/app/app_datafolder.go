package app

import "github.com/junkerderprovinz/knightloader/internal/settings"

// dataFolderOf returns the first of dirs that lies in the data folder, or ""
// when none does. Nothing a download, an extraction, a rename or a move writes
// may land there, beside the password hash, the API tokens, the settings and
// the tools KnightLoader runs (see settings.InsideData).
func (a *App) dataFolderOf(dirs ...string) string {
	for _, d := range dirs {
		if d != "" && settings.InsideData(a.DataDir, d) {
			return d
		}
	}
	return ""
}

// dataFolderError is a write refused because dir lies in the data folder. It
// reads as core.CodeDataFolder (see diskCode).
type dataFolderError struct {
	dir string
}

func (e *dataFolderError) Error() string {
	return e.dir + " is inside KnightLoader's data folder; choose a folder outside it"
}

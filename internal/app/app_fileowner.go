package app

import (
	"path/filepath"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// roleWatch is the drop folder's role id. The other roles live in
// app_diskreport.go; this one is here because the disk report has no row for
// the drop folder.
const roleWatch = "watch"

// TargetFolder is one folder this instance writes into, and why.
type TargetFolder struct {
	// Dir is a cleaned absolute path, cut back from any placeholder template
	// with settings.FixedPrefix. It may not exist yet.
	Dir string `json:"dir"`
	// Role is one of the role ids; the client looks up the label in its own
	// language.
	Role string `json:"role"`
}

// TargetFolders returns every folder this instance writes into, deduplicated,
// with the first role that named a folder winning. Unlike the disk report it
// includes the drop folder, because KL deletes the .crawljob files it consumes
// there and that needs write permission on the folder. Relative folders are
// left out, since they would resolve against the process's working directory.
func (a *App) TargetFolders() []TargetFolder {
	cfg := a.Settings.Get()
	var out []TargetFolder
	seen := map[string]bool{}
	add := func(dir, role string) {
		// "/downloads/<jd:date>/<jd:packagename>" never exists as written, so
		// only the fixed head of a template is a folder worth checking.
		dir = filepath.Clean(settings.FixedPrefix(strings.TrimSpace(dir)))
		if dir == "." || !filepath.IsAbs(dir) || seen[dir] {
			return
		}
		seen[dir] = true
		out = append(out, TargetFolder{Dir: dir, Role: role})
	}
	// defaultDir rather than cfg.DownloadDir, so an install that never set one
	// still gets its built-in download folder checked.
	add(a.defaultDir(), roleDownloads)
	add(cfg.WorkDir, roleWork)
	for _, c := range cfg.Categories {
		add(c.Dir, roleCategory)
	}
	add(cfg.WatchDir, roleWatch)
	// A nil slice would encode as JSON null.
	if out == nil {
		out = []TargetFolder{}
	}
	return out
}

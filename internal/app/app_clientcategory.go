package app

import (
	"log"
	"path/filepath"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// CategoryNamed returns the id of the category a download client such as
// Sonarr named, and files one under that name when none exists yet, with its
// own folder inside the download folder. An empty name and SABnzbd's
// catch-all "*" mean no category, and so does a category that cannot be
// filed, which is logged: the grab still goes in, into the download folder.
func (a *App) CategoryNamed(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || name == "*" {
		return ""
	}
	// A templated download folder contributes its fixed part only; the
	// placeholders describe one task, not a drawer.
	dir := filepath.Join(settings.FixedPrefix(a.defaultDir()), sanitizeSegment(name))
	id, created, err := a.Settings.EnsureCategory(name, dir)
	if err != nil {
		log.Printf("the category %q could not be created, so the download goes in without one: %v", name, err)
		return ""
	}
	if created {
		log.Printf("category %q created for a download client's grabs, with the folder %s", name, dir)
		a.afterSettingsChange(a.Settings.Get())
	}
	return id
}

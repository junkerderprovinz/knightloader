package app

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/settings"
)

// CategoryNamed returns the id of the category a download client such as
// Sonarr named, filing one as ClientCategory does when none exists yet. An
// empty name and SABnzbd's catch-all "*" mean no category, and so does a
// category that cannot be filed, which is logged: the grab still goes in, into
// the download folder.
func (a *App) CategoryNamed(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || name == "*" {
		return ""
	}
	id, err := a.ClientCategory(name, "")
	if err != nil {
		log.Printf("the category %q could not be created, so the download goes in without one: %v", name, err)
		return ""
	}
	return id
}

// ClientCategory returns the id of the category a download client names (see
// settings.Settings.CategoryByName), and files one under that name when there
// is none. Its folder is dir, taken inside the download folder when relative,
// or a folder of its own there when dir is empty. Both download client doors
// create their categories here, so a category Sonarr names is the same drawer
// whichever door it came through. A category that exists keeps its folder.
func (a *App) ClientCategory(name, dir string) (string, error) {
	name = strings.TrimSpace(name)
	// A templated download folder contributes its fixed part only; the
	// placeholders describe one task, not a drawer.
	base := settings.FixedPrefix(a.defaultDir())
	switch dir = strings.TrimSpace(dir); {
	case dir == "":
		dir = filepath.Join(base, sanitizeSegment(name))
	case !filepath.IsAbs(dir):
		dir = filepath.Join(base, dir)
	}
	id, created, err := a.Settings.EnsureCategory(name, dir)
	if err != nil {
		return "", err
	}
	if created {
		log.Printf("category %q created for a download client's grabs, with the folder %s", name, dir)
		a.afterSettingsChange(a.Settings.Get())
	}
	return id, nil
}

// GrabFolder creates and returns the folder one grab from a download client
// downloads into: the release's name inside dir, or inside the folder of the
// category with the id category when dir is empty, with ".1", ".2" and so on
// after a name that is taken, as in SABnzbd. Sonarr deletes the folder a door
// reports once it has imported from it, so only a folder this call made can
// be one.
func (a *App) GrabFolder(category, dir, name string) (string, error) {
	base := strings.TrimSpace(dir)
	if base == "" {
		base = settings.FixedPrefix(a.Settings.Get().CategoryFor(category).Dir)
	}
	if base == "" {
		base = settings.FixedPrefix(a.defaultDir())
	}
	if err := settings.Validate("the folder for this batch", base); err != nil {
		return "", err
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	name = sanitizeSegment(name)
	for n := 0; ; n++ {
		folder := filepath.Join(base, name)
		if n > 0 {
			folder += "." + strconv.Itoa(n)
		}
		switch err := os.Mkdir(folder, 0o755); {
		case err == nil:
			return folder, nil
		case !errors.Is(err, fs.ErrExist):
			return "", err
		}
	}
}

package main

// Revealing a task's file in the file manager and opening it with its default
// application exist only on the desktop, so window.go.main.DesktopFiles is
// undefined in the browser build. Paths go through app.App.SafeTaskFile, the
// same check internal/api/routes_files.go uses, so this is no weaker way onto
// the file.

import (
	"github.com/junkerderprovinz/knightloader/internal/app"
)

// DesktopFiles is bound to the frontend as window.go.main.DesktopFiles.
type DesktopFiles struct {
	app *app.App
}

func newDesktopFiles(a *app.App) *DesktopFiles {
	return &DesktopFiles{app: a}
}

// RevealInFolder opens the file manager on the folder holding taskID's file
// and selects the file where the platform allows it (not on Linux).
func (d *DesktopFiles) RevealInFolder(taskID string) error {
	f, err := d.app.SafeTaskFile(taskID)
	if err != nil {
		return err
	}
	return revealInFolder(f.Path)
}

// OpenFolder opens the file manager on the folder taskID downloads into,
// with its file selected once there is one, as JDownloader's "open folder"
// does for a link or a package.
func (d *DesktopFiles) OpenFolder(taskID string) error {
	if f, err := d.app.SafeTaskFile(taskID); err == nil {
		return revealInFolder(f.Path)
	}
	dir, err := d.app.NearestTaskFolder(taskID)
	if err != nil {
		return err
	}
	return openNatively(dir)
}

// OpenNatively opens taskID's file with the application the OS associates with
// it.
func (d *DesktopFiles) OpenNatively(taskID string) error {
	f, err := d.app.SafeTaskFile(taskID)
	if err != nil {
		return err
	}
	return openNatively(f.Path)
}

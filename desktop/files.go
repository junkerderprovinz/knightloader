package main

// The two OS-native actions package 20 promises: revealing a task's file in
// the platform's own file manager, and handing it to whatever application the
// OS opens that kind of file with. A browser has no such capability at all,
// and neither does the container build - DesktopFiles is bound to the
// frontend only here, in the desktop module (see main.go's Bind option), so
// window.go.main.DesktopFiles is simply undefined on the other two builds
// rather than a button that silently does nothing when pressed.
//
// Both methods call back into the shared server's own path resolution
// (app.App.SafeTaskFile) rather than trusting the id for anything more than a
// lookup key - the same rule internal/api/routes_files.go follows for the
// browser-reachable half of this feature, and for the same reason: a task
// whose stored Dir or name does not check out must refuse here exactly as it
// refuses there, or the desktop build would be a second, weaker door onto the
// same file.
//
// revealInFolder and openNatively are implemented once per OS in
// files_windows.go / files_darwin.go / files_linux.go - the three platforms
// desktop.yml actually builds.

import (
	"github.com/junkerderprovinz/knightloader/internal/app"
)

// DesktopFiles is the Wails-bound type; its exported methods are what
// window.go.main.DesktopFiles.* becomes on the frontend.
type DesktopFiles struct {
	app *app.App
}

func newDesktopFiles(a *app.App) *DesktopFiles {
	return &DesktopFiles{app: a}
}

// RevealInFolder opens the OS file manager on the folder holding taskID's
// file, selecting the file itself where the platform supports doing that
// (Windows, macOS) and opening just the containing folder where it does not
// (Linux - see files_linux.go).
func (d *DesktopFiles) RevealInFolder(taskID string) error {
	f, err := d.app.SafeTaskFile(taskID)
	if err != nil {
		return err
	}
	return revealInFolder(f.Path)
}

// OpenNatively hands taskID's file to whatever application the OS opens that
// kind of file with - the desktop build's answer to "Open", where the
// browser build's only option is to stream the bytes into a tab.
func (d *DesktopFiles) OpenNatively(taskID string) error {
	f, err := d.app.SafeTaskFile(taskID)
	if err != nil {
		return err
	}
	return openNatively(f.Path)
}

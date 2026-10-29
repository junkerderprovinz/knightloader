// Command installer installs KnightLoader on Windows and Linux. The desktop
// build (scripts/desktop.mjs) copies the program and yt-dlp into payload/ and
// builds this around it; on macOS the app ships as a disk image instead.
package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"

	"github.com/junkerderprovinz/pommelporter"
)

var (
	//go:embed payload
	payload embed.FS
	//go:embed logo.svg
	logo []byte
	//go:embed icon.png
	icon []byte
)

// Set by the build: an updatetest build installs as "KnightLoader Test", next
// to a real installation.
var (
	name    = "KnightLoader"
	id      = "com.junkerderprovinz.knightloader"
	version = "0.0.0"
)

// The installers before this one ran company and product together into one
// name, for all users or for one.
const legacy = "KnightLoaderKnightLoader"

func main() {
	files, err := fs.Sub(payload, "payload")
	if err == nil {
		err = pommelporter.Run(pommelporter.App{
			Name:       name,
			ID:         id,
			Executable: executable,
			Version:    version,
			Publisher:  "junkerderprovinz",
			Homepage:   "https://github.com/junkerderprovinz/knightloader",
			Accent:     "#FCC419",
			Logo:       logo,
			Icon:       icon,
			Categories: []string{"Network", "FileTransfer"},
			Payload:    executables{files, []string{"yt-dlp"}},
			Legacy: pommelporter.Legacy{
				MachineKeys: []string{`Software\Microsoft\Windows\CurrentVersion\Uninstall\` + legacy},
				UserKeys:    []string{`Software\Microsoft\Windows\CurrentVersion\Uninstall\` + legacy},
				ChoiceKeys:  []string{`Software\` + legacy},
			},
		})
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

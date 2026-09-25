package settings

// The event programs (Settings.EventPrograms): what this instance starts when
// something happens. The sibling of settings_notify.go, which sends a message
// on the same events instead of starting a program.

import "github.com/junkerderprovinz/knightloader/internal/eventprog"

// sanitizeEventPrograms hands the list to the package that runs it, so what a
// row may contain is written down once, next to the code that starts it.
func sanitizeEventPrograms(n Settings) Settings {
	n.EventPrograms = eventprog.Sanitize(n.EventPrograms)
	return n
}

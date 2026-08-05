// Separate module: the Wails desktop wrapper. Kept out of the server module so
// the Wails toolchain never affects the container/server build. Built in CI.
module github.com/junkerderprovinz/knightloader/desktop

go 1.24

require (
	github.com/junkerderprovinz/knightloader v0.0.0
	github.com/wailsapp/wails/v2 v2.10.2
)

// Use the server sources from the parent checkout.
replace github.com/junkerderprovinz/knightloader => ../

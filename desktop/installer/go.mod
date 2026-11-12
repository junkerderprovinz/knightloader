module github.com/junkerderprovinz/knightloader/desktop/installer

// Never older than the root module's go directive, which the replace below
// pulls in, as in ../go.mod.
go 1.27.3

toolchain go1.27.4

require (
	github.com/junkerderprovinz/knightloader v0.0.0
	github.com/junkerderprovinz/pommelporter v1.1.6-0.20261112102726-80cc535a4e21
)

require (
	github.com/GopeedLab/gopeed v1.9.3 // indirect
	github.com/andybalholm/brotli v1.2.8 // indirect
	github.com/bodgit/plumbing v1.3.0 // indirect
	github.com/bodgit/sevenzip v1.6.5 // indirect
	github.com/bodgit/windows v1.0.1 // indirect
	github.com/coder/websocket v1.8.15 // indirect
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/klauspost/compress v1.20.4 // indirect
	github.com/nwaples/rardecode/v2 v2.4.6 // indirect
	github.com/pierrec/lz4/v4 v4.1.27 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/stangelandcl/ppmd v0.1.1 // indirect
	github.com/ulikunitz/xz v0.5.17 // indirect
	github.com/wailsapp/wails/v3 v3.1.0 // indirect
	go4.org v0.0.0-20260112195520-a5071408f32f // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/term v0.46.0 // indirect
	golang.org/x/text v0.44.0 // indirect
)

// The installer fetches ffmpeg, Java and JDownloader with the app's own code
// from the parent checkout.
replace github.com/junkerderprovinz/knightloader => ../../

// The same forks as the server module, since a dependency's replace does not
// reach this one.
replace github.com/GopeedLab/gopeed => github.com/junkerderprovinz/gopeed v0.0.0-20261108080126-7ccce87b55a4

replace github.com/anacrolix/torrent => github.com/junkerderprovinz/torrent v1.61.1-0.20261014215729-7f7e9bdefc17

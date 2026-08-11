//go:build !race

package engine

// raceEnabled is true only in a binary built with -race. See
// torrent_live_test.go's own use of it for what it exists to skip and why.
const raceEnabled = false

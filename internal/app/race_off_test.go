//go:build !race

package app

// raceEnabled reports a -race build. Tests that run a real Gopeed transfer
// skip under it, because Gopeed v1.9.3 races inside itself.
const raceEnabled = false

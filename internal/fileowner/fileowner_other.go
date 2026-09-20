//go:build !unix && !windows

package fileowner

// Platforms without file owners (js/wasm, wasip1, plan9) report Known false,
// so Check answers VerdictUnknown rather than claiming root owns everything.

import "io/fs"

const supported = false

func who() Identity { return Identity{} }

func statOwner(fs.FileInfo) (int, int, bool) { return 0, 0, false }

func names(int, int) (string, string) { return "", "" }

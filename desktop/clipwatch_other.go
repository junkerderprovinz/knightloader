//go:build !windows && !darwin && !linux

package main

func openClipboard(read func() (string, bool)) (clipboardReader, bool) {
	return &polledClipboard{read: read}, true
}

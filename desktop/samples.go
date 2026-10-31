//go:build !updatetest

package main

import "github.com/junkerderprovinz/knightloader/internal/appkey"

// seedSamples does nothing in a release build; the test build fills a fresh
// list with sample links (samples_updatetest.go).
func seedSamples(string, *appkey.Key) {}

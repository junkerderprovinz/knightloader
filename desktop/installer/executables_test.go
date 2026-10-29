package main

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestTheNamedPayloadFilesReportExecuteBits(t *testing.T) {
	payload := executables{fstest.MapFS{
		"KnightLoader": {Data: []byte("program"), Mode: 0o444},
		"yt-dlp":       {Data: []byte("yt-dlp"), Mode: 0o444},
	}, []string{"yt-dlp"}}

	modes := map[string]fs.FileMode{}
	err := fs.WalkDir(payload, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		modes[name] = info.Mode().Perm()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if modes["yt-dlp"]&0o111 == 0 {
		t.Errorf("yt-dlp reports %v", modes["yt-dlp"])
	}
	if modes["KnightLoader"]&0o111 != 0 {
		t.Errorf("KnightLoader reports %v though it was not named", modes["KnightLoader"])
	}
}

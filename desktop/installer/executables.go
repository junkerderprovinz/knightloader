package main

import (
	"io/fs"
	"path"
)

// executables marks the payload files it names as executable. An embedded file
// system carries no file modes, and the Linux installer gives the execute bits
// only to the program and to what the payload reports as executable.
type executables struct {
	fs.FS
	names []string
}

func (e executables) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(e.FS, name)
	for i, entry := range entries {
		for _, n := range e.names {
			if path.Join(name, entry.Name()) == n {
				entries[i] = executableEntry{entry}
			}
		}
	}
	return entries, err
}

type executableEntry struct{ fs.DirEntry }

func (e executableEntry) Info() (fs.FileInfo, error) {
	info, err := e.DirEntry.Info()
	if err != nil {
		return nil, err
	}
	return executableInfo{info}, nil
}

type executableInfo struct{ fs.FileInfo }

func (i executableInfo) Mode() fs.FileMode { return i.FileInfo.Mode() | 0o111 }

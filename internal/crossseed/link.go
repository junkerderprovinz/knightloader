package crossseed

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// LinkMode is how a cross-seed reaches the files of the torrent it matched.
type LinkMode string

const (
	// LinkNone seeds from the files where they are.
	LinkNone LinkMode = "none"
	LinkHard LinkMode = "hardlink"
	LinkSym  LinkMode = "symlink"
	// LinkReflink copies by sharing the blocks, on Btrfs, XFS, APFS and ZFS.
	LinkReflink LinkMode = "reflink"
)

// ErrOtherFilesystem is a hardlink or a reflink between two filesystems,
// which neither can be.
var ErrOtherFilesystem = errors.New("the two folders are on different filesystems, where hardlinks and reflinks cannot reach")

// sameFilesystem reports whether two existing paths are on one filesystem.
var sameFilesystem = onSameFilesystem

// Link makes files, found under src, appear under dst the way mode says. A
// file already at its place with its size is left, anything else in the way
// stops the link.
func Link(mode LinkMode, src, dst string, files []FileEntry) error {
	if mode == LinkNone {
		return nil
	}
	for _, f := range files {
		if !filepath.IsLocal(filepath.FromSlash(f.Path)) {
			return fmt.Errorf("the path %q points outside its folder", f.Path)
		}
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	if mode == LinkHard || mode == LinkReflink {
		same, err := sameFilesystem(src, dst)
		if err != nil {
			return err
		}
		if !same {
			return ErrOtherFilesystem
		}
	}
	for _, f := range files {
		from := filepath.Join(src, filepath.FromSlash(f.Path))
		to := filepath.Join(dst, filepath.FromSlash(f.Path))
		if fi, err := os.Stat(to); err == nil {
			if fi.Size() == f.Size {
				continue
			}
			return fmt.Errorf("%s is in the way with another size", to)
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		var err error
		switch mode {
		case LinkHard:
			err = os.Link(from, to)
		case LinkSym:
			err = os.Symlink(from, to)
		case LinkReflink:
			err = reflink(from, to)
		default:
			err = fmt.Errorf("%q is not a link mode", mode)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// CheckFiles reports the first of files that is not under dir at its size.
func CheckFiles(dir string, files []FileEntry) error {
	for _, f := range files {
		if !filepath.IsLocal(filepath.FromSlash(f.Path)) {
			return fmt.Errorf("the path %q points outside its folder", f.Path)
		}
		p := filepath.Join(dir, filepath.FromSlash(f.Path))
		fi, err := os.Stat(p)
		if err != nil {
			return fmt.Errorf("%s is not there to seed from", p)
		}
		if fi.Size() != f.Size {
			return fmt.Errorf("%s is not whole", p)
		}
	}
	return nil
}

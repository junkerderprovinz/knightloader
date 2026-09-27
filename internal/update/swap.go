package update

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Swap unpacks a zip from Fetch beside the program and puts what it holds in
// place of the program, so the next start runs the new version while this one
// keeps running from what it started with. The zip holds the program alone at
// its top level: the executable, or on macOS the .app bundle.
func (u *Updater) Swap(download string, rel *Release) error {
	target, err := u.target()
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(target), downloadPrefix(target)+"*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := unzip(download, tmp); err != nil {
		return err
	}
	bundle := strings.HasSuffix(target, ".app")
	fresh, err := program(tmp, bundle)
	if err != nil {
		return fmt.Errorf("%s: %w", rel.asset, err)
	}
	if bundle {
		err = replaceBundle(target, fresh)
	} else {
		err = replaceFile(target, fresh)
	}
	if err != nil {
		return err
	}
	u.staged = rel.Version
	if u.UninstallKey != "" {
		if err := recordVersion(u.UninstallKey, target, rel.Version); err != nil {
			u.logf("update: recording version %s for the uninstall entry: %v", rel.Version, err)
		}
	}
	return nil
}

// Cleanup removes what an earlier run left behind: the program an update
// replaced, and downloads that never reached Swap.
func (u *Updater) Cleanup() {
	target, err := u.target()
	if err != nil {
		u.logf("update: %v", err)
		return
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(target), downloadPrefix(target)+"*"))
	for _, p := range append(leftovers, target+".old") {
		if err := os.RemoveAll(p); err != nil {
			u.logf("update: removing %s: %v", p, err)
		}
	}
}

func (u *Updater) target() (string, error) {
	if u.Path != "" {
		return u.Path, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	if runtime.GOOS != "darwin" {
		return exe, nil
	}
	app := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if filepath.Ext(app) != ".app" {
		return "", fmt.Errorf("%s does not run from an .app bundle", exe)
	}
	return app, nil
}

// downloadPrefix names the temporary files and folders next to target, so
// Cleanup finds the ones a crash left.
func downloadPrefix(target string) string {
	return "." + filepath.Base(target) + ".update-"
}

// program is the one entry an unpacked release zip holds: a folder ending in
// .app for a bundle, a plain file otherwise. Its name does not matter, since
// the update keeps whatever name the user gave the program.
func program(dir string, bundle bool) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	if len(entries) != 1 {
		return "", fmt.Errorf("the zip holds %d entries at its top level, want the program alone", len(entries))
	}
	e := entries[0]
	switch {
	case bundle && (!e.IsDir() || filepath.Ext(e.Name()) != ".app"):
		return "", fmt.Errorf("the zip holds %s, want an .app bundle", e.Name())
	case !bundle && !e.Type().IsRegular():
		return "", fmt.Errorf("the zip holds %s, want a program file", e.Name())
	}
	return filepath.Join(dir, e.Name()), nil
}

func replaceFile(target, fresh string) error {
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	if err := os.Chmod(fresh, info.Mode().Perm()); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		return os.Rename(fresh, target)
	}

	// Windows refuses to overwrite or delete a running program but lets it be
	// renamed, so it steps aside as .old until the next start removes it.
	old := target + ".old"
	if err := os.Remove(old); err != nil && !errors.Is(err, fs.ErrNotExist) {
		// The .old is the running program, left by an earlier update in this
		// run, so target is a file nothing runs and can simply be replaced.
		return rename(fresh, target)
	}
	if err := rename(target, old); err != nil {
		return err
	}
	if err := rename(fresh, target); err != nil {
		rename(old, target)
		return err
	}
	return nil
}

// rename retries for a moment while Windows refuses it. A virus scanner opens
// a program it has just seen written and holds it briefly, and until it lets
// go the file cannot be renamed or replaced.
func rename(from, to string) error {
	var err error
	for range 30 {
		if err = os.Rename(from, to); !errors.Is(err, fs.ErrPermission) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return err
}

// replaceBundle trades the running bundle for the unpacked one by rename, so
// the program is never half replaced. The old bundle waits as .old for the
// next start, since this run still loads from it.
func replaceBundle(app, fresh string) error {
	old := app + ".old"
	if err := os.RemoveAll(old); err != nil {
		return err
	}
	if err := os.Rename(app, old); err != nil {
		return err
	}
	if err := os.Rename(fresh, app); err != nil {
		os.Rename(old, app)
		return err
	}
	return nil
}

// unzip extracts through an os.Root, so neither a name nor a symlink in the
// archive can reach outside dest.
func unzip(archive, dest string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()
	root, err := os.OpenRoot(dest)
	if err != nil {
		return err
	}
	defer root.Close()

	for _, f := range r.File {
		name := strings.TrimSuffix(f.Name, "/")
		mode := f.Mode()
		if !mode.IsDir() {
			if err := root.MkdirAll(pathDir(name), 0o755); err != nil {
				return err
			}
		}
		switch {
		case mode.IsDir():
			err = root.MkdirAll(name, 0o755)
		case mode&fs.ModeSymlink != 0:
			err = extractLink(root, f, name)
		default:
			err = extractFile(root, f, name, mode.Perm())
		}
		if err != nil {
			return fmt.Errorf("extracting %s: %w", f.Name, err)
		}
	}
	return nil
}

func pathDir(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[:i]
	}
	return "."
}

func extractLink(root *os.Root, f *zip.File, name string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	link, err := io.ReadAll(io.LimitReader(rc, 4096))
	if err != nil {
		return err
	}
	return root.Symlink(string(link), name)
}

func extractFile(root *os.Root, f *zip.File, name string, perm fs.FileMode) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

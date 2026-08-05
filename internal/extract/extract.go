// Package extract unpacks downloaded archives (zip, rar incl. multi-volume,
// 7z incl. .001 volumes, tar.gz/tgz) into a folder next to the archive.
// Pure Go, no external binaries.
package extract

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bodgit/sevenzip"
	"github.com/nwaples/rardecode/v2"
)

// nonFirstVolume matches volumes of a multi-part set that must NOT start an
// extraction themselves (the first volume pulls them in automatically).
var nonFirstVolume = regexp.MustCompile(`(?i)(\.part(0*[2-9]|0*[1-9]\d+)\.rar|\.r\d\d|\.[7z]\S*\.\d*[2-9]\d*$|\.z\d\d)$`)

// firstVolume matches names that start an archive set (or are a whole archive).
var firstVolume = regexp.MustCompile(`(?i)(\.zip|\.part0*1\.rar|\.rar|\.7z|\.7z\.0*1|\.tar\.gz|\.tgz)$`)

// Supported reports whether name looks like an archive this package can start
// extracting (first volume of a set, or a single archive).
func Supported(name string) bool {
	l := strings.ToLower(name)
	if nonFirstVolume.MatchString(l) {
		return false
	}
	// ".rar" also matches ".partN.rar"; the nonFirstVolume check above already
	// rejected parts 2+, so what remains is .part1.rar or a plain .rar.
	return firstVolume.MatchString(l)
}

// volumeFamilies maps a trailing volume marker to the archive family it belongs
// to. Two files in one folder with the same base name and the same family are
// parts of one archive, which is what lets the caller wait for a complete set.
// Order matters: the more specific marker has to be tried first (.part01.rar
// before .rar, .7z.001 before .7z).
var volumeFamilies = []struct {
	re     *regexp.Regexp
	family string
}{
	{regexp.MustCompile(`(?i)\.part\d+\.rar$`), "rar"},
	{regexp.MustCompile(`(?i)\.r\d\d$`), "rar"},
	{regexp.MustCompile(`(?i)\.rar$`), "rar"},
	{regexp.MustCompile(`(?i)\.7z\.\d{3}$`), "7z"},
	{regexp.MustCompile(`(?i)\.7z$`), "7z"},
	{regexp.MustCompile(`(?i)\.z\d\d$`), "zip"},
	{regexp.MustCompile(`(?i)\.zip$`), "zip"},
}

// SetKey identifies the volume set a file belongs to: every part of one
// multi-volume archive returns the same key. ok is false for names that cannot
// be part of a set (a .tar.gz, a plain file). A single-file archive is simply a
// set of one.
func SetKey(name string) (key string, ok bool) {
	base := filepath.Base(name)
	for _, f := range volumeFamilies {
		if loc := f.re.FindStringIndex(base); loc != nil {
			return strings.ToLower(base[:loc[0]]) + "|" + f.family, true
		}
	}
	return "", false
}

// Result describes a finished extraction.
type Result struct {
	Dir     string   // directory the archive was extracted into
	Files   int      // number of files written
	Volumes []string // all volume files consumed (for delete-after-extract)
}

// ErrPasswordRequired says the archive is encrypted and none of the supplied
// passwords opened it. It is a distinct error so the app can tell "I need a
// password" from "this archive is broken".
var ErrPasswordRequired = errors.New("extract: the archive is encrypted and no password fits")

// Extract unpacks the archive at path into a sibling directory named after the
// archive. It refuses to write outside that directory (zip-slip safe).
func Extract(path string) (*Result, error) {
	return ExtractWith(path, nil)
}

// ExtractWith is Extract with passwords to try, in order, if the archive turns
// out to be encrypted. The unencrypted attempt always comes first, so a normal
// archive never pays for the list.
func ExtractWith(path string, passwords []string) (*Result, error) {
	dest := destDir(path)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, err
	}
	res, err := extractOnce(path, dest, "")
	if err == nil || !errors.Is(err, ErrPasswordRequired) {
		return res, err
	}
	for _, pw := range passwords {
		if pw == "" {
			continue
		}
		res, err = extractOnce(path, dest, pw)
		if err == nil || !errors.Is(err, ErrPasswordRequired) {
			return res, err
		}
	}
	return nil, ErrPasswordRequired
}

func extractOnce(path, dest, password string) (*Result, error) {
	l := strings.ToLower(path)
	switch {
	case strings.HasSuffix(l, ".zip"):
		return extractZip(path, dest)
	case strings.HasSuffix(l, ".rar"):
		return extractRar(path, dest, password)
	case strings.HasSuffix(l, ".7z") || sevenZipVolume.MatchString(l):
		return extract7z(path, dest, password)
	case strings.HasSuffix(l, ".tar.gz") || strings.HasSuffix(l, ".tgz"):
		return extractTarGz(path, dest)
	}
	return nil, fmt.Errorf("extract: unsupported archive %q", filepath.Base(path))
}

// sevenZipVolume matches the first part of a split 7z archive.
var sevenZipVolume = regexp.MustCompile(`(?i)\.7z\.0*1$`)

// destDir returns the extraction directory for an archive path: the archive
// name without its (possibly multi-part) extension, next to the archive.
func destDir(path string) string {
	base := filepath.Base(path)
	for _, suf := range []string{".tar.gz", ".tgz", ".zip", ".rar", ".7z"} {
		if i := strings.LastIndex(strings.ToLower(base), suf); i > 0 {
			base = base[:i]
			break
		}
	}
	base = strings.TrimSuffix(base, ".part1")
	base = strings.TrimSuffix(base, ".part01")
	return filepath.Join(filepath.Dir(path), base)
}

// safePath joins name under dest and rejects traversal outside dest.
func safePath(dest, name string) (string, error) {
	p := filepath.Join(dest, filepath.FromSlash(name))
	rel, err := filepath.Rel(dest, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("extract: illegal path %q", name)
	}
	return p, nil
}

func writeFile(dst string, mode os.FileMode, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if mode == 0 {
		mode = 0o644
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	_, err = io.Copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func extractZip(path, dest string) (*Result, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	res := &Result{Dir: dest, Volumes: []string{path}}
	for _, f := range zr.File {
		if f.Flags&0x1 != 0 {
			// Go's archive/zip cannot decrypt, and there is no honest way to
			// pretend otherwise.
			return nil, errors.New("extract: encrypted zip archives are not supported")
		}
		if f.FileInfo().IsDir() {
			continue
		}
		dst, err := safePath(dest, f.Name)
		if err != nil {
			return nil, err
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		err = writeFile(dst, f.Mode(), rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		res.Files++
	}
	return res, nil
}

func extractRar(path, dest, password string) (*Result, error) {
	var opts []rardecode.Option
	if password != "" {
		opts = append(opts, rardecode.Password(password))
	}
	rc, err := rardecode.OpenReader(path, opts...)
	if err != nil {
		if errors.Is(err, rardecode.ErrArchiveEncrypted) || errors.Is(err, rardecode.ErrArchivedFileEncrypted) {
			return nil, ErrPasswordRequired
		}
		return nil, err
	}
	defer rc.Close()
	res := &Result{Dir: dest}
	// Sequential Next/Read works for solid and multi-volume archives alike.
	for {
		h, err := rc.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			if errors.Is(err, rardecode.ErrArchivedFileEncrypted) || errors.Is(err, rardecode.ErrArchiveEncrypted) {
				return nil, ErrPasswordRequired
			}
			return nil, err
		}
		if h.IsDir {
			continue
		}
		dst, err := safePath(dest, h.Name)
		if err != nil {
			return nil, err
		}
		if err := writeFile(dst, h.Mode(), rc); err != nil {
			return nil, err
		}
		res.Files++
	}
	res.Volumes = rc.Volumes()
	return res, nil
}

func extract7z(path, dest, password string) (*Result, error) {
	var zr *sevenzip.ReadCloser
	var err error
	if password == "" {
		zr, err = sevenzip.OpenReader(path)
	} else {
		zr, err = sevenzip.OpenReaderWithPassword(path, password)
	}
	if err != nil {
		if isEncrypted(err) {
			return nil, ErrPasswordRequired
		}
		return nil, err
	}
	defer zr.Close()
	res := &Result{Dir: dest, Volumes: zr.Volumes()}
	// In-order extraction so solid-block streams are read once.
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		dst, err := safePath(dest, f.Name)
		if err != nil {
			return nil, err
		}
		rc, err := f.Open()
		if err != nil {
			if isEncrypted(err) {
				return nil, ErrPasswordRequired
			}
			return nil, err
		}
		err = writeFile(dst, f.Mode(), rc)
		rc.Close()
		if err != nil {
			if isEncrypted(err) {
				return nil, ErrPasswordRequired
			}
			return nil, err
		}
		res.Files++
	}
	return res, nil
}

// isEncrypted recognises the 7z library's password failures, which it reports
// as plain errors rather than typed ones.
func isEncrypted(err error) bool {
	if err == nil {
		return false
	}
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "password") || strings.Contains(m, "decrypt") ||
		strings.Contains(m, "checksum") || strings.Contains(m, "encrypt")
}

func extractTarGz(path, dest string) (*Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	res := &Result{Dir: dest, Volumes: []string{path}}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		dst, err := safePath(dest, h.Name)
		if err != nil {
			return nil, err
		}
		if err := writeFile(dst, os.FileMode(h.Mode), tr); err != nil {
			return nil, err
		}
		res.Files++
	}
	return res, nil
}

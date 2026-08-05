// Package extract unpacks downloaded archives (zip, rar incl. multi-volume,
// 7z incl. .001 volumes, tar, and the gzip/bzip2/xz/zstd single-stream formats
// whether or not they wrap a tar) into a folder next to the archive.
// Pure Go, no external binaries.
package extract

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bodgit/sevenzip"
	"github.com/klauspost/compress/zstd"
	"github.com/nwaples/rardecode/v2"
	"github.com/ulikunitz/xz"
)

// nonFirstVolume matches volumes of a multi-part set that must NOT start an
// extraction themselves (the first volume pulls them in automatically).
var nonFirstVolume = regexp.MustCompile(`(?i)(\.part(0*[2-9]|0*[1-9]\d+)\.rar|\.r\d\d|\.[7z]\S*\.\d*[2-9]\d*$|\.z\d\d)$`)

// firstVolume matches names that start an archive set (or are a whole archive).
// The single-stream compressions are listed in both their ".tar.X" and their
// bare spelling, because whether the stream holds a tar is decided by the
// content later on and must not gate whether we offer to unpack it at all.
var firstVolume = regexp.MustCompile(`(?i)(` +
	`\.zip|\.part0*1\.rar|\.rar|\.7z|\.7z\.0*1|` +
	`\.tar|\.tar\.gz|\.tgz|\.tar\.bz2|\.tbz2?|\.tar\.xz|\.txz|\.tar\.zst|\.tzst|` +
	`\.gz|\.bz2|\.xz|\.zst` +
	`)$`)

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
	case strings.HasSuffix(l, ".tar"):
		return extractTar(path, dest)
	}
	// Every single-stream compression shares one code path; the suffix only
	// picks the codec, never what the decompressed bytes turn out to be.
	for _, c := range compressions {
		if strings.HasSuffix(l, c.suffix) {
			return extractCompressed(path, dest, c.suffix, c.open)
		}
	}
	return nil, fmt.Errorf("extract: unsupported archive %q", filepath.Base(path))
}

// sevenZipVolume matches the first part of a split 7z archive.
var sevenZipVolume = regexp.MustCompile(`(?i)\.7z\.0*1$`)

// archiveSuffixes are stripped from an archive name to get its extraction
// directory. Longest first, because the list is scanned until something
// matches: ".gz" ahead of ".tar.gz" would drop "data.tar.gz" into "data.tar".
var archiveSuffixes = []string{
	".tar.gz", ".tar.bz2", ".tar.xz", ".tar.zst",
	".tgz", ".tbz2", ".tbz", ".txz", ".tzst",
	".tar", ".gz", ".bz2", ".xz", ".zst",
	".zip", ".rar", ".7z",
}

// destDir returns the extraction directory for an archive path: the archive
// name without its (possibly multi-part) extension, next to the archive.
func destDir(path string) string {
	base := filepath.Base(path)
	// A split 7z volume carries its part number behind the extension
	// ("set.7z.001"), so fold it back to a plain ".7z" before matching.
	if loc := sevenZipVolume.FindStringIndex(strings.ToLower(base)); loc != nil {
		base = base[:loc[0]] + ".7z"
	}
	l := strings.ToLower(base)
	for _, suf := range archiveSuffixes {
		if len(base) > len(suf) && strings.HasSuffix(l, suf) {
			base = base[:len(base)-len(suf)]
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

// unpackTar writes every regular entry of a tar stream under dest and counts
// them in res. Directories and the various special entries are skipped: the
// directories are recreated implicitly by writeFile, and devices, links and
// fifos have no business being restored from a download.
func unpackTar(tr *tar.Reader, dest string, res *Result) error {
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		dst, err := safePath(dest, h.Name)
		if err != nil {
			return err
		}
		if err := writeFile(dst, os.FileMode(h.Mode), tr); err != nil {
			return err
		}
		res.Files++
	}
}

func extractTar(path, dest string) (*Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	res := &Result{Dir: dest, Volumes: []string{path}}
	if err := unpackTar(tar.NewReader(f), dest, res); err != nil {
		return nil, err
	}
	return res, nil
}

// compressions lists every single-stream compression this package can open,
// mapped to the decompressor that opens it. Longest suffix first, so that
// ".tar.gz" is reported as the suffix rather than ".gz" when both match; the
// suffix is what a non-tar payload gets named after.
var compressions = []struct {
	suffix string
	open   func(io.Reader) (io.ReadCloser, error)
}{
	{".tar.gz", openGzip},
	{".tar.bz2", openBzip2},
	{".tar.xz", openXz},
	{".tar.zst", openZstd},
	{".tgz", openGzip},
	{".tbz2", openBzip2},
	{".tbz", openBzip2},
	{".txz", openXz},
	{".tzst", openZstd},
	{".gz", openGzip},
	{".bz2", openBzip2},
	{".xz", openXz},
	{".zst", openZstd},
}

func openGzip(r io.Reader) (io.ReadCloser, error) { return gzip.NewReader(r) }

// openBzip2 wraps the stdlib decompressor, which holds nothing that needs
// releasing, in a no-op closer so all four codecs share one signature.
func openBzip2(r io.Reader) (io.ReadCloser, error) {
	return io.NopCloser(bzip2.NewReader(r)), nil
}

func openXz(r io.Reader) (io.ReadCloser, error) {
	xr, err := xz.NewReader(r)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(xr), nil
}

func openZstd(r io.Reader) (io.ReadCloser, error) {
	zr, err := zstd.NewReader(r)
	if err != nil {
		return nil, err
	}
	// The decoder runs goroutines, so hand back its own closer rather than a
	// no-op one; dropping it would leak them for the life of the process.
	return zr.IOReadCloser(), nil
}

// tarProbeSize is how much of the decompressed stream the tar probe gets to
// look at. A header block is 512 bytes, but PAX and GNU long-name archives put
// extra records in front of the first real header, so give it plenty of room.
const tarProbeSize = 64 << 10

// looksLikeTar reports whether head begins with a header archive/tar accepts.
//
// The file name deliberately does not get a vote. Double extensions in the wild
// are unreliable in both directions: ".tgz" and ".tar.gz" are handed out for
// single gzipped files that hold no tar at all, and plain ".gz" downloads
// routinely do hold one. A tar header carries a checksum over its own bytes, so
// reading one is a far stronger signal than anything the name claims.
func looksLikeTar(head []byte) bool {
	if len(head) < 512 {
		return false
	}
	_, err := tar.NewReader(bytes.NewReader(head)).Next()
	return err == nil
}

// extractCompressed handles every single-stream compressed archive: it opens
// the codec, decides from the content whether the stream is a tar, and either
// unpacks it or writes the payload out as one file.
func extractCompressed(path, dest, suffix string, open func(io.Reader) (io.ReadCloser, error)) (*Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec, err := open(f)
	if err != nil {
		return nil, err
	}
	defer dec.Close()

	// Peek rather than read: when the probe says "not a tar", those same bytes
	// are still the beginning of the file we have to write out.
	br := bufio.NewReaderSize(dec, tarProbeSize)
	head, _ := br.Peek(tarProbeSize)

	res := &Result{Dir: dest, Volumes: []string{path}}
	if looksLikeTar(head) {
		if err := unpackTar(tar.NewReader(br), dest, res); err != nil {
			return nil, err
		}
		return res, nil
	}
	dst, err := safePath(dest, payloadName(filepath.Base(path), suffix))
	if err != nil {
		return nil, err
	}
	if err := writeFile(dst, 0, br); err != nil {
		return nil, err
	}
	res.Files++
	return res, nil
}

// payloadName is the name a non-tar payload is written under: the archive name
// minus its compression suffix, so "notes.txt.gz" yields "notes.txt". A ".tgz"
// that turned out not to hold a tar loses the whole suffix instead of gaining a
// ".tar", because calling it a tar would be exactly the lie the probe caught.
func payloadName(base, suffix string) string {
	name := base[:len(base)-len(suffix)]
	if name == "" {
		name = "content"
	}
	return name
}

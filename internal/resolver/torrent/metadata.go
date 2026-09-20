package torrent

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

// The limits a .torrent must fit before it is opened. No honest torrent
// exceeds them, and a hostile one gains from each.
const (
	// MaxTorrentBytes is the largest .torrent this app parses. A 100 GB
	// torrent at 4 MiB pieces carries about half a megabyte of hashes. The
	// upload route enforces it too, since Parse only sees bytes already in
	// memory.
	MaxTorrentBytes = 2 << 20
	// MaxFiles is the longest file list; real archives reach a few thousand.
	MaxFiles = 50_000
	// MinPieceLength and MaxPieceLength bracket a transferable piece size.
	// Below 16 KiB the hash list outgrows the data; above 1 GiB a piece is
	// larger than most shared files.
	MinPieceLength = 16 << 10
	MaxPieceLength = 1 << 30
	// MaxTrackers caps the announce list, each entry being an outbound
	// connection made on a stranger's behalf.
	MaxTrackers = 1024
	// MaxPathBytes and MaxPathComponentBytes are the path and segment limits
	// of mainstream filesystems.
	MaxPathBytes          = 4096
	MaxPathComponentBytes = 255
)

// The refusals are distinct values so a caller can tell "not a torrent" from
// "a hostile torrent" and pick an HTTP status for each.
var (
	// ErrTooLarge is a .torrent over MaxTorrentBytes.
	ErrTooLarge = errors.New("this .torrent file is too large to be a real one")
	// ErrNotTorrent is bytes that are not a bencoded torrent at all.
	ErrNotTorrent = errors.New("this is not a .torrent file")
	// ErrNoInfo is a bencoded file with no usable info dictionary.
	ErrNoInfo = errors.New("this .torrent has no usable info dictionary")
	// ErrPieceGeometry is a piece length or piece count that cannot describe the
	// data the torrent claims to hold.
	ErrPieceGeometry = errors.New("this .torrent's piece layout does not match the data it describes")
	// ErrTooManyFiles is a file list over MaxFiles.
	ErrTooManyFiles = errors.New("this .torrent lists more files than this app will open")
	// ErrUnsafePath is a file path that would write outside the download
	// folder.
	ErrUnsafePath = errors.New("refused: this .torrent contains a file path that would write outside the download folder")
	// ErrDuplicatePath is two files at one path, which lets the selection
	// tree show one entry while another is written.
	ErrDuplicatePath = errors.New("refused: this .torrent lists the same file path twice")
	// ErrBadTracker is a malformed announce URL.
	ErrBadTracker = errors.New("refused: this .torrent has a malformed tracker URL")
)

// Metadata is a .torrent read, checked and reduced to what KnightLoader needs:
// a file tree to show and the flags for DHT and PEX.
type Metadata struct {
	// InfoHash is the v1 hex info hash, or the v2 one for a v2-only torrent.
	InfoHash string
	// Name is the torrent's own name, checked as a single safe path segment
	// since a multi-file torrent lands in a folder of that name.
	Name string
	// Private is BEP 27's info.private flag, which decides DHT and PEX.
	// gopeed reads it internally but never exposes it.
	Private bool
	// TotalSize is every file added up, selected or not.
	TotalSize int64
	// PieceLength and Pieces explain a refusal.
	PieceLength int64
	Pieces      int
	// Files is the tree in the order gopeed indexes it: both come from this
	// info's UpvertedFiles(), and SelectFiles is a list of indices, so the
	// order must match.
	Files []core.TorrentFile
	// Trackers are the announce URLs that survived checking, deduplicated.
	Trackers []string
	// DroppedTrackers counts announce URLs with a scheme nothing here can
	// speak, reported for a user chasing a stalled torrent.
	DroppedTrackers int
}

// Parse reads a .torrent and refuses it if anything about it is wrong. It uses
// anacrolix/torrent's metainfo, the parser gopeed uses as well, so the checked
// file and the file that gets opened cannot differ.
func Parse(b []byte) (Metadata, error) {
	if len(b) > MaxTorrentBytes {
		return Metadata{}, fmt.Errorf("%w: %d bytes, the limit is %d", ErrTooLarge, len(b), MaxTorrentBytes)
	}
	if len(b) == 0 {
		return Metadata{}, ErrNotTorrent
	}
	mi, err := metainfo.Load(bytes.NewReader(b))
	// gopeed ignores a trailing "expected EOF" (anacrolix/torrent#992), so a
	// torrent it would fetch is not refused here.
	if err != nil && !strings.Contains(err.Error(), "expected EOF") {
		return Metadata{}, fmt.Errorf("%w: %v", ErrNotTorrent, err)
	}
	if mi == nil {
		return Metadata{}, ErrNotTorrent
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return Metadata{}, fmt.Errorf("%w: %v", ErrNoInfo, err)
	}
	return fromInfo(mi, &info)
}

func fromInfo(mi *metainfo.MetaInfo, info *metainfo.Info) (Metadata, error) {
	m := Metadata{
		Name:        info.BestName(),
		PieceLength: info.PieceLength,
		Pieces:      info.NumPieces(),
		InfoHash:    mi.HashInfoBytes().HexString(),
	}
	if info.Private != nil {
		m.Private = *info.Private
	}
	if err := checkGeometry(info); err != nil {
		return Metadata{}, err
	}
	// A multi-file torrent's name is the folder its files land in; one named
	// ".." would escape with otherwise ordinary paths.
	if info.IsDir() {
		if err := safeComponent(m.Name); err != nil {
			return Metadata{}, fmt.Errorf("%w: the torrent's own name %q is not a usable folder name", ErrUnsafePath, m.Name)
		}
	}

	files := info.UpvertedFiles()
	if len(files) > MaxFiles {
		return Metadata{}, fmt.Errorf("%w: %d files, the limit is %d", ErrTooManyFiles, len(files), MaxFiles)
	}
	seen := make(map[string]bool, len(files))
	m.Files = make([]core.TorrentFile, 0, len(files))
	for i := range files {
		p, err := relPath(info, &files[i])
		if err != nil {
			return Metadata{}, err
		}
		// Compared case-insensitively and without trailing dots or spaces,
		// which Windows and default macOS volumes fold into one file.
		key := strings.ToLower(strings.TrimRight(p, ". "))
		if seen[key] {
			return Metadata{}, fmt.Errorf("%w: %q", ErrDuplicatePath, p)
		}
		seen[key] = true
		if files[i].Length < 0 {
			return Metadata{}, fmt.Errorf("%w: %q claims a negative length", ErrNotTorrent, p)
		}
		m.TotalSize += files[i].Length
		// Selected by default, as in other torrent clients.
		m.Files = append(m.Files, core.TorrentFile{Path: p, Size: files[i].Length, Selected: true})
	}
	if len(m.Files) == 0 {
		return Metadata{}, fmt.Errorf("%w: it lists no files", ErrNoInfo)
	}

	tr, dropped, err := trackers(mi)
	if err != nil {
		return Metadata{}, err
	}
	m.Trackers, m.DroppedTrackers = tr, dropped
	return m, nil
}

// checkGeometry refuses a piece length out of range and a hash list that does
// not cover exactly the bytes the torrent claims, which every real torrent
// satisfies by construction.
func checkGeometry(info *metainfo.Info) error {
	if info.PieceLength < MinPieceLength || info.PieceLength > MaxPieceLength {
		return fmt.Errorf("%w: a piece length of %d is outside %d..%d",
			ErrPieceGeometry, info.PieceLength, MinPieceLength, MaxPieceLength)
	}
	if info.HasV2() && len(info.Pieces) == 0 {
		// A v2-only torrent keeps its hashes per file in the file tree.
		if info.FileTree.IsDir() || info.NumPieces() > 0 {
			return nil
		}
		return fmt.Errorf("%w: a v2 torrent with an empty file tree", ErrPieceGeometry)
	}
	if len(info.Pieces)%20 != 0 {
		return fmt.Errorf("%w: the piece hash list is %d bytes, which is not a whole number of 20-byte hashes",
			ErrPieceGeometry, len(info.Pieces))
	}
	total := info.TotalLength()
	if total <= 0 {
		return fmt.Errorf("%w: it describes %d bytes of data", ErrPieceGeometry, total)
	}
	want := int((total + info.PieceLength - 1) / info.PieceLength)
	if got := len(info.Pieces) / 20; got != want {
		return fmt.Errorf("%w: %d piece hashes for %d bytes at %d per piece, which needs %d",
			ErrPieceGeometry, got, total, info.PieceLength, want)
	}
	return nil
}

// relPath is one file's path inside the torrent, forward-slashed and checked.
func relPath(info *metainfo.Info, fi *metainfo.FileInfo) (string, error) {
	parts := fi.BestPath()
	if len(parts) == 0 {
		// A single-file torrent's name is the file name.
		if err := safeComponent(info.BestName()); err != nil {
			return "", fmt.Errorf("%w: %q", ErrUnsafePath, info.BestName())
		}
		return info.BestName(), nil
	}
	for _, c := range parts {
		if err := safeComponent(c); err != nil {
			return "", fmt.Errorf("%w: %q in %q", ErrUnsafePath, c, strings.Join(parts, "/"))
		}
	}
	p := strings.Join(parts, "/")
	if len(p) > MaxPathBytes {
		return "", fmt.Errorf("%w: a path of %d bytes", ErrUnsafePath, len(p))
	}
	return p, nil
}

// windowsReservedNames are device names Windows resolves whatever follows the
// first dot: "CON", "con.txt" and "CON.tar.gz" all address the console.
var windowsReservedNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// safeComponent accepts one segment of an in-torrent path only if it is a
// plain name: not empty, no traversal, no separator of either kind, no
// control character, no colon and no Windows device name. A colon is an NTFS
// alternate-data-stream separator: "readme.txt:payload.exe" writes a hidden
// stream onto readme.txt.
func safeComponent(c string) error {
	switch {
	case c == "":
		return errors.New("empty path segment")
	case c == "." || c == "..":
		return errors.New("traversal segment")
	case len(c) > MaxPathComponentBytes:
		return errors.New("path segment too long")
	case strings.ContainsAny(c, `/\`):
		return errors.New("path separator inside a segment")
	case strings.ContainsRune(c, ':'):
		return errors.New("colon inside a path segment (an NTFS alternate-data-stream separator)")
	}
	for _, r := range c {
		if r < 0x20 || r == 0x7f {
			return errors.New("control character in a path segment")
		}
	}
	base, _, _ := strings.Cut(c, ".")
	if windowsReservedNames[strings.ToLower(base)] {
		return errors.New("a Windows reserved device name")
	}
	return nil
}

// Contained checks that every path in rels stays inside dir once joined.
//
// It is the second gate after Parse, for a magnet's file list, which arrives
// from the swarm after the engine has the link; the engine runs it before
// writing a byte. rels are forward-slashed paths relative to dir, including
// the torrent's folder name where the library nests files under it.
func Contained(dir string, rels []string) error {
	if dir == "" {
		return fmt.Errorf("%w: no download folder to check against", ErrUnsafePath)
	}
	base := filepath.Clean(dir)
	for _, rel := range rels {
		if rel == "" {
			return fmt.Errorf("%w: an empty file path", ErrUnsafePath)
		}
		// Checked by hand rather than with filepath.IsAbs so the result does
		// not depend on the platform.
		if rooted(rel) {
			return fmt.Errorf("%w: %q is a rooted path", ErrUnsafePath, rel)
		}
		// The same colon rule as safeComponent, for paths that never passed
		// through Parse.
		if strings.ContainsRune(rel, ':') {
			return fmt.Errorf("%w: %q contains a colon", ErrUnsafePath, rel)
		}
		// rel is forward-slashed, so a backslash is never a separator here.
		// Join treats it as one only on Windows, where "..\x" escapes, so it
		// is refused on every platform.
		if strings.ContainsRune(rel, '\\') {
			return fmt.Errorf("%w: %q contains a backslash", ErrUnsafePath, rel)
		}
		full := filepath.Join(base, filepath.FromSlash(rel))
		if !within(base, full) {
			return fmt.Errorf("%w: %q resolves to %q", ErrUnsafePath, rel, full)
		}
	}
	return nil
}

// rooted reports whether a path states a root of its own: a leading separator
// of either flavour, or a Windows drive or UNC prefix.
func rooted(p string) bool {
	if p == "" {
		return false
	}
	if p[0] == '/' || p[0] == '\\' {
		return true
	}
	return len(p) >= 2 && p[1] == ':' &&
		((p[0] >= 'a' && p[0] <= 'z') || (p[0] >= 'A' && p[0] <= 'Z'))
}

// within is a copy of app's withinDir; internal/app imports this package, not
// the other way round. Keep the two identical.
func within(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// usableTrackerSchemes are the schemes the embedded download library can
// announce to.
var usableTrackerSchemes = map[string]bool{
	"http": true, "https": true, "udp": true, "ws": true, "wss": true,
}

// trackers reduces an announce list to the URLs that can be announced to. An
// unusable scheme is dropped and counted, since real torrents carry dead
// entries; a control character refuses the torrent, since it only appears in
// a string built to be interpreted somewhere.
func trackers(mi *metainfo.MetaInfo) ([]string, int, error) {
	raw := make([]string, 0, 8)
	if mi.Announce != "" {
		raw = append(raw, mi.Announce)
	}
	for _, tier := range mi.AnnounceList {
		raw = append(raw, tier...)
	}
	if len(raw) > MaxTrackers {
		return nil, 0, fmt.Errorf("%w: %d announce URLs, the limit is %d", ErrBadTracker, len(raw), MaxTrackers)
	}
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	dropped := 0
	for _, t := range raw {
		t = strings.TrimSpace(t)
		if t == "" {
			dropped++
			continue
		}
		for _, r := range t {
			if r < 0x20 || r == 0x7f {
				return nil, 0, fmt.Errorf("%w: an announce URL contains a control character", ErrBadTracker)
			}
		}
		u, err := url.Parse(t)
		if err != nil || !usableTrackerSchemes[strings.ToLower(u.Scheme)] || u.Host == "" {
			dropped++
			continue
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out, dropped, nil
}

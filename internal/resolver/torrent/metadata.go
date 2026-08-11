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

// The limits a .torrent has to fit inside before anything reads it as one.
//
// These are refusals, not warnings. A torrent that trips one of them is a file
// this app will not open, because every one of them describes something no
// honest torrent does and something a hostile one gains from: a metadata blob
// large enough to be the attack, a file list long enough to be the attack, a
// piece geometry that cannot describe the bytes it claims to.
const (
	// MaxTorrentBytes is the largest .torrent this app will parse. Piece hashes
	// are twenty bytes each, so even a 100 GB torrent at the 4 MiB piece size
	// modern clients pick is around half a megabyte of metadata; two megabytes
	// leaves room for a small-pieced older one and still refuses a blob sent to
	// see what happens.
	//
	// THE UPLOAD ROUTE MUST ENFORCE THIS TOO rather than relying on this parser,
	// for the reason every size limit is enforced at the door: by the time Parse
	// sees the bytes they are already in memory. It is exported so there is one
	// number and not two - see internal/api's .torrent upload handler.
	MaxTorrentBytes = 2 << 20
	// MaxFiles is the longest file list. Real torrents of complete discographies
	// and archive dumps reach a few thousand.
	MaxFiles = 50_000
	// MinPieceLength / MaxPieceLength bracket a piece geometry that can actually
	// be transferred. Below 16 KiB the hash list outgrows the data it describes;
	// above a gibibyte a single piece is larger than most of the files anyone
	// shares, and both ends are what a zero or an absurd value looks like once
	// it has been through a multiplication.
	MinPieceLength = 16 << 10
	MaxPieceLength = 1 << 30
	// MaxTrackers caps the announce list. A torrent with a thousand trackers is
	// asking this client to make a thousand outbound connections on somebody
	// else's behalf.
	MaxTrackers = 1024
	// MaxPathBytes is the longest in-torrent path, and MaxPathComponentBytes the
	// longest single segment of one. Both sit at the boundary every mainstream
	// filesystem stops at.
	MaxPathBytes          = 4096
	MaxPathComponentBytes = 255
)

// The refusals. They are values rather than sentences so a caller can tell
// "this is not a torrent at all" (the paste box pasted something else) from
// "this is a torrent and it is hostile" (which somebody should be told about),
// and so the HTTP layer can pick its own status code for each.
var (
	// ErrTooLarge is a .torrent over MaxTorrentBytes.
	ErrTooLarge = errors.New("this .torrent file is too large to be a real one")
	// ErrNotTorrent is bytes that are not a bencoded torrent at all.
	ErrNotTorrent = errors.New("this is not a .torrent file")
	// ErrNoInfo is a bencoded file with no usable info dictionary - the part
	// that says what the torrent actually contains.
	ErrNoInfo = errors.New("this .torrent has no usable info dictionary")
	// ErrPieceGeometry is a piece length or piece count that cannot describe the
	// data the torrent claims to hold.
	ErrPieceGeometry = errors.New("this .torrent's piece layout does not match the data it describes")
	// ErrTooManyFiles is a file list over MaxFiles.
	ErrTooManyFiles = errors.New("this .torrent lists more files than this app will open")
	// ErrUnsafePath is the one this whole file exists for: a file inside the
	// torrent whose own path would write outside the download folder.
	ErrUnsafePath = errors.New("refused: this .torrent contains a file path that would write outside the download folder")
	// ErrDuplicatePath is two files at the same path. Harmless-looking, and it
	// is how a selection tree is made to lie: the box the user unticks and the
	// file that gets written are two different entries with one name.
	ErrDuplicatePath = errors.New("refused: this .torrent lists the same file path twice")
	// ErrBadTracker is an announce URL with something in it that has no business
	// being in a URL.
	ErrBadTracker = errors.New("refused: this .torrent has a malformed tracker URL")
)

// Metadata is a .torrent read, checked and reduced to what KnightLoader needs:
// enough to show a file tree, enough to decide DHT/PEX, and nothing that came
// out of the file unexamined.
type Metadata struct {
	// InfoHash is the v1 hex info hash, or the v2 one for a v2-only torrent.
	InfoHash string
	// Name is the torrent's own name, already checked as a single safe path
	// segment - it is the folder a multi-file torrent lands in.
	Name string
	// Private is BEP 27's info.private flag. It is the whole input to the
	// private-tracker DHT/PEX decision, and it is read HERE because gopeed
	// reads it too but never exposes it: its bt fetcher computes the same
	// boolean locally in addTorrent and uses it only to skip adding extra
	// trackers, so nothing downstream of the download library can see it.
	Private bool
	// TotalSize is every file added up, selected or not.
	TotalSize int64
	// PieceLength and Pieces are kept because they are what makes a torrent
	// plausible, and a caller that wants to say why one was refused needs them.
	PieceLength int64
	Pieces      int
	// Files is the tree, in the SAME ORDER the download library will index it.
	// That is not a coincidence to be relied on quietly: gopeed builds its file
	// list from anacrolix's Torrent.Files(), which is built from exactly this
	// info's UpvertedFiles() (torrent/t.go initFiles), so position i here is
	// position i in base.Options.SelectFiles. A selection is a list of indices,
	// so a different order would silently download different files.
	Files []core.TorrentFile
	// Trackers are the announce URLs that survived checking, deduplicated.
	Trackers []string
	// DroppedTrackers counts announce URLs left out because nothing here can
	// speak to them. Reported rather than hidden: "12 of 40 trackers were
	// ignored" is something a user chasing a stalled torrent needs to know.
	DroppedTrackers int
}

// Parse reads a .torrent and refuses it if anything about it is wrong.
//
// It parses with the same library the download library itself parses with
// (anacrolix/torrent's metainfo, which gopeed's bt fetcher calls), deliberately:
// a validator that accepts a different set of files from the thing that
// eventually opens them is a validator that can be walked around, and the
// interesting direction is the one where this says yes and the real parser
// reads something else.
func Parse(b []byte) (Metadata, error) {
	if len(b) > MaxTorrentBytes {
		return Metadata{}, fmt.Errorf("%w: %d bytes, the limit is %d", ErrTooLarge, len(b), MaxTorrentBytes)
	}
	if len(b) == 0 {
		return Metadata{}, ErrNotTorrent
	}
	mi, err := metainfo.Load(bytes.NewReader(b))
	// The download library carries a hotfix for anacrolix/torrent#992 and
	// ignores a trailing "expected EOF"; this ignores it for the same reason and
	// no other, because a torrent gopeed would happily fetch must not be refused
	// here as malformed.
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
	// The torrent's own name is the folder every file in it lands in, so it is
	// a path segment before it is a label and gets checked as one. A torrent
	// named ".." with one file in it escapes without a single unusual file path.
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
		// Case-insensitively AND with a trailing dot/space trimmed, because the
		// filesystem this lands on may well fold both away: on Windows and on a
		// default macOS volume "A/x" and "a/X" are one file, and Windows silently
		// strips a trailing dot or space from the name it actually creates, so
		// "a.txt" and "a.txt " land on the very same file there too - a tree
		// that shows the user two rows for one real file is a tree they cannot
		// actually choose between.
		key := strings.ToLower(strings.TrimRight(p, ". "))
		if seen[key] {
			return Metadata{}, fmt.Errorf("%w: %q", ErrDuplicatePath, p)
		}
		seen[key] = true
		if files[i].Length < 0 {
			return Metadata{}, fmt.Errorf("%w: %q claims a negative length", ErrNotTorrent, p)
		}
		m.TotalSize += files[i].Length
		// Selected by default. Unticking is the deliberate act, the same way
		// every other torrent client presents it, and the safe direction if a
		// tree is never shown at all.
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

// checkGeometry is the "zero-length or absurd piece count" gate.
//
// The consistency check at the end is the one that matters and the one a size
// range alone would miss: piece length and total length are two numbers the
// author chose, and a torrent whose hash list does not cover the bytes it
// claims is either corrupt or built to see what a client does about it. Every
// real torrent satisfies it by construction, which is exactly why it is safe
// to insist on.
func checkGeometry(info *metainfo.Info) error {
	if info.PieceLength < MinPieceLength || info.PieceLength > MaxPieceLength {
		return fmt.Errorf("%w: a piece length of %d is outside %d..%d",
			ErrPieceGeometry, info.PieceLength, MinPieceLength, MaxPieceLength)
	}
	if info.HasV2() && len(info.Pieces) == 0 {
		// A v2-only torrent states its hashes per file in the file tree and has
		// no flat piece list to count. The geometry is the tree's own, already
		// walked by NumPieces.
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
		// A single-file torrent states no path at all; the file IS the name, and
		// the name was already checked as a segment above only when it is a
		// folder, so check it here for the case where it is the file.
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

// windowsReservedNames are device names NTFS resolves specially regardless of
// what follows the name: "CON", "con.txt" and "CON.tar.gz" all address the
// console device rather than creating a file, matched case-insensitively
// against the component up to (not including) its first dot.
var windowsReservedNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// safeComponent is the rule for one segment of an in-torrent path.
//
// It is a whitelist of shapes rather than a blacklist of tricks, because the
// blacklist is the version that gets one entry short. A segment is usable when
// it is a name and only a name: no separator of either flavour, no traversal,
// no control character that a log line or a filesystem call would read as
// something else, not empty, no colon (an NTFS alternate-data-stream
// separator - "readme.txt:payload.exe" is one visible file, "readme.txt",
// with a second, hidden data stream attached to it; reproduced live before
// this check existed), and not a Windows reserved device name.
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

// Contained is the containment check, and it is written to be the opposite of
// the one Wave 10 shipped and had to take back.
//
// THAT ONE WAS A TAUTOLOGY: it joined a single-segment name onto a directory
// and then confirmed the result was inside that directory, which it always was,
// whatever the directory happened to be. This one cannot be, because the pieces
// it joins are attacker-supplied MULTI-segment paths out of a stranger's file
// and filepath.Join calls Clean, so "a/../../etc/x" really does resolve to a
// sibling of dir and really is caught here. The test that proves it feeds it
// exactly that and expects a refusal - a containment check whose test only ever
// passes it well-formed names is a check nobody has run.
//
// It is the SECOND gate, not the only one: Parse already refuses these paths
// when the .torrent's bytes were seen. This one exists because a MAGNET's file
// list is never seen by Parse at all - it arrives from the swarm, after the
// download library has already been handed the link - so the engine runs it on
// what the resolve came back with, before a single byte is written.
//
// dir is the folder the download lands in. rels are paths relative to it,
// forward-slashed, INCLUDING the torrent's own folder name where there is one:
// the caller joins those two, because only the caller knows whether the library
// it is talking to nests the files under the torrent name or not.
func Contained(dir string, rels []string) error {
	if dir == "" {
		return fmt.Errorf("%w: no download folder to check against", ErrUnsafePath)
	}
	base := filepath.Clean(dir)
	for _, rel := range rels {
		if rel == "" {
			return fmt.Errorf("%w: an empty file path", ErrUnsafePath)
		}
		// Rooted paths are tested by hand rather than with filepath.IsAbs,
		// because IsAbs answers per platform and this must not: "/etc/passwd" is
		// absolute on Linux and merely odd on Windows, and a check that refuses
		// it on one machine and shrugs on another is a check whose behaviour
		// depends on where the review happened to run. Neither form escapes
		// through Join, which treats both as relative - they are refused because
		// a torrent that states one is a torrent nothing should be opening.
		if rooted(rel) {
			return fmt.Errorf("%w: %q is a rooted path", ErrUnsafePath, rel)
		}
		// safeComponent's own colon refusal never sees a magnet's file list -
		// it arrives here, from the swarm, never through Parse - so this gate
		// needs the identical check or the two gates disagree. Checked against
		// the whole rel, not per-segment, because a colon is an NTFS
		// alternate-data-stream separator wherever it appears, not only in the
		// final segment - see safeComponent's own doc comment for the
		// reproduced write this closes.
		if strings.ContainsRune(rel, ':') {
			return fmt.Errorf("%w: %q contains a colon", ErrUnsafePath, rel)
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

// within is app_files.go's withinDir, and it is the same three lines on purpose:
// the containment rule this app enforces should be one rule, so that a change
// to it is a change everywhere rather than a divergence nobody notices. It is
// copied rather than imported because internal/app imports this side of the
// tree and not the other way round.
func within(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// usableTrackerSchemes is what the embedded download library can actually
// announce to. Anything else in an announce list is not a tracker this app can
// use, whatever it is.
var usableTrackerSchemes = map[string]bool{
	"http": true, "https": true, "udp": true, "ws": true, "wss": true,
}

// trackers reduces an announce list to the URLs that can be announced to.
//
// A tracker with an unusable scheme is DROPPED and counted, not fatal: torrents
// carry dead ws:// and dht:// entries all the time and refusing the file over
// one would refuse half the real world. A tracker with a control character in
// it IS fatal, because that is not a tracker somebody forgot to remove, it is a
// string built to be pasted somewhere it will be interpreted.
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

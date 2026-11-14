package torrent

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/bencode"
)

// The tests build their .torrent files with internal/bencode, the encoding
// Parse reads. Adversarial cases break one property of a valid torrent at a
// time.

const testPieceLength = 32 << 10

// testInfo is an info dictionary as a test writes it.
type testInfo struct {
	Name        string
	Length      int64
	Files       []fileInfo
	PieceLength int64
	Pieces      []byte
	Private     *bool
}

type fileInfo struct {
	Length int64
	Path   []string
}

// testMeta is a whole .torrent as a test writes it.
type testMeta struct {
	InfoBytes    []byte
	Announce     string
	AnnounceList [][]string
}

func (in testInfo) dict() map[string]any {
	d := map[string]any{"name": in.Name, "piece length": in.PieceLength, "pieces": in.Pieces}
	if in.Files == nil {
		d["length"] = in.Length
	} else {
		files := make([]any, len(in.Files))
		for i, f := range in.Files {
			path := make([]any, len(f.Path))
			for k, p := range f.Path {
				path[k] = p
			}
			files[i] = map[string]any{"length": f.Length, "path": path}
		}
		d["files"] = files
	}
	if in.Private != nil && *in.Private {
		d["private"] = 1
	}
	return d
}

// marshal bencodes a testInfo or a testMeta.
func marshal(v any) ([]byte, error) {
	switch x := v.(type) {
	case testInfo:
		return bencode.Encode(x.dict())
	case testMeta:
		var buf []byte
		buf = append(buf, 'd')
		add := func(key string, val any) error {
			k, _ := bencode.Encode(key)
			b, err := bencode.Encode(val)
			if err != nil {
				return err
			}
			buf = append(append(buf, k...), b...)
			return nil
		}
		// In key order, with the info dictionary as it was given.
		if x.Announce != "" {
			if err := add("announce", x.Announce); err != nil {
				return nil, err
			}
		}
		if len(x.AnnounceList) > 0 {
			tiers := make([]any, len(x.AnnounceList))
			for i, tier := range x.AnnounceList {
				urls := make([]any, len(tier))
				for k, u := range tier {
					urls[k] = u
				}
				tiers[i] = urls
			}
			if err := add("announce-list", tiers); err != nil {
				return nil, err
			}
		}
		k, _ := bencode.Encode("info")
		buf = append(append(buf, k...), x.InfoBytes...)
		return append(buf, 'e'), nil
	}
	return bencode.Encode(v)
}

// build bencodes an info dict into a whole .torrent.
func build(t *testing.T, info testInfo, announce string) []byte {
	t.Helper()
	ib, err := marshal(info)
	if err != nil {
		t.Fatalf("bencoding the info dict: %v", err)
	}
	b, err := marshal(testMeta{InfoBytes: ib, Announce: announce})
	if err != nil {
		t.Fatalf("bencoding the torrent: %v", err)
	}
	return b
}

// pieces is a piece-hash list of the right length for total bytes, so a
// torrent built with it satisfies the geometry check the way a real one does.
func pieces(total int64) []byte {
	n := int((total + testPieceLength - 1) / testPieceLength)
	return make([]byte, n*20)
}

// singleFile is the simplest honest torrent: one file, no directory.
func singleFile(t *testing.T, name string, size int64) []byte {
	t.Helper()
	return build(t, testInfo{
		Name:        name,
		Length:      size,
		PieceLength: testPieceLength,
		Pieces:      pieces(size),
	}, "udp://tracker.example.org:6969/announce")
}

// multiFile is a folder torrent. paths are slash-free component lists so a
// test can put whatever it likes in one.
func multiFile(t *testing.T, folder string, files []fileInfo, private bool) []byte {
	t.Helper()
	var total int64
	for _, f := range files {
		total += f.Length
	}
	info := testInfo{
		Name:        folder,
		Files:       files,
		PieceLength: testPieceLength,
		Pieces:      pieces(total),
	}
	if private {
		p := true
		info.Private = &p
	}
	return build(t, info, "http://tracker.example.org/announce")
}

func file(size int64, path ...string) fileInfo {
	return fileInfo{Length: size, Path: path}
}

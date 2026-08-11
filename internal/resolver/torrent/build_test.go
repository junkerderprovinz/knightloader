package torrent

import (
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

// The tests in this package build their own .torrent files rather than
// checking in binary fixtures, and they build them with the same library that
// reads them back. That is the point: a hand-rolled fixture proves this parser
// agrees with whoever wrote the fixture, while one produced by the reference
// implementation proves it agrees with the thing that will actually open the
// file. The adversarial cases then take a valid one and break exactly the one
// property under test, so a refusal can only be about that property.

const testPieceLength = 32 << 10

// fileInfo keeps the metainfo import out of every test file that only wants to
// list a couple of files.
type fileInfo = metainfo.FileInfo

// build bencodes an info dict into a whole .torrent.
func build(t *testing.T, info metainfo.Info, announce string) []byte {
	t.Helper()
	ib, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("bencoding the info dict: %v", err)
	}
	mi := metainfo.MetaInfo{InfoBytes: ib, Announce: announce}
	b, err := bencode.Marshal(mi)
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
	return build(t, metainfo.Info{
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
	info := metainfo.Info{
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

package torrent

import (
	"slices"
	"testing"
)

func TestEveryFileButThePaddingHasItsPlaceInTheTorrentsBytes(t *testing.T) {
	in := Info{PieceLength: 16384, Pieces: make([]byte, 20), Files: []InfoFile{
		{Path: []string{"a.mkv"}, Length: 100000},
		{Path: []string{".pad", "14688"}, Length: 14688, Pad: true},
		{Path: []string{"b.mkv"}, Length: 5000},
		{Path: []string{"c.nfo"}, Length: 10},
	}}
	if got := in.Offsets(); !slices.Equal(got, []int64{0, 114688, 119688}) {
		t.Errorf("offsets %v", got)
	}
}

func TestATorrentOfBitTorrentV2AloneStartsEveryFileOnAPieceOfItsOwn(t *testing.T) {
	in := Info{PieceLength: 16384, V2: true, Files: []InfoFile{
		{Path: []string{"a"}, Length: 20000},
		{Path: []string{"b"}, Length: 0},
		{Path: []string{"c"}, Length: 16384},
		{Path: []string{"d"}, Length: 1},
	}}
	if got := in.Offsets(); !slices.Equal(got, []int64{0, 32768, 32768, 49152}) {
		t.Errorf("offsets %v", got)
	}
}

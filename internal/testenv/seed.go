package testenv

import (
	"bytes"
	"crypto/sha1"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/bencode"
	"github.com/junkerderprovinz/knightloader/internal/torrents"
	"github.com/junkerderprovinz/knightloader/internal/torrents/torrentstest"
)

// TorrentPieceLength is the piece size of every torrent MakeTorrent builds.
const TorrentPieceLength = 16 << 10

// MakeTorrent writes files, by their paths inside the torrent and their sizes,
// into a folder named name under root, and returns the .torrent of that
// folder and its info hash. The files are listed in the order of their paths.
func MakeTorrent(t testing.TB, root, name string, files map[string]int) ([]byte, string) {
	t.Helper()
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	var data bytes.Buffer
	list := make([]any, 0, len(paths))
	for _, p := range paths {
		body := Content(p, files[p])
		full := filepath.Join(root, name, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, body, 0o644); err != nil {
			t.Fatal(err)
		}
		data.Write(body)
		parts := make([]any, 0, 4)
		for _, part := range strings.Split(p, "/") {
			parts = append(parts, part)
		}
		list = append(list, map[string]any{"length": len(body), "path": parts})
	}
	info := map[string]any{
		"name":         name,
		"piece length": TorrentPieceLength,
		"pieces":       pieceHashes(data.Bytes()),
		"files":        list,
	}
	return encodeTorrent(t, info)
}

// MakeSingleFileTorrent writes one file of size bytes as root/name and
// returns its .torrent and info hash.
func MakeSingleFileTorrent(t testing.TB, root, name string, size int) ([]byte, string) {
	t.Helper()
	body := Content(name, size)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, name), body, 0o644); err != nil {
		t.Fatal(err)
	}
	info := map[string]any{
		"name":         name,
		"piece length": TorrentPieceLength,
		"pieces":       pieceHashes(body),
		"length":       len(body),
	}
	return encodeTorrent(t, info)
}

// Content is the bytes MakeTorrent writes for the file at p.
func Content(p string, size int) []byte {
	if size == 0 {
		return []byte{}
	}
	return bytes.Repeat([]byte(p), size/len(p)+1)[:size]
}

func pieceHashes(data []byte) []byte {
	var out []byte
	for at := 0; at < len(data); at += TorrentPieceLength {
		sum := sha1.Sum(data[at:min(at+TorrentPieceLength, len(data))])
		out = append(out, sum[:]...)
	}
	return out
}

func encodeTorrent(t testing.TB, info map[string]any) ([]byte, string) {
	t.Helper()
	rawInfo, err := bencode.Encode(info)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := bencode.Encode(map[string]any{"info": info})
	if err != nil {
		t.Fatal(err)
	}
	return raw, bencode.InfoHash(rawInfo)
}

// SeedTorrent writes files, by their paths inside the torrent and their sizes,
// into a folder named name and shares that folder in the swarm of
// torrentstest, from which a torrentstest client fetches it. The magnet it
// returns carries the name.
func SeedTorrent(t *testing.T, name string, files map[string]int) (hash, magnet string) {
	t.Helper()
	root := t.TempDir()
	_, hash = MakeTorrent(t, root, name, files)
	shared := make([]torrents.File, 0, len(files))
	for p, size := range files {
		shared = append(shared, torrents.File{Path: path.Join(name, p), Size: int64(size)})
	}
	slices.SortFunc(shared, func(a, b torrents.File) int { return strings.Compare(a.Path, b.Path) })
	torrentstest.Share(hash, root, shared)
	t.Cleanup(func() { torrentstest.Unshare(hash) })
	return hash, "magnet:?xt=urn:btih:" + hash + "&dn=" + name
}

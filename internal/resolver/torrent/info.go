package torrent

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/bencode"
)

// Info is the part of a .torrent's info dictionary KnightLoader reads.
type Info struct {
	Name        string
	PieceLength int64
	// Pieces is the v1 piece hashes, 20 bytes each, and empty for a torrent
	// of BitTorrent v2 alone.
	Pieces  []byte
	Private bool
	// Dir says the files land in a folder of Name; otherwise the torrent is
	// one file of that name.
	Dir bool
	// V2 says the torrent carries a v2 file tree.
	V2 bool
	// Files is every file in the order the torrent lists them.
	Files []InfoFile
}

// InfoFile is one file of a torrent. Path is empty for a torrent of one file.
type InfoFile struct {
	Path   []string
	Length int64
	// Pad is a BEP 47 padding file, which fills a piece and is never written.
	Pad bool
}

// TotalLength is every file added up, padding included.
func (in Info) TotalLength() int64 {
	var n int64
	for _, f := range in.Files {
		n += f.Length
	}
	return n
}

// Offsets is where each file but the padding begins in the torrent's bytes,
// in the torrent's order, which is where a client's pieces place it. A
// torrent of BitTorrent v2 alone has no padding listed, yet starts each file
// on a piece of its own.
func (in Info) Offsets() []int64 {
	out := make([]int64, 0, len(in.Files))
	var at int64
	for _, f := range in.Files {
		if f.Pad {
			at += f.Length
			continue
		}
		out = append(out, at)
		at += f.Length
		if in.V2 && len(in.Pieces) == 0 && in.PieceLength > 0 {
			at = (at + in.PieceLength - 1) / in.PieceLength * in.PieceLength
		}
	}
	return out
}

// ReadInfo reads the info dictionary of the .torrent b, and returns it with
// the dictionary as it stands in b, which the info hash is taken over.
func ReadInfo(b []byte) (Info, []byte, error) {
	raw, err := bencode.Raw(b, "info")
	if err != nil {
		return Info{}, nil, err
	}
	v, err := bencode.Decode(raw)
	if err != nil {
		return Info{}, nil, err
	}
	d, ok := v.(map[string]any)
	if !ok {
		return Info{}, nil, errors.New("the info is not a dictionary")
	}
	in := Info{
		Name:        utf8OrPlain(d, "name"),
		PieceLength: num(d["piece length"]),
		Pieces:      bytesOf(d["pieces"]),
		Private:     num(d["private"]) == 1,
	}
	if tree, ok := d["file tree"].(map[string]any); ok && num(d["meta version"]) == 2 {
		in.V2 = true
		if len(in.Pieces) == 0 {
			files, err := walkTree(tree, nil, 0)
			if err != nil {
				return Info{}, nil, err
			}
			in.Files = files
			in.Dir = len(files) != 1 || len(files[0].Path) != 1 || files[0].Path[0] != in.Name
			if !in.Dir {
				in.Files[0].Path = nil
			}
			return in, raw, nil
		}
	}
	list, multi := d["files"].([]any)
	if !multi {
		if _, ok := d["length"].(int64); !ok {
			return Info{}, nil, errors.New("the info names neither a length nor files")
		}
		in.Files = []InfoFile{{Length: num(d["length"])}}
		return in, raw, nil
	}
	in.Dir = true
	in.Files = make([]InfoFile, 0, len(list))
	for i, item := range list {
		f, ok := item.(map[string]any)
		if !ok {
			return Info{}, nil, fmt.Errorf("file %d is not a dictionary", i)
		}
		key := "path"
		if _, ok := f["path.utf-8"]; ok {
			key = "path.utf-8"
		}
		parts, ok := f[key].([]any)
		if !ok {
			return Info{}, nil, fmt.Errorf("file %d has no path", i)
		}
		p := make([]string, len(parts))
		for k, part := range parts {
			s, ok := part.([]byte)
			if !ok {
				return Info{}, nil, fmt.Errorf("file %d has a path that is not text", i)
			}
			p[k] = string(s)
		}
		if _, ok := f["length"].(int64); !ok {
			return Info{}, nil, fmt.Errorf("file %d has no length", i)
		}
		in.Files = append(in.Files, InfoFile{Path: p, Length: num(f["length"]), Pad: strings.Contains(string(bytesOf(f["attr"])), "p")})
	}
	return in, raw, nil
}

// walkTree lists the files of a v2 file tree, in the order of its keys. A
// file is a dictionary under the empty key.
func walkTree(tree map[string]any, at []string, depth int) ([]InfoFile, error) {
	if depth > 32 {
		return nil, errors.New("the file tree is nested too deep")
	}
	names := make([]string, 0, len(tree))
	for k := range tree {
		names = append(names, k)
	}
	slices.Sort(names)
	var out []InfoFile
	for _, name := range names {
		node, ok := tree[name].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%q in the file tree is not a dictionary", name)
		}
		p := append(slices.Clone(at), name)
		if leaf, ok := node[""].(map[string]any); ok {
			out = append(out, InfoFile{Path: p, Length: num(leaf["length"])})
			continue
		}
		sub, err := walkTree(node, p, depth+1)
		if err != nil {
			return nil, err
		}
		out = append(out, sub...)
	}
	return out, nil
}

// utf8OrPlain is d's key.utf-8 where it has one, which some clients write
// beside a name in another encoding.
func utf8OrPlain(d map[string]any, key string) string {
	if b := bytesOf(d[key+".utf-8"]); b != nil {
		return string(b)
	}
	return string(bytesOf(d[key]))
}

func num(v any) int64 {
	n, _ := v.(int64)
	return n
}

func bytesOf(v any) []byte {
	b, _ := v.([]byte)
	return b
}

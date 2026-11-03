package mega

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Link is a public MEGA link taken apart. A file link carries the file's own
// 32-byte key. A folder link carries the folder's 16-byte key, and Node
// names a file or subfolder inside it, or nothing for the shared folder
// itself. Which of the two Node is only the folder's listing knows for sure;
// SubFolder is what the link said.
type Link struct {
	Folder    bool
	Handle    string
	Key       []byte
	Node      string
	SubFolder bool
}

// errNotMega is a link that is not one MEGA shares files or folders by.
var errNotMega = errors.New("mega: not a MEGA file or folder link")

// hosts are the domains MEGA serves its links under.
var hosts = map[string]bool{"mega.nz": true, "mega.co.nz": true}

// Parse reads every link form MEGA has handed out:
//
//	https://mega.nz/file/HANDLE#KEY
//	https://mega.nz/folder/HANDLE#KEY
//	https://mega.nz/folder/HANDLE#KEY/file/NODE
//	https://mega.nz/folder/HANDLE#KEY/folder/NODE
//	https://mega.nz/embed/HANDLE#KEY
//	https://mega.nz/#!HANDLE!KEY
//	https://mega.nz/#F!HANDLE!KEY
//	https://mega.nz/#F!HANDLE!KEY!NODE
//
// The older forms also come with mega.co.nz and an "embed" path in front of
// the fragment.
func Parse(raw string) (Link, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !hosts[strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")] {
		return Link{}, errNotMega
	}
	path := strings.Trim(u.Path, "/")
	frag := u.Fragment
	var l Link
	var key string
	switch {
	case strings.HasPrefix(path, "file/"), strings.HasPrefix(path, "embed/"):
		_, l.Handle, _ = strings.Cut(path, "/")
		key = frag
	case strings.HasPrefix(path, "folder/"):
		l.Folder = true
		l.Handle = strings.TrimPrefix(path, "folder/")
		var rest string
		key, rest, _ = strings.Cut(frag, "/")
		if rest != "" {
			kind, node, _ := strings.Cut(rest, "/")
			if kind != "file" && kind != "folder" {
				return Link{}, errNotMega
			}
			l.Node, l.SubFolder = node, kind == "folder"
		}
	case strings.HasPrefix(frag, "F!"):
		l.Folder = true
		parts := strings.Split(strings.TrimPrefix(frag, "F!"), "!")
		if len(parts) < 2 || len(parts) > 3 {
			return Link{}, errNotMega
		}
		l.Handle, key = parts[0], parts[1]
		// The old form named only subfolders this way.
		if len(parts) == 3 {
			l.Node, l.SubFolder = parts[2], true
		}
	case strings.HasPrefix(frag, "!"):
		parts := strings.Split(strings.TrimPrefix(frag, "!"), "!")
		if len(parts) != 2 {
			return Link{}, errNotMega
		}
		l.Handle, key = parts[0], parts[1]
	default:
		return Link{}, errNotMega
	}
	if !validHandle(l.Handle) || (l.Node != "" && !validHandle(l.Node)) {
		return Link{}, errNotMega
	}
	if key == "" {
		return Link{}, errors.New("mega: the link has no key after the #, and the file cannot be decrypted without it")
	}
	if l.Key, err = decode64(key); err != nil {
		return Link{}, errNotMega
	}
	want := 32
	if l.Folder {
		want = 16
	}
	if len(l.Key) != want {
		return Link{}, fmt.Errorf("mega: the key in the link is %d bytes long, not %d; it was probably cut short", len(l.Key), want)
	}
	return l, nil
}

// String is the link in the form MEGA hands out today.
func (l Link) String() string {
	k := base64.RawURLEncoding.EncodeToString(l.Key)
	if !l.Folder {
		return "https://mega.nz/file/" + l.Handle + "#" + k
	}
	s := "https://mega.nz/folder/" + l.Handle + "#" + k
	switch {
	case l.Node != "" && l.SubFolder:
		s += "/folder/" + l.Node
	case l.Node != "":
		s += "/file/" + l.Node
	}
	return s
}

// fileIn is the link to one file inside the folder l points at.
func (l Link) fileIn(node string) Link {
	l.Node, l.SubFolder = node, false
	return l
}

// validHandle accepts MEGA's 8-character node handles, and anything else of
// the same alphabet, since a handle's length is MEGA's business.
func validHandle(h string) bool {
	if h == "" || len(h) > 64 {
		return false
	}
	for _, r := range h {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// decode64 reads MEGA's base64, which is URL-safe and unpadded, and takes the
// standard alphabet with padding as well.
func decode64(s string) ([]byte, error) {
	s = strings.TrimRight(s, "=")
	s = strings.NewReplacer("+", "-", "/", "_").Replace(s)
	return base64.RawURLEncoding.DecodeString(s)
}

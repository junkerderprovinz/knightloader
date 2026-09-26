package engine

import (
	"bytes"
	"io"
	"os"
)

// errPlaylist is the failure a job with Job.PassOnPlaylists reports for a
// stream playlist. It only stays on the task when no backend after this one
// takes the link.
const errPlaylist = "the link is a stream playlist that lists the video's segments"

// playlistHead is how much of a file streamPlaylist reads. A DASH manifest can
// open with an XML declaration and a comment before its root element.
const playlistHead = 512

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// streamPlaylist reports whether the file at path is an HLS or DASH playlist,
// judged by how it begins.
func streamPlaylist(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, playlistHead)
	n, _ := io.ReadFull(f, head)
	head = bytes.TrimLeft(bytes.TrimPrefix(head[:n], utf8BOM), space)
	if bytes.HasPrefix(head, []byte("#EXTM3U")) {
		return true
	}
	return rootElement(head) == "MPD"
}

const space = " \t\r\n"

// rootElement is the name of the first element in an XML document's head,
// past its declaration and comments, or "" when head is not XML.
func rootElement(head []byte) string {
	for {
		head = bytes.TrimLeft(head, space)
		var end []byte
		switch {
		case bytes.HasPrefix(head, []byte("<?")):
			end = []byte("?>")
		case bytes.HasPrefix(head, []byte("<!--")):
			end = []byte("-->")
		case bytes.HasPrefix(head, []byte("<")):
			name := head[1:]
			if i := bytes.IndexAny(name, space+"/>"); i >= 0 {
				return string(name[:i])
			}
			return ""
		default:
			return ""
		}
		i := bytes.Index(head, end)
		if i < 0 {
			return ""
		}
		head = head[i+len(end):]
	}
}

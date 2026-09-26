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

// errPage is the failure a job with Job.RefusePages reports for a web page.
const errPage = "the link opens a web page instead of a file, and no backend here found a video on it"

// headSize is how much of a file fileHead reads. A DASH manifest can open
// with an XML declaration and a comment before its root element.
const headSize = 512

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// fileHead returns how the file at path begins, past a byte order mark and
// white space, or nothing when it cannot be read.
func fileHead(path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	head := make([]byte, headSize)
	n, _ := io.ReadFull(f, head)
	return bytes.TrimLeft(bytes.TrimPrefix(head[:n], utf8BOM), space)
}

// streamPlaylist reports whether a file that begins with head is an HLS or
// DASH playlist.
func streamPlaylist(head []byte) bool {
	return bytes.HasPrefix(head, []byte("#EXTM3U")) || rootElement(head) == "MPD"
}

// webPage reports whether a file that begins with head is an HTML document:
// one that declares itself HTML or opens with an html element.
func webPage(head []byte) bool {
	h := bytes.ToLower(head)
	return bytes.HasPrefix(h, []byte("<!doctype html")) || rootElement(h) == "html"
}

const space = " \t\r\n"

// rootElement is the name of the first element in an XML document's head,
// past its declaration, comments and doctype, or "" when head is not XML.
func rootElement(head []byte) string {
	for {
		head = bytes.TrimLeft(head, space)
		var end []byte
		switch {
		case bytes.HasPrefix(head, []byte("<?")):
			end = []byte("?>")
		case bytes.HasPrefix(head, []byte("<!--")):
			end = []byte("-->")
		case bytes.HasPrefix(head, []byte("<!")):
			end = []byte(">")
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

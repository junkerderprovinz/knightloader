package core

import (
	"path"
	"path/filepath"
	"strings"
)

// mediaKinds is the audio and video half of inlineTypes in
// internal/api/routes_files.go, whose test keeps the two in step.
var mediaKinds = map[string]string{
	".mp3":  "audio",
	".m4a":  "audio",
	".wav":  "audio",
	".flac": "audio",
	".ogg":  "audio",
	".mp4":  "video",
	".m4v":  "video",
	".webm": "video",
	".mkv":  "video",
	".mov":  "video",
	".avi":  "video",
}

// MediaKind is "audio" or "video" for a file the file route serves as either,
// and empty for every other name.
func MediaKind(name string) string {
	return mediaKinds[strings.ToLower(filepath.Ext(name))]
}

// PlayFile is the file of a torrent that Play opens: the largest selected one
// that is audio or video, or -1 when there is none.
func PlayFile(files []TorrentFile) int {
	at := -1
	for i, f := range files {
		if !f.Selected || MediaKind(path.Base(f.Path)) == "" {
			continue
		}
		if at < 0 || f.Size > files[at].Size {
			at = i
		}
	}
	return at
}

// TorrentMedia is the kind of file Play opens in a torrent of several files,
// empty for one with no selected audio or video and for a torrent of one file,
// whose task is named after it.
func TorrentMedia(files []TorrentFile) string {
	if len(files) < 2 {
		return ""
	}
	if i := PlayFile(files); i >= 0 {
		return MediaKind(path.Base(files[i].Path))
	}
	return ""
}

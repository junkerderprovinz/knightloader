package core

import "testing"

func TestTorrentMediaIsTheKindOfTheFilePlayOpens(t *testing.T) {
	cases := []struct {
		name  string
		files []TorrentFile
		want  string
	}{
		{"an album", []TorrentFile{
			{Path: "Album/01.flac", Size: 30, Selected: true},
			{Path: "Album/cover.jpg", Size: 90, Selected: true},
		}, "audio"},
		{"a film with an unselected soundtrack", []TorrentFile{
			{Path: "Film/film.mkv", Size: 50, Selected: true},
			{Path: "Film/score.mp3", Size: 90},
		}, "video"},
		{"no audio or video", []TorrentFile{
			{Path: "Pack/setup.exe", Size: 50, Selected: true},
			{Path: "Pack/readme.txt", Size: 1, Selected: true},
		}, ""},
		{"one file", []TorrentFile{{Path: "film.mkv", Size: 50, Selected: true}}, ""},
	}
	for _, c := range cases {
		if got := TorrentMedia(c.files); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

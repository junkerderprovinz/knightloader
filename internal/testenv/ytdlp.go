package testenv

import (
	"os"
	"path/filepath"
)

// NoYtdlp points KL_YTDLP at a file that does not exist, so the test binary
// finds no yt-dlp whether or not the machine has one. Call it from TestMain.
//
// Every App looks for yt-dlp when it wires its backends, and starts it with
// --version when one is found. With KL_YTDLP empty that means searching PATH,
// and on Windows each search tries a dozen extensions in every folder on it,
// which is most of what building an App costs. An installed yt-dlp would also
// take media links away from the debrid services, which CI never sees. A test
// about how yt-dlp is found sets KL_YTDLP itself.
func NoYtdlp() {
	os.Setenv("KL_YTDLP", filepath.Join(os.TempDir(), "knightloader-tests", "no-yt-dlp"))
}

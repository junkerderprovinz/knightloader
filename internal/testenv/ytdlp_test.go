package testenv

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestNoYtdlpNamesAFileThatIsNotThere(t *testing.T) {
	t.Setenv("KL_YTDLP", "")
	NoYtdlp()

	bin := os.Getenv("KL_YTDLP")
	if !filepath.IsAbs(bin) {
		t.Fatalf("KL_YTDLP = %q, want an absolute path so nothing searches PATH for it", bin)
	}
	if _, err := os.Stat(bin); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("KL_YTDLP = %q, want a file that does not exist, got %v", bin, err)
	}
}

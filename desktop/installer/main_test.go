package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/junkerderprovinz/pommelporter"

	"github.com/junkerderprovinz/knightloader/internal/checksum"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

func TestTheCauseOfAFailedFetchReachesTheWindow(t *testing.T) {
	refused := fmt.Errorf("ghrelease: github answered %w", &httpx.StatusError{Status: "404 Not Found"})
	tampered := fmt.Errorf("downloading: %w", &checksum.MismatchError{Msg: "the archive does not match"})
	other := errors.New("the archive holds no bin/ffmpeg")

	if err := marked(refused); !errors.As(err, new(*pommelporter.ServerError)) || err.Error() != refused.Error() {
		t.Errorf("a refusal came out as %T %q", err, err)
	}
	if err := marked(tampered); !errors.As(err, new(*pommelporter.ChecksumError)) || err.Error() != tampered.Error() {
		t.Errorf("a checksum mismatch came out as %T %q", err, err)
	}
	if err := marked(other); err != other {
		t.Errorf("another error came out as %T %q", err, err)
	}
	if marked(nil) != nil {
		t.Error("a fetch that worked came out as an error")
	}
}

func TestTheInstallerFetchesFFmpegJavaAndJDownloader(t *testing.T) {
	var ids []string
	for _, c := range components(t.TempDir()) {
		if c.Target == "" || c.Fetch == nil || c.Name["en"] == "" {
			t.Errorf("%s is missing its target, its fetch or its name", c.ID)
		}
		ids = append(ids, c.ID)
	}
	if fmt.Sprint(ids) != "[ffmpeg java jdownloader]" {
		t.Fatalf("components %v", ids)
	}
}

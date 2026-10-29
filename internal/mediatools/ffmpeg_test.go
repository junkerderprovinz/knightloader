package mediatools

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ulikunitz/xz"

	"github.com/junkerderprovinz/knightloader/internal/checksum"
	"github.com/junkerderprovinz/knightloader/internal/ghrelease"
)

// ffmpegArchive packs the test binary as bin/ffmpeg and bin/ffprobe, under the
// top folder the real archives have, plus a file that must not be unpacked.
func ffmpegArchive(t *testing.T, name string, bins ...string) []byte {
	t.Helper()
	self := selfBytes(t)
	files := map[string][]byte{"ffmpeg-master-latest-lgpl/LICENSE.txt": []byte("LGPL")}
	for _, b := range bins {
		files["ffmpeg-master-latest-lgpl/bin/"+exeName(b)] = self
	}

	var buf bytes.Buffer
	if strings.HasSuffix(name, ".zip") {
		w := zip.NewWriter(&buf)
		for n, body := range files {
			fw, err := w.Create(n)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fw.Write(body); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	xw, err := xz.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(xw)
	for n, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := xw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func ffmpegRelease(name string, body []byte) (ghrelease.Release, []checksum.Sum) {
	sum := sha256.Sum256(body)
	rel := ghrelease.Release{Tag: "latest", Assets: []ghrelease.Asset{{Name: name, URL: "https://github.com/" + name, Size: int64(len(body))}}}
	return rel, []checksum.Sum{{Name: name, Kind: checksum.SHA256, Hex: hex.EncodeToString(sum[:])}}
}

func TestFFmpegIsFetchedFromEitherArchiveFormat(t *testing.T) {
	for _, name := range []string{"ffmpeg-master-latest-win64-lgpl.zip", "ffmpeg-master-latest-linux64-lgpl.tar.xz"} {
		t.Run(name, func(t *testing.T) {
			isolate(t)
			t.Setenv(fakeEnv, "ffversion")
			dir := t.TempDir()
			body := ffmpegArchive(t, name, "ffmpeg", "ffprobe")
			rel, sums := ffmpegRelease(name, body)

			rec, err := installFFmpeg(context.Background(), dir, name, rel, sums, writes(body))
			if err != nil {
				t.Fatal(err)
			}
			if rec.Version != "N-128142-g28fda48e0b-20261028" || rec.Asset != name {
				t.Errorf("record %+v", rec)
			}
			for _, p := range []string{FFmpegPath(dir), FFprobePath(dir)} {
				if _, err := os.Stat(p); err != nil {
					t.Errorf("%s: %v", p, err)
				}
			}
			if _, err := os.Stat(filepath.Join(ToolsDir(dir), "LICENSE.txt")); err == nil {
				t.Error("a file other than the two programs was unpacked")
			}
			if saved, err := LoadFFmpegRecord(dir); err != nil || saved == nil || saved.SHA256 != sums[0].Hex {
				t.Errorf("saved record %+v (%v)", saved, err)
			}
		})
	}
}

func TestAnFFmpegArchiveThatFailsItsChecksumInstallsNothing(t *testing.T) {
	isolate(t)
	t.Setenv(fakeEnv, "ffversion")
	dir := t.TempDir()
	name := "ffmpeg-master-latest-win64-lgpl.zip"
	body := ffmpegArchive(t, name, "ffmpeg", "ffprobe")
	rel, sums := ffmpegRelease(name, body)
	sums[0].Hex = strings.Repeat("0", 64)

	if _, err := installFFmpeg(context.Background(), dir, name, rel, sums, writes(body)); err == nil {
		t.Fatal("a download that does not match its checksum was accepted")
	}
	if _, err := os.Stat(FFmpegPath(dir)); err == nil {
		t.Error("ffmpeg was put in place anyway")
	}
}

func TestAnFFmpegArchiveWithoutFFprobeInstallsNothing(t *testing.T) {
	isolate(t)
	t.Setenv(fakeEnv, "ffversion")
	dir := t.TempDir()
	name := "ffmpeg-master-latest-win64-lgpl.zip"
	body := ffmpegArchive(t, name, "ffmpeg")
	rel, sums := ffmpegRelease(name, body)

	if _, err := installFFmpeg(context.Background(), dir, name, rel, sums, writes(body)); err == nil {
		t.Fatal("an archive without ffprobe was accepted")
	}
	if _, err := os.Stat(FFmpegPath(dir)); err == nil {
		t.Error("ffmpeg was put in place without its ffprobe")
	}
}

func TestAnFFmpegThatDoesNotRunInstallsNothing(t *testing.T) {
	isolate(t)
	t.Setenv(fakeEnv, "fail")
	dir := t.TempDir()
	name := "ffmpeg-master-latest-win64-lgpl.zip"
	body := ffmpegArchive(t, name, "ffmpeg", "ffprobe")
	rel, sums := ffmpegRelease(name, body)

	if _, err := installFFmpeg(context.Background(), dir, name, rel, sums, writes(body)); err == nil {
		t.Fatal("an ffmpeg that does not start was accepted")
	}
	if _, err := os.Stat(FFmpegPath(dir)); err == nil {
		t.Error("ffmpeg was put in place anyway")
	}
}

func TestResolveFFmpegPrecedence(t *testing.T) {
	t.Run("nothing anywhere names the bare programs", func(t *testing.T) {
		isolate(t)
		ff := ResolveFFmpeg(t.TempDir())
		if ff.FFmpeg != "ffmpeg" || ff.FFmpegSource != SourceNone || ff.FFprobeSource != SourceNone {
			t.Fatalf("got %+v", ff)
		}
	})

	t.Run("a fetched copy stands in where PATH has none", func(t *testing.T) {
		isolate(t)
		dir := t.TempDir()
		fakeYtdlp(t, FFmpegPath(dir))
		fakeYtdlp(t, FFprobePath(dir))
		ff := ResolveFFmpeg(dir)
		if ff.FFmpeg != FFmpegPath(dir) || ff.FFmpegSource != SourceManaged || ff.FFprobeSource != SourceManaged {
			t.Fatalf("got %+v", ff)
		}
	})

	t.Run("PATH outranks a fetched copy, each program on its own", func(t *testing.T) {
		isolate(t)
		dir := t.TempDir()
		fakeYtdlp(t, FFmpegPath(dir))
		fakeYtdlp(t, FFprobePath(dir))
		bin := t.TempDir()
		fakeYtdlp(t, filepath.Join(bin, exeName("ffmpeg")))
		t.Setenv("PATH", bin)
		ff := ResolveFFmpeg(dir)
		if ff.FFmpegSource != SourcePath || filepath.Dir(ff.FFmpeg) != bin {
			t.Errorf("ffmpeg %q from %q, want the one on PATH", ff.FFmpeg, ff.FFmpegSource)
		}
		if ff.FFprobe != FFprobePath(dir) || ff.FFprobeSource != SourceManaged {
			t.Errorf("ffprobe %q from %q, want the fetched one", ff.FFprobe, ff.FFprobeSource)
		}
	})
}

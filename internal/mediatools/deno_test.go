package mediatools

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/checksum"
	"github.com/junkerderprovinz/knightloader/internal/ghrelease"
)

func TestEachPlatformGetsItsOwnDenoBuild(t *testing.T) {
	cases := []struct{ goos, goarch, want string }{
		{"windows", "amd64", "deno-x86_64-pc-windows-msvc.zip"},
		{"windows", "arm64", "deno-aarch64-pc-windows-msvc.zip"},
		{"linux", "amd64", "deno-x86_64-unknown-linux-gnu.zip"},
		{"linux", "arm64", "deno-aarch64-unknown-linux-gnu.zip"},
		{"darwin", "amd64", "deno-x86_64-apple-darwin.zip"},
		{"darwin", "arm64", "deno-aarch64-apple-darwin.zip"},
		{"linux", "arm", ""},
		{"freebsd", "amd64", ""},
	}
	for _, c := range cases {
		if got := denoAssetFor(c.goos, c.goarch); got != c.want {
			t.Errorf("%s/%s: %q, want %q", c.goos, c.goarch, got, c.want)
		}
	}
}

func TestDenoChecksumsAreReadInBothShapesItPublishes(t *testing.T) {
	const hexsum = "cba84b26e0df5ae7f74fc66b86a95b866872a86292ebd84fa1e959927edd93bf"
	powershell := "\r\nAlgorithm : SHA256\r\nHash      : " + strings.ToUpper(hexsum) +
		"\r\nPath      : C:\\a\\deno\\deno\\target\\release\\deno-x86_64-pc-windows-msvc.zip\r\n\r\n"
	cases := []struct {
		name, file, asset string
		ok                bool
	}{
		{"sha256sum on Linux and macOS", hexsum + "  deno-x86_64-unknown-linux-gnu.zip\n", "deno-x86_64-unknown-linux-gnu.zip", true},
		{"Get-FileHash on Windows", powershell, "deno-x86_64-pc-windows-msvc.zip", true},
		{"a sum for another file", hexsum + "  deno-aarch64-apple-darwin.zip\n", "deno-x86_64-apple-darwin.zip", false},
		{"Get-FileHash for another file", powershell, "deno-aarch64-pc-windows-msvc.zip", false},
		{"a digest other than SHA-256", strings.Replace(powershell, "SHA256", "MD5", 1), "deno-x86_64-pc-windows-msvc.zip", false},
		{"no digest at all", "Algorithm : SHA256\r\nPath : deno-x86_64-pc-windows-msvc.zip\r\n", "deno-x86_64-pc-windows-msvc.zip", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sum, err := denoSum([]byte(c.file), c.asset)
			if !c.ok {
				if err == nil {
					t.Fatalf("accepted %+v", sum)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if sum.Kind != checksum.SHA256 || sum.Hex != hexsum || sum.Name != c.asset {
				t.Errorf("read %+v", sum)
			}
		})
	}
}

// denoArchive packs the test binary as the deno at the archive's root, which
// is the shape of every release zip.
func denoArchive(t *testing.T, entries ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, n := range entries {
		fw, err := w.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(selfBytes(t)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func denoRelease(tag, name string, body []byte) (ghrelease.Release, checksum.Sum) {
	sum := sha256.Sum256(body)
	rel := ghrelease.Release{Tag: tag, Assets: []ghrelease.Asset{{Name: name, URL: "https://github.com/" + name, Size: int64(len(body))}}}
	return rel, checksum.Sum{Name: name, Kind: checksum.SHA256, Hex: hex.EncodeToString(sum[:])}
}

func TestDenoIsFetchedVerifiedAndRecorded(t *testing.T) {
	isolate(t)
	t.Setenv(fakeEnv, "denoversion")
	dir := t.TempDir()
	const name = "deno-x86_64-unknown-linux-gnu.zip"
	body := denoArchive(t, exeName("deno"))
	rel, sum := denoRelease("v2.9.8", name, body)

	rec, err := installDeno(context.Background(), dir, name, rel, sum, writes(body))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Version != "2.9.8" || rec.Tag != "v2.9.8" || rec.Asset != name || rec.SHA256 != sum.Hex {
		t.Errorf("record %+v", rec)
	}
	if _, err := os.Stat(DenoPath(dir)); err != nil {
		t.Fatal(err)
	}
	if saved, err := LoadDenoRecord(dir); err != nil || saved == nil || saved.Version != "2.9.8" {
		t.Errorf("saved record %+v (%v)", saved, err)
	}
	if path, source := ResolveDeno(dir); path != DenoPath(dir) || source != SourceManaged {
		t.Errorf("resolved %q from %q, want the fetched copy", path, source)
	}
}

func TestADenoThatFailsAnyCheckInstallsNothing(t *testing.T) {
	const name = "deno-x86_64-pc-windows-msvc.zip"
	cases := []struct {
		name     string
		behave   string
		tag      string
		entries  []string
		tampered bool
	}{
		{"a download that does not match its checksum", "denoversion", "v2.9.8", []string{exeName("deno")}, true},
		{"an archive without deno at its root", "denoversion", "v2.9.8", []string{"bin/" + exeName("deno")}, false},
		{"a deno that does not start", "fail", "v2.9.8", []string{exeName("deno")}, false},
		{"a deno that names another release", "denoversion", "v2.9.9", []string{exeName("deno")}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isolate(t)
			t.Setenv(fakeEnv, c.behave)
			dir := t.TempDir()
			body := denoArchive(t, c.entries...)
			rel, sum := denoRelease(c.tag, name, body)
			if c.tampered {
				sum.Hex = strings.Repeat("0", 64)
			}
			if _, err := installDeno(context.Background(), dir, name, rel, sum, writes(body)); err == nil {
				t.Fatal("it was accepted")
			}
			if _, err := os.Stat(DenoPath(dir)); err == nil {
				t.Error("deno was put in place anyway")
			}
			if rec, _ := LoadDenoRecord(dir); rec != nil {
				t.Errorf("a record was written: %+v", rec)
			}
		})
	}
}

func TestResolveDenoPrecedence(t *testing.T) {
	cases := []struct {
		name                   string
		managed, env, fromPath bool
		want                   Source
	}{
		{"nothing anywhere", false, false, false, SourceNone},
		{"PATH alone", false, false, true, SourcePath},
		{"KL_DENO outranks PATH", false, true, true, SourceEnv},
		{"the fetched copy outranks KL_DENO and PATH", true, true, true, SourceManaged},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isolate(t)
			dir := t.TempDir()
			want := "deno"
			if c.fromPath {
				bin := t.TempDir()
				fakeYtdlp(t, filepath.Join(bin, exeName("deno")))
				t.Setenv("PATH", bin)
				want = filepath.Join(bin, exeName("deno"))
			}
			if c.env {
				want = filepath.Join(t.TempDir(), exeName("deno"))
				t.Setenv("KL_DENO", want)
			}
			if c.managed {
				fakeYtdlp(t, DenoPath(dir))
				want = DenoPath(dir)
			}
			path, source := ResolveDeno(dir)
			if source != c.want || path != want {
				t.Errorf("got %q from %q, want %q from %q", path, source, want, c.want)
			}
			handed := want
			if c.want == SourceNone {
				handed = ""
			}
			if got := DenoForYtdlp(dir); got != handed {
				t.Errorf("yt-dlp is handed %q, want %q", got, handed)
			}
		})
	}
}

func TestTheProberReportsDeno(t *testing.T) {
	isolate(t)
	t.Setenv(fakeEnv, "denoversion")
	dir := t.TempDir()
	fakeYtdlp(t, DenoPath(dir))
	st := NewProber(dir).Read()
	if !st.Deno.Found || st.Deno.Version != "2.9.8" || st.Deno.Source != SourceManaged || st.Deno.Path != DenoPath(dir) {
		t.Errorf("deno %+v", st.Deno)
	}
}

func TestTheProberSaysWhyThereIsNoDeno(t *testing.T) {
	isolate(t)
	st := NewProber(t.TempDir()).Read()
	if st.Deno.Found || st.Deno.Source != SourceNone {
		t.Fatalf("found a deno on a machine with none: %+v", st.Deno)
	}
	if st.Deno.Detail == "" {
		t.Error("a missing deno is reported with no reason at all")
	}
}

func TestTheJSRuntimesLineIsRead(t *testing.T) {
	const header = "[debug] yt-dlp version stable@2026.10.26 from yt-dlp/yt-dlp [c91f8de0b] (win_exe)\r\n" +
		"[debug] Optional libraries: certifi-2026.10.23, yt_dlp_ejs-0.8.0\r\n"
	cases := []struct {
		name, out, want string
		ok              bool
	}{
		{"none found", header + "[debug] JS runtimes: none\r\n[debug] Proxy map: {}\r\n", "none", true},
		{"deno handed over", header + "[debug] JS runtimes: deno-2.9.8\r\n", "deno-2.9.8", true},
		{"a yt-dlp too old to know the line", header + "[debug] Proxy map: {}\r\n", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := jsRuntimesLine(c.out)
			if got != c.want || ok != c.ok {
				t.Errorf("read (%q, %v), want (%q, %v)", got, ok, c.want, c.ok)
			}
		})
	}
}

func TestYtdlpIsAskedForItsJSRuntimesWithTheDenoItIsHanded(t *testing.T) {
	isolate(t)
	t.Setenv(fakeEnv, "jsruntimes")
	ytdlp := filepath.Join(t.TempDir(), binaryName())
	fakeYtdlp(t, ytdlp)

	cases := []struct{ name, deno, want string }{
		{"without a deno", "", "none"},
		{"with one", filepath.Join(t.TempDir(), exeName("deno")), "deno-2.9.8"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := JSRuntimes(context.Background(), ytdlp, c.deno)
			if !ok || got != c.want {
				t.Errorf("got (%q, %v), want %q", got, ok, c.want)
			}
		})
	}
}

func TestTheProberSaysWhetherYtdlpHasAJSRuntime(t *testing.T) {
	cases := []struct {
		name string
		deno bool
		want string
	}{
		{"no deno anywhere", false, "none"},
		{"a fetched deno", true, "deno-2.9.8"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isolate(t)
			t.Setenv(fakeEnv, "jsruntimes")
			dir := t.TempDir()
			ytdlp := filepath.Join(t.TempDir(), binaryName())
			fakeYtdlp(t, ytdlp)
			t.Setenv("KL_YTDLP", ytdlp)
			if c.deno {
				fakeYtdlp(t, DenoPath(dir))
			}
			if got := NewProber(dir).Read().JSRuntimes; got != c.want {
				t.Errorf("JS runtimes %q, want %q", got, c.want)
			}
		})
	}
}

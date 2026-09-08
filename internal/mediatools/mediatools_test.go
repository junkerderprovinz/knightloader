package mediatools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/checksum"
	"github.com/junkerderprovinz/knightloader/internal/ghrelease"
)

// ---------------------------------------------------------------------------
// A yt-dlp that is not yt-dlp.
//
// Everything in this package that matters spawns a program and reads what it
// prints, so the tests need a real executable that behaves like yt-dlp in a way
// they control. A shell script is not an option (Windows will not exec one) and
// building a helper with `go build` costs seconds per test.
//
// So: the test binary is its own fake. TestMain checks one environment
// variable, and when it is set, behaves as the program named by it instead of
// running any tests. Copying THIS binary to <tools>/yt-dlp makes the copy do
// exactly the same thing, because it inherits the variable from the process
// that spawned it.
// ---------------------------------------------------------------------------

const fakeEnv = "KL_MEDIATOOLS_FAKE_BEHAVIOUR"

func TestMain(m *testing.M) {
	if spec := os.Getenv(fakeEnv); spec != "" {
		switch {
		case strings.HasPrefix(spec, "print:"):
			fmt.Println(strings.TrimPrefix(spec, "print:"))
			os.Exit(0)
		case spec == "fail":
			fmt.Fprintln(os.Stderr, "fake yt-dlp: this build is broken")
			os.Exit(1)
		case spec == "silent":
			os.Exit(0)
		}
		os.Exit(2)
	}
	os.Exit(m.Run())
}

// fakeYtdlp copies the test binary to path and makes it executable.
func fakeYtdlp(t *testing.T, path string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	b, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("read the test binary: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o755); err != nil {
		t.Fatal(err)
	}
}

// isolate takes the machine's own yt-dlp out of the picture, so a developer who
// happens to have one installed does not get different results from CI.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("KL_YTDLP", "")
	t.Setenv("PATH", t.TempDir())
}

// ---------------------------------------------------------------------------
// Version ordering
// ---------------------------------------------------------------------------

// The comparison is a separate function from internal/update's isNewer for one
// specific reason, and this is it: yt-dlp publishes a fourth segment for a
// same-day rerelease, update.go's SplitN(v, ".", 3) cannot read it, and its
// caller turns "could not read" into "you are current". The one answer this
// must never give for a version it could not order is "same".
func TestVersionComparisonNeverSaysSameWhenItCannotTell(t *testing.T) {
	cases := []struct {
		latest, current, want string
	}{
		// The case the whole function exists for: a hotfix published hours
		// after the release somebody already has.
		{"2026.08.11.1", "2026.08.11", CompareNewer},
		{"2026.08.11", "2026.08.11.1", CompareOlder},

		{"2026.08.11", "2026.08.11", CompareSame},
		{"2026.08.11", "2026.07.30", CompareNewer},
		{"2026.07.30", "2026.08.11", CompareOlder},
		// Leading zeros are what a date-based version is made of.
		{"2026.08.01", "2026.8.1", CompareSame},
		// Missing trailing segments are zero, the arithmetic every dotted
		// scheme uses.
		{"2026.08", "2026.08.0", CompareSame},

		// Genuinely unorderable, and it has to say so rather than pick.
		{"2026.08.11", "2026.08.11.dev0", CompareUnknown},
		{"nightly", "2026.08.11", CompareUnknown},
		{"2026.08.11", "", CompareUnknown},
		{"", "2026.08.11", CompareUnknown},
	}
	for _, c := range cases {
		if got := CompareVersions(c.latest, c.current); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %q, want %q", c.latest, c.current, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Version strings as the two programs actually print them
// ---------------------------------------------------------------------------

func TestVersionLinesAreRead(t *testing.T) {
	if got := ffmpegVersion("ffmpeg version 6.1.2-r1 Copyright (c) 2000-2024 the FFmpeg developers"); got != "6.1.2-r1" {
		t.Errorf("Alpine's ffmpeg banner read as %q", got)
	}
	if got := ffmpegVersion("ffprobe version n7.0 Copyright (c) 2007-2024 the FFmpeg developers"); got != "n7.0" {
		t.Errorf("a git-built ffprobe banner read as %q", got)
	}
	// A banner in a shape nobody predicted keeps the whole line rather than
	// blanking the cell: an unparsed fact is still a fact in a bug report.
	if got := ffmpegVersion("some other build 1.2.3"); got != "some other build 1.2.3" {
		t.Errorf("an unexpected banner was thrown away: %q", got)
	}
	// yt-dlp prints the bare version and nothing else.
	if got := ytdlpVersion("2026.08.11"); got != "2026.08.11" {
		t.Errorf("yt-dlp's own line read as %q", got)
	}
}

// ---------------------------------------------------------------------------
// Which yt-dlp actually runs
// ---------------------------------------------------------------------------

func TestResolveYtdlpPrecedence(t *testing.T) {
	t.Run("nothing anywhere falls back to the bare name", func(t *testing.T) {
		isolate(t)
		path, source, _ := ResolveYtdlp(t.TempDir())
		if path != "yt-dlp" || source != SourceNone {
			t.Fatalf("got %q from %q, want the historical \"yt-dlp\"/none", path, source)
		}
	})

	t.Run("KL_YTDLP wins over PATH", func(t *testing.T) {
		isolate(t)
		t.Setenv("KL_YTDLP", "/somewhere/yt-dlp")
		path, source, _ := ResolveYtdlp(t.TempDir())
		if path != "/somewhere/yt-dlp" || source != SourceEnv {
			t.Fatalf("got %q from %q", path, source)
		}
	})

	t.Run("a working fetched copy outranks KL_YTDLP", func(t *testing.T) {
		// The owner's decision, and the container is why: Dockerfile pins
		// KL_YTDLP=/usr/bin/yt-dlp on every install, so the other precedence
		// would make the whole feature a silent no-op exactly where it is
		// needed.
		isolate(t)
		dir := t.TempDir()
		t.Setenv("KL_YTDLP", "/somewhere/yt-dlp")
		t.Setenv(fakeEnv, "print:2026.08.11")
		fakeYtdlp(t, BinaryPath(dir))
		if err := saveRecord(dir, ManagedRecord{Tag: "2026.08.11", Version: "2026.08.11"}); err != nil {
			t.Fatal(err)
		}
		path, source, detail := ResolveYtdlp(dir)
		if source != SourceManaged || path != BinaryPath(dir) {
			t.Fatalf("got %q from %q (detail %q)", path, source, detail)
		}
	})

	t.Run("a recorded copy that does not start loses", func(t *testing.T) {
		// Windows Defender quarantining the file, an operator clearing
		// /data/tools by hand, a half-written download: whatever the cause,
		// yt-dlp has to keep working rather than the resolver table keeping a
		// dead path.
		isolate(t)
		dir := t.TempDir()
		t.Setenv("KL_YTDLP", "/somewhere/yt-dlp")
		t.Setenv(fakeEnv, "fail")
		fakeYtdlp(t, BinaryPath(dir))
		if err := saveRecord(dir, ManagedRecord{Tag: "2026.08.11"}); err != nil {
			t.Fatal(err)
		}
		path, source, detail := ResolveYtdlp(dir)
		if source != SourceEnv || path != "/somewhere/yt-dlp" {
			t.Fatalf("a broken fetched copy was started anyway: %q from %q", path, source)
		}
		if !strings.Contains(detail, "does not start") {
			t.Fatalf("the fallback does not say why it happened: %q", detail)
		}
	})

	t.Run("a record whose file is gone loses and says so", func(t *testing.T) {
		isolate(t)
		dir := t.TempDir()
		t.Setenv("KL_YTDLP", "/somewhere/yt-dlp")
		if err := saveRecord(dir, ManagedRecord{Tag: "2026.08.11"}); err != nil {
			t.Fatal(err)
		}
		_, source, detail := ResolveYtdlp(dir)
		if source != SourceEnv {
			t.Fatalf("source %q", source)
		}
		if !strings.Contains(detail, "not there") {
			t.Fatalf("detail %q", detail)
		}
	})
}

// ---------------------------------------------------------------------------
// The install sequence
// ---------------------------------------------------------------------------

// release builds a release whose assets are the named ones, with the digests of
// `body` - the same bytes every candidate would download.
func release(t *testing.T, tag string, body []byte, names ...string) (ghrelease.Release, []checksum.Sum) {
	t.Helper()
	sum := sha256.Sum256(body)
	hexsum := hex.EncodeToString(sum[:])
	rel := ghrelease.Release{Tag: tag}
	var sums []checksum.Sum
	for _, n := range names {
		rel.Assets = append(rel.Assets, ghrelease.Asset{Name: n, URL: "https://github.com/" + n, Size: int64(len(body))})
		sums = append(sums, checksum.Sum{Name: n, Kind: checksum.SHA256, Hex: hexsum})
	}
	return rel, sums
}

// writes is a fetcher that puts fixed bytes on disk instead of downloading.
func writes(body []byte) fetcher {
	return func(_ context.Context, _ ghrelease.Asset, dst string) error {
		return os.WriteFile(dst, body, 0o644)
	}
}

func selfBytes(t *testing.T) []byte {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// THE PROPERTY THIS FEATURE LIVES OR DIES BY: a candidate that downloads and
// verifies but does not RUN here must leave the working copy exactly as it was.
// The glibc build on the musl container is precisely this case, and it is the
// one an operator meets first.
func TestAFailedSmokeTestReplacesNothing(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	live := BinaryPath(dir)
	if err := os.MkdirAll(ToolsDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	const working = "the copy that already works"
	if err := os.WriteFile(live, []byte(working), 0o755); err != nil {
		t.Fatal(err)
	}

	body := selfBytes(t)
	rel, sums := release(t, "2026.08.11", body, "yt-dlp_linux")
	t.Setenv(fakeEnv, "fail") // downloads, verifies, and then refuses to run

	_, err := install(context.Background(), dir, []string{"yt-dlp_linux"}, rel, sums, writes(body))
	if err == nil {
		t.Fatal("a binary that does not run was installed")
	}
	// The message has to name the asset and carry the program's own failure,
	// or nobody can tell a broken build from a noexec mount.
	if !strings.Contains(err.Error(), "yt-dlp_linux") {
		t.Errorf("the failure does not name the asset it tried: %v", err)
	}
	if !strings.Contains(err.Error(), "does not run on this machine") {
		t.Errorf("the failure does not say what went wrong: %v", err)
	}
	if !strings.Contains(err.Error(), "nothing was replaced") {
		t.Errorf("the failure does not say the working copy survived: %v", err)
	}

	got, err := os.ReadFile(live)
	if err != nil || string(got) != working {
		t.Fatalf("the working copy was touched: %q (%v)", string(got), err)
	}
	if _, err := os.Stat(live + ".part"); err == nil {
		t.Error("the staging file was left behind")
	}
	if rec, _ := LoadRecord(dir); rec != nil {
		t.Error("a record was written for an install that did not happen")
	}
}

// A digest that does not match means the bytes are not the bytes the release
// published, and there is exactly one right answer: throw them away and touch
// nothing.
func TestAWrongChecksumReplacesNothing(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	body := selfBytes(t)
	rel, sums := release(t, "2026.08.11", body, "yt-dlp_linux")
	sums[0].Hex = strings.Repeat("0", 64)

	t.Setenv(fakeEnv, "print:2026.08.11")
	_, err := install(context.Background(), dir, []string{"yt-dlp_linux"}, rel, sums, writes(body))
	if err == nil {
		t.Fatal("a download that failed its checksum was installed")
	}
	if !strings.Contains(err.Error(), checksumAsset) {
		t.Errorf("the refusal does not name what it checked against: %v", err)
	}
	if _, statErr := os.Stat(BinaryPath(dir)); statErr == nil {
		t.Error("the unverified download was left in place")
	}
}

// A release with no digests at all is refused rather than installed
// unverified - internal/update takes the same line for its own downloads, for
// the same reason: a release missing its digests is broken or tampered with,
// not merely old.
func TestAReleaseWithoutDigestsIsRefused(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	body := selfBytes(t)
	rel, _ := release(t, "2026.08.11", body, "yt-dlp_linux")

	t.Setenv(fakeEnv, "print:2026.08.11")
	_, err := install(context.Background(), dir, []string{"yt-dlp_linux"}, rel, nil, writes(body))
	if err == nil {
		t.Fatal("an unverifiable download was installed")
	}
	if _, statErr := os.Stat(BinaryPath(dir)); statErr == nil {
		t.Error("the unverified download was left in place")
	}
}

// The candidate list is not decoration: on the Alpine container the glibc build
// cannot start and the zipapp is what works, so the second candidate has to be
// tried after the first one fails its smoke test - not instead of the install
// failing.
func TestTheSecondCandidateIsTriedWhenTheFirstWillNotRun(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	body := selfBytes(t)
	rel, sums := release(t, "2026.08.11", body, "yt-dlp")

	// "yt-dlp_linux" is not in this release at all, which is the same shape as
	// it being there and failing: the walk moves on either way.
	t.Setenv(fakeEnv, "print:2026.08.11")
	rec, err := install(context.Background(), dir, []string{"yt-dlp_linux", "yt-dlp"}, rel, sums, writes(body))
	if err != nil {
		t.Fatalf("the fallback candidate was not tried: %v", err)
	}
	if rec.Asset != "yt-dlp" {
		t.Fatalf("installed %q", rec.Asset)
	}
	if rec.Version != "2026.08.11" || rec.Tag != "2026.08.11" {
		t.Fatalf("the record disagrees with what was installed: %+v", rec)
	}

	// And the whole point: the resolver now starts it.
	path, source, detail := ResolveYtdlp(dir)
	if source != SourceManaged || path != BinaryPath(dir) {
		t.Fatalf("the fetched copy is not the one that would run: %q from %q (%s)", path, source, detail)
	}
}

// A binary that runs but claims to be a different version is not the release it
// came from, whatever the digest said - a mislabelled or re-uploaded asset.
func TestAVersionThatDisagreesWithTheTagIsRefused(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	body := selfBytes(t)
	rel, sums := release(t, "2026.08.11", body, "yt-dlp")

	t.Setenv(fakeEnv, "print:2019.01.01")
	_, err := install(context.Background(), dir, []string{"yt-dlp"}, rel, sums, writes(body))
	if err == nil {
		t.Fatal("a binary that identifies itself as another release was installed")
	}
	if !strings.Contains(err.Error(), "2019.01.01") || !strings.Contains(err.Error(), "2026.08.11") {
		t.Errorf("the refusal names neither version: %v", err)
	}
}

// A successful install replaces the previous copy rather than sitting beside
// it, and leaves no .old behind on a platform that can delete it.
func TestASecondInstallReplacesTheFirst(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	body := selfBytes(t)

	t.Setenv(fakeEnv, "print:2026.08.11")
	rel, sums := release(t, "2026.08.11", body, "yt-dlp")
	if _, err := install(context.Background(), dir, []string{"yt-dlp"}, rel, sums, writes(body)); err != nil {
		t.Fatal(err)
	}

	t.Setenv(fakeEnv, "print:2026.09.01")
	rel2, sums2 := release(t, "2026.09.01", body, "yt-dlp")
	rec, err := install(context.Background(), dir, []string{"yt-dlp"}, rel2, sums2, writes(body))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Tag != "2026.09.01" {
		t.Fatalf("the record still says %q", rec.Tag)
	}
	if _, err := os.Stat(BinaryPath(dir) + ".old"); err == nil && runtime.GOOS != "windows" {
		t.Error("the superseded copy was left on disk")
	}
}

// Remove is "back to the system copy": both the binary and the record go, and
// resolution falls back to what was there before.
func TestRemovePutsTheSystemCopyBack(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	t.Setenv("KL_YTDLP", "/somewhere/yt-dlp")
	body := selfBytes(t)
	t.Setenv(fakeEnv, "print:2026.08.11")
	rel, sums := release(t, "2026.08.11", body, "yt-dlp")
	if _, err := install(context.Background(), dir, []string{"yt-dlp"}, rel, sums, writes(body)); err != nil {
		t.Fatal(err)
	}

	if err := Remove(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(BinaryPath(dir)); err == nil {
		t.Error("the fetched binary is still there")
	}
	if rec, _ := LoadRecord(dir); rec != nil {
		t.Error("the record is still there")
	}
	path, source, _ := ResolveYtdlp(dir)
	if source != SourceEnv || path != "/somewhere/yt-dlp" {
		t.Fatalf("after removal, %q from %q", path, source)
	}
	// Removing again is not an error: a record with no binary, or neither, is
	// the state an operator who deleted the folder by hand leaves behind.
	if err := Remove(dir); err != nil {
		t.Fatalf("removing what is already gone failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// The probe, and the one property it must never lose
// ---------------------------------------------------------------------------

func TestProberReportsTheFetchedCopyAndWhatItShadows(t *testing.T) {
	isolate(t)
	dir := t.TempDir()

	// A "system" yt-dlp for KL_YTDLP to point at, so Shadowed has something to
	// report.
	system := filepath.Join(t.TempDir(), "system-yt-dlp")
	if runtime.GOOS == "windows" {
		system += ".exe"
	}
	fakeYtdlp(t, system)
	t.Setenv("KL_YTDLP", system)

	body := selfBytes(t)
	t.Setenv(fakeEnv, "print:2026.08.11")
	rel, sums := release(t, "2026.08.11", body, "yt-dlp")
	if _, err := install(context.Background(), dir, []string{"yt-dlp"}, rel, sums, writes(body)); err != nil {
		t.Fatal(err)
	}

	st := NewProber(dir).Read()
	if !st.Ytdlp.Found || st.Ytdlp.Source != SourceManaged || st.Ytdlp.Version != "2026.08.11" {
		t.Fatalf("yt-dlp row: %+v", st.Ytdlp)
	}
	if st.Managed == nil || st.Managed.Tag != "2026.08.11" {
		t.Fatalf("managed record: %+v", st.Managed)
	}
	// The row that keeps this feature from making the long run worse: what
	// would run if the fetched copy went away.
	if st.Shadowed == nil || st.Shadowed.Path != system {
		t.Fatalf("shadowed: %+v", st.Shadowed)
	}
}

func TestProberSaysWhyNothingWasFound(t *testing.T) {
	isolate(t)
	st := NewProber(t.TempDir()).Read()
	if st.Ytdlp.Found {
		t.Fatal("found a yt-dlp on a machine with none")
	}
	if st.Ytdlp.Detail == "" {
		t.Fatal("a missing yt-dlp is reported with no reason at all")
	}
	// Nothing is shadowing anything when nothing was fetched.
	if st.Shadowed != nil {
		t.Fatalf("shadowed: %+v", st.Shadowed)
	}
	if st.Managed != nil {
		t.Fatalf("managed: %+v", st.Managed)
	}
}

// The cache is not a nicety. GET /api/mediatools is read on every settings page
// load AND by every diagnostics bundle, and the diagnostics page has a refresh
// button. Without this, a tab left open on it is a fountain of processes.
func TestTheProbeIsCachedAndInvalidatable(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	t.Setenv(fakeEnv, "print:2026.08.11")
	fakeYtdlp(t, BinaryPath(dir))
	if err := saveRecord(dir, ManagedRecord{Tag: "2026.08.11"}); err != nil {
		t.Fatal(err)
	}

	p := NewProber(dir)
	first := p.Read()
	if first.Ytdlp.Version != "2026.08.11" {
		t.Fatalf("first read: %+v", first.Ytdlp)
	}

	// Change what the binary would print. A cached read must NOT notice.
	t.Setenv(fakeEnv, "print:2099.01.01")
	if again := p.Read(); again.Ytdlp.Version != "2026.08.11" {
		t.Fatalf("the second read re-spawned instead of using the cache: %q", again.Ytdlp.Version)
	}

	// After an install or a revert it must notice immediately, because "the
	// page tells the truth about what just happened" cannot wait a minute.
	p.Invalidate()
	if after := p.Read(); after.Ytdlp.Version != "2099.01.01" {
		t.Fatalf("Invalidate did not drop the cache: %q", after.Ytdlp.Version)
	}
}

// A file swapped underneath the prober by hand invalidates the cache on its
// own, because a stale version string is at its most misleading exactly then.
func TestTheCacheNoticesTheFileChanging(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	t.Setenv(fakeEnv, "print:2026.08.11")
	fakeYtdlp(t, BinaryPath(dir))
	if err := saveRecord(dir, ManagedRecord{Tag: "2026.08.11"}); err != nil {
		t.Fatal(err)
	}
	p := NewProber(dir)
	if v := p.Read().Ytdlp.Version; v != "2026.08.11" {
		t.Fatalf("first read: %q", v)
	}

	// Same behaviour, different bytes: the fingerprint is size and modification
	// time, so appending is enough to move both.
	f, err := os.OpenFile(BinaryPath(dir), os.O_APPEND|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	f.Close()

	t.Setenv(fakeEnv, "print:2099.01.01")
	if v := p.Read().Ytdlp.Version; v != "2099.01.01" {
		t.Fatalf("the cache did not notice the file changing: %q", v)
	}
}

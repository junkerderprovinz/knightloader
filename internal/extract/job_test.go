package extract

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/collide"
	"github.com/junkerderprovinz/knightloader/internal/extract/extracttest"
)

func gzipInto(t *testing.T, w io.Writer, body []byte) {
	t.Helper()
	zw := gzip.NewWriter(w)
	if _, err := zw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

// entry is one file inside a test archive.
type entry struct {
	name string
	body []byte
}

// zipBytes builds an archive in memory, so one can be put inside another
// without a second temporary directory.
func zipBytes(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func write(t *testing.T, path string, body []byte) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// The parts are one file, and where that file is an archive the job goes on and
// unpacks it: somebody who downloaded five ".001" parts wants the film.
func TestSplitFileIsJoinedAndThenOpened(t *testing.T) {
	dir := t.TempDir()
	whole := zipBytes(t, entry{"inside.txt", []byte("payload")})
	// Cut into three, the last one short, which is what a real split looks like.
	cuts := []int{len(whole) / 3, 2 * len(whole) / 3, len(whole)}
	from := 0
	for i, to := range cuts {
		write(t, filepath.Join(dir, "release.zip.00"+string(rune('1'+i))), whole[from:to])
		from = to
	}

	out, err := Run(context.Background(), Request{Path: filepath.Join(dir, "release.zip.001")})
	if err != nil {
		t.Fatal(err)
	}
	joined := filepath.Join(dir, "release.zip")
	got, err := os.ReadFile(joined)
	if err != nil {
		t.Fatalf("the parts were not joined: %v", err)
	}
	if !bytes.Equal(got, whole) {
		t.Fatalf("the joined file is %d bytes, want %d", len(got), len(whole))
	}
	if len(out.Joined) != 1 || out.Joined[0] != joined {
		t.Errorf("Joined = %v, want the file the parts made", out.Joined)
	}
	// Every part is a volume, so "delete the archive afterwards" reaches the
	// pieces and not only the file they were glued into.
	if len(out.Volumes) < 4 {
		t.Errorf("Volumes = %v, want the three parts and the zip they made", out.Volumes)
	}
	if body, err := os.ReadFile(filepath.Join(dir, "release", "inside.txt")); err != nil || string(body) != "payload" {
		t.Errorf("the joined archive was not unpacked: %q, %v", body, err)
	}
}

// Writing out the first half of a film as if it were the film is worse than
// refusing: it plays for twenty minutes and stops, and nothing on disk says why.
func TestASplitSetWithAHoleIsRefused(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "film.mkv.001"), []byte("first"))
	write(t, filepath.Join(dir, "film.mkv.003"), []byte("third"))

	if _, err := Run(context.Background(), Request{Path: filepath.Join(dir, "film.mkv.001")}); err == nil {
		t.Fatal("a set missing its middle part was joined anyway")
	}
	if exists(filepath.Join(dir, "film.mkv")) {
		t.Error("the half-joined file was left on the disk")
	}
}

// An archive inside an archive, which is how most releases are packed.
func TestDeepExtraction(t *testing.T) {
	dir := t.TempDir()
	inner := zipBytes(t, entry{"deep.txt", []byte("bottom")})
	write(t, filepath.Join(dir, "outer.zip"), zipBytes(t,
		entry{"notes.txt", []byte("top")},
		entry{"inner.zip", inner},
	))

	out, err := Run(context.Background(), Request{Path: filepath.Join(dir, "outer.zip")})
	if err != nil {
		t.Fatal(err)
	}
	if out.Nested != 1 {
		t.Errorf("Nested = %d, want the one archive found inside", out.Nested)
	}
	if body, err := os.ReadFile(filepath.Join(dir, "outer", "inner", "deep.txt")); err != nil || string(body) != "bottom" {
		t.Errorf("the nested archive was not unpacked: %q, %v", body, err)
	}
}

// TestDepthStopsTheDescent pins the floor. An archive that unpacks to a copy of
// itself is a disk filled in silence, and the seen-list alone only catches the
// case where the copy has the same path.
func TestDepthStopsTheDescent(t *testing.T) {
	dir := t.TempDir()
	inner := zipBytes(t, entry{"deep.txt", []byte("bottom")})
	write(t, filepath.Join(dir, "outer.zip"), zipBytes(t, entry{"inner.zip", inner}))

	out, err := Run(context.Background(), Request{Path: filepath.Join(dir, "outer.zip"), Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if out.Nested != 0 {
		t.Errorf("Nested = %d at depth 1, want the inner archive left alone", out.Nested)
	}
	if exists(filepath.Join(dir, "outer", "inner", "deep.txt")) {
		t.Error("depth 1 still descended into the nested archive")
	}
}

// A half-written extraction folder is indistinguishable from a finished one, so
// a job that is called off leaves nothing the next deep pass would walk into.
func TestACancelledJobTakesBackTheFolderItMade(t *testing.T) {
	dir := t.TempDir()
	inner := zipBytes(t, entry{"deep.txt", []byte("bottom")})
	write(t, filepath.Join(dir, "outer.zip"), zipBytes(t,
		entry{"notes.txt", []byte("top")},
		entry{"inner.zip", inner},
	))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := Run(ctx, Request{
		Path: filepath.Join(dir, "outer.zip"),
		// Opening the nested archive is reported the moment it happens, so this
		// fires with the outer archive's files already written to disk.
		OnProgress: func(p Progress) {
			if p.Depth > 0 {
				cancel()
			}
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the cancellation", err)
	}
	if exists(filepath.Join(dir, "outer")) {
		t.Error("the cancelled job left its output folder behind")
	}
	if !exists(filepath.Join(dir, "outer.zip")) {
		t.Error("the clean-up removed the archive itself")
	}
}

// A folder the job did not create is not the job's to remove, and for a single
// compressed stream that folder is the download folder.
func TestCleanUpLeavesWhatWasAlreadyThere(t *testing.T) {
	dir := t.TempDir()
	inner := zipBytes(t, entry{"deep.txt", []byte("bottom")})
	write(t, filepath.Join(dir, "outer.zip"), zipBytes(t,
		entry{"notes.txt", []byte("top")},
		entry{"inner.zip", inner},
	))
	keep := write(t, filepath.Join(dir, "outer", "mine.txt"), []byte("not yours"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := Run(ctx, Request{
		Path: filepath.Join(dir, "outer.zip"),
		OnProgress: func(p Progress) {
			if p.Depth > 0 {
				cancel()
			}
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the cancellation", err)
	}
	if body, err := os.ReadFile(keep); err != nil || string(body) != "not yours" {
		t.Errorf("a file that was there before the job started was removed: %q, %v", body, err)
	}
	if exists(filepath.Join(dir, "outer", "notes.txt")) {
		t.Error("the job's own output survived the clean-up")
	}
}

// zipWithBadEntry builds a zip whose second entry arrives in full and then
// fails its CRC, which is the last moment a copy can fail.
func zipWithBadEntry(t *testing.T, good, bad entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(good.name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(good.body); err != nil {
		t.Fatal(err)
	}
	w, err = zw.CreateRaw(&zip.FileHeader{
		Name:               bad.name,
		Method:             zip.Store,
		CRC32:              crc32.ChecksumIEEE(bad.body) ^ 1,
		CompressedSize64:   uint64(len(bad.body)),
		UncompressedSize64: uint64(len(bad.body)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(bad.body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// The folder a failed job created goes, with the empty subfolders its entries
// were written into. Left behind, it reads as an extraction that worked.
func TestAFailedJobTakesBackTheFolderItMade(t *testing.T) {
	dir := t.TempDir()
	arc := write(t, filepath.Join(dir, "release.zip"), zipWithBadEntry(t,
		entry{"sub/one.txt", []byte("fine")},
		entry{"sub/two.txt", []byte("damaged")},
	))

	if _, err := Run(context.Background(), Request{Path: arc}); err == nil {
		t.Fatal("an archive with a damaged entry unpacked")
	}
	if exists(filepath.Join(dir, "release")) {
		t.Error("the failed job left its folder behind")
	}
	if !exists(arc) {
		t.Error("the clean-up removed the archive itself")
	}
}

// A failed job's row keeps its byte count, so it has to be told the count at
// the moment of failure rather than whatever the throttle last let through.
func TestAFailedJobReportsHowFarItGot(t *testing.T) {
	dir := t.TempDir()
	good := entry{"one.bin", bytes.Repeat([]byte("x"), 64<<10)}
	bad := entry{"two.bin", bytes.Repeat([]byte("y"), 1<<10)}
	arc := write(t, filepath.Join(dir, "release.zip"), zipWithBadEntry(t, good, bad))

	var last Progress
	_, err := Run(context.Background(), Request{
		Path:       arc,
		OnProgress: func(p Progress) { last = p },
	})
	if err == nil {
		t.Fatal("an archive with a damaged entry unpacked")
	}
	if want := int64(len(good.body) + len(bad.body)); last.Bytes != want {
		t.Errorf("the last report said %d bytes, want the %d the job wrote before it failed", last.Bytes, want)
	}
}

// A job that reported only the outermost archive would show a bar that stops
// moving for the half of the work that happens inside it.
func TestProgressCountsEveryDepth(t *testing.T) {
	dir := t.TempDir()
	inner := zipBytes(t, entry{"deep.txt", bytes.Repeat([]byte("x"), 4096)})
	write(t, filepath.Join(dir, "outer.zip"), zipBytes(t,
		entry{"notes.txt", []byte("top")},
		entry{"inner.zip", inner},
	))

	var opened []string
	out, err := Run(context.Background(), Request{
		Path:       filepath.Join(dir, "outer.zip"),
		OnProgress: func(p Progress) { opened = append(opened, p.Archive) },
	})
	if err != nil {
		t.Fatal(err)
	}
	// notes.txt, inner.zip, deep.txt.
	if out.Files != 3 {
		t.Errorf("Files = %d, want 3 across both archives", out.Files)
	}
	if out.Bytes < 4096 {
		t.Errorf("Bytes = %d, want at least the nested file's own size", out.Bytes)
	}
	if !contains(opened, "outer.zip") || !contains(opened, "inner.zip") {
		t.Errorf("progress named %v, want both archives", opened)
	}
}

// A row reading "Unpacking 45%" needs to know how far through the archive in
// hand the job is, which the job-wide byte count cannot say once a nested
// archive is being unpacked.
func TestProgressMeasuresTheArchiveOpenNow(t *testing.T) {
	dir := t.TempDir()
	body := bytes.Repeat([]byte("x"), 4096)
	write(t, filepath.Join(dir, "outer.zip"), zipBytes(t,
		entry{"notes.txt", []byte("top")},
		entry{"inner.zip", zipBytes(t, entry{"deep.txt", body})},
	))

	var last Progress
	if _, err := Run(context.Background(), Request{
		Path:       filepath.Join(dir, "outer.zip"),
		OnProgress: func(p Progress) { last = p },
	}); err != nil {
		t.Fatal(err)
	}
	if last.Archive != "inner.zip" || last.Size != int64(len(body)) || last.Unpacked != last.Size {
		t.Errorf("the last report = %+v, want inner.zip complete at its own %d bytes", last, len(body))
	}
	if last.Bytes <= last.Size {
		t.Errorf("Bytes = %d, want the outer archive's files counted as well", last.Bytes)
	}
}

// A split file has no headers to ask and needs none: it joins to the sum of its
// parts.
func TestAJoinIsMeasuredAgainstItsParts(t *testing.T) {
	dir := t.TempDir()
	parts := []string{"once upon ", "a time ", "in the west"}
	var size int64
	for i, body := range parts {
		write(t, filepath.Join(dir, "notes.txt.00"+string(rune('1'+i))), []byte(body))
		size += int64(len(body))
	}

	var last Progress
	if _, err := Run(context.Background(), Request{
		Path:       filepath.Join(dir, "notes.txt.001"),
		OnProgress: func(p Progress) { last = p },
	}); err != nil {
		t.Fatal(err)
	}
	if last.Archive != "notes.txt.001" || last.Size != size || last.Unpacked != size {
		t.Errorf("the last report = %+v, want the join complete at %d bytes", last, size)
	}
}

// The rar reader meets its file headers one at a time as it reads, so the size
// of the whole set has to come from a walk over them before the first byte.
func TestARarIsMeasuredFromItsHeaders(t *testing.T) {
	arc := writeFixture(t, t.TempDir(), "film.rar", rar5Archive)

	var last Progress
	if _, err := Run(context.Background(), Request{
		Path:       arc,
		OnProgress: func(p Progress) { last = p },
	}); err != nil {
		t.Fatal(err)
	}
	if want := int64(len("hoarse")); last.Size != want || last.Unpacked != want {
		t.Errorf("the last report = %+v, want film.rar complete at %d bytes", last, want)
	}
}

// The byte tap is a single binding, so without the slot two jobs at once would
// each be told the other's progress and either could cancel the other's copy.
// Both still finish; neither hears about the other's archive.
func TestTwoJobsDoNotReadEachOther(t *testing.T) {
	dir := t.TempDir()
	names := []string{"one", "two"}
	for i, n := range names {
		write(t, filepath.Join(dir, n+".zip"), zipBytes(t,
			entry{n + ".txt", bytes.Repeat([]byte{byte('a' + i)}, 512<<10)},
		))
	}

	type run struct {
		saw map[string]bool
		err error
	}
	results := make([]run, len(names))
	done := make(chan int, len(names))
	for i, n := range names {
		go func() {
			r := run{saw: map[string]bool{}}
			var mu sync.Mutex
			_, r.err = Run(context.Background(), Request{
				Path: filepath.Join(dir, n+".zip"),
				OnProgress: func(p Progress) {
					mu.Lock()
					r.saw[p.Archive] = true
					mu.Unlock()
				},
			})
			results[i] = r
			done <- i
		}()
	}
	for range names {
		<-done
	}
	for i, n := range names {
		if results[i].err != nil {
			t.Fatalf("%s failed: %v", n, results[i].err)
		}
		if len(results[i].saw) != 1 || !results[i].saw[n+".zip"] {
			t.Errorf("the job on %s.zip was told about %v", n, results[i].saw)
		}
		if !exists(filepath.Join(dir, n, n+".txt")) {
			t.Errorf("%s.zip was not unpacked", n)
		}
	}
}

// A gzipped file unpacks beside the archive, so that job's output folder is the
// whole download folder and walking it for nested archives would unpack
// everything else in there.
func TestASingleStreamDoesNotDragInItsNeighbours(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "neighbour.zip"), zipBytes(t, entry{"theirs.txt", []byte("theirs")}))
	var gz bytes.Buffer
	gzipInto(t, &gz, []byte("mine"))
	write(t, filepath.Join(dir, "notes.txt.gz"), gz.Bytes())

	out, err := Run(context.Background(), Request{Path: filepath.Join(dir, "notes.txt.gz")})
	if err != nil {
		t.Fatal(err)
	}
	if out.Nested != 0 {
		t.Errorf("Nested = %d, want nothing: the folder is not this job's output", out.Nested)
	}
	if exists(filepath.Join(dir, "neighbour")) {
		t.Error("a job on one file unpacked an unrelated archive sitting next to it")
	}
	if body, err := os.ReadFile(filepath.Join(dir, "notes.txt")); err != nil || string(body) != "mine" {
		t.Errorf("the payload = %q, %v", body, err)
	}
}

// A list numbering its parts off a plain sort gets both families wrong: a
// spanned rar starts at ".rar" and a spanned zip ends at ".zip".
func TestVolumeRankFollowsTheReaderAndNotTheAlphabet(t *testing.T) {
	ordered := [][]string{
		{"film.part01.rar", "film.part02.rar", "film.part10.rar"},
		{"film.rar", "film.r00", "film.r01"},
		{"film.7z.001", "film.7z.002"},
		{"film.z01", "film.z02", "film.zip"},
		{"film.mkv.001", "film.mkv.002"},
	}
	for _, set := range ordered {
		for i := 1; i < len(set); i++ {
			if VolumeRank(set[i-1]) >= VolumeRank(set[i]) {
				t.Errorf("%q ranks at or after %q", set[i-1], set[i])
			}
		}
	}
}

// TestSplitStart keeps a split 7z out of the joiner: sevenzip reads that set
// itself, and gluing the parts together first makes a file it cannot open.
func TestSplitStart(t *testing.T) {
	yes := []string{"film.mkv.001", "film.mkv.000", "release.zip.001"}
	no := []string{"film.mkv.002", "set.7z.001", "set.7z.002", "film.mkv", "film.r01", "notes.01"}
	for _, n := range yes {
		if !SplitStart(n) {
			t.Errorf("SplitStart(%q) = false, want true", n)
		}
	}
	for _, n := range no {
		if SplitStart(n) {
			t.Errorf("SplitStart(%q) = true, want false", n)
		}
	}
	// And a job can be started on either kind, which is what the app asks.
	if !Startable("film.mkv.001") || !Startable("film.zip") || Startable("film.mkv") {
		t.Error("Startable disagrees with Supported plus SplitStart")
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

// Overwrite unpacks into a folder that is already there, and another package's
// archives in it are neither this job's to open nor its to dispose of.
func TestAReusedFolderKeepsTheArchivesItAlreadyHeld(t *testing.T) {
	dir := t.TempDir()
	collect := filepath.Join(dir, "unpacked")
	theirs := write(t, filepath.Join(collect, "release", "theirs.zip"), zipBytes(t, entry{"theirs.txt", []byte("theirs")}))
	inner := zipBytes(t, entry{"mine.txt", []byte("mine")})
	archive := write(t, filepath.Join(dir, "dl", "release.zip"), zipBytes(t, entry{"mine.zip", inner}))

	out, err := Run(context.Background(), Request{Path: archive, Options: Options{Dest: collect}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Nested != 1 {
		t.Errorf("Nested = %d, want only the archive this job wrote", out.Nested)
	}
	if exists(filepath.Join(collect, "release", "theirs")) {
		t.Error("an archive that was already in the folder was unpacked")
	}
	for _, v := range out.Volumes {
		if v == theirs {
			t.Errorf("Volumes = %v, offers another package's archive for disposal", out.Volumes)
		}
	}
	if body, err := os.ReadFile(filepath.Join(collect, "release", "mine", "mine.txt")); err != nil || string(body) != "mine" {
		t.Errorf("the nested archive this job wrote was not unpacked: %q, %v", body, err)
	}
}

// A compressed stream says nothing about its size up front, so a small one
// that unpacks without end would fill the disk the downloads share.
func TestACompressedStreamIsHeldToALimit(t *testing.T) {
	ratio, floor := streamRatio, streamFloor
	streamRatio, streamFloor = 2, 4096
	t.Cleanup(func() { streamRatio, streamFloor = ratio, floor })

	dir := t.TempDir()
	var fits, bomb bytes.Buffer
	gzipInto(t, &fits, make([]byte, 4096))
	gzipInto(t, &bomb, make([]byte, 4097))
	write(t, filepath.Join(dir, "fits.bin.gz"), fits.Bytes())
	write(t, filepath.Join(dir, "bomb.bin.gz"), bomb.Bytes())

	if _, err := Run(context.Background(), Request{Path: filepath.Join(dir, "fits.bin.gz")}); err != nil {
		t.Fatalf("a stream that ends at the limit failed: %v", err)
	}
	_, err := Run(context.Background(), Request{Path: filepath.Join(dir, "bomb.bin.gz")})
	if !errors.Is(err, ErrStreamTooLarge) {
		t.Fatalf("err = %v, want ErrStreamTooLarge", err)
	}
	if exists(filepath.Join(dir, "bomb.bin")) {
		t.Error("the payload that went past the limit was left on the disk")
	}
}

func TestATarInsideAStreamIsHeldToTheSameLimit(t *testing.T) {
	ratio, floor := streamRatio, streamFloor
	streamRatio, streamFloor = 2, 4096
	t.Cleanup(func() { streamRatio, streamFloor = ratio, floor })

	var tarred bytes.Buffer
	tw := tar.NewWriter(&tarred)
	body := make([]byte, 8192)
	if err := tw.WriteHeader(&tar.Header{Name: "big.bin", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var gz bytes.Buffer
	gzipInto(t, &gz, tarred.Bytes())
	write(t, filepath.Join(dir, "set.tar.gz"), gz.Bytes())

	_, err := Run(context.Background(), Request{Path: filepath.Join(dir, "set.tar.gz")})
	if !errors.Is(err, ErrStreamTooLarge) {
		t.Fatalf("err = %v, want ErrStreamTooLarge", err)
	}
	if exists(filepath.Join(dir, "set")) {
		t.Error("the folder of the refused tar was left on the disk")
	}
}

func readBody(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A release is packed as "Film/..." into "Film.part1.rar". Unpacked beside the
// archive that is "Film/...", once.
func TestAReleaseFolderNamedLikeItsArchiveIsNotWrappedTwice(t *testing.T) {
	dir := t.TempDir()
	movie := bytes.Repeat([]byte("frame"), 2000)
	paths := extracttest.WriteRarSet(t, dir, "Film.2026", extracttest.RarSet(4096,
		extracttest.File{Name: "Film.2026/Film (2026).mkv", Body: movie},
		extracttest.File{Name: "Film.2026/support/info.txt", Body: []byte("read me")},
	))

	out, err := Run(context.Background(), Request{Path: paths[0]})
	if err != nil {
		t.Fatal(err)
	}
	release := filepath.Join(dir, "Film.2026")
	if out.Dir != release {
		t.Errorf("Dir = %q, want %q", out.Dir, release)
	}
	if got := readBody(t, filepath.Join(release, "Film (2026).mkv")); got != string(movie) {
		t.Error("the film did not land directly in the release folder")
	}
	if got := readBody(t, filepath.Join(release, "support", "info.txt")); got != "read me" {
		t.Errorf("support/info.txt = %q", got)
	}
	if exists(filepath.Join(release, "Film.2026")) {
		t.Error("the release folder was created inside itself")
	}
}

func TestTheFolderMatchesTheArchiveNameRegardlessOfCase(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "film.zip"), zipBytes(t, entry{"Film/a.txt", []byte("a")}))

	if _, err := Run(context.Background(), Request{Path: filepath.Join(dir, "film.zip")}); err != nil {
		t.Fatal(err)
	}
	if got := readBody(t, filepath.Join(dir, "film", "a.txt")); got != "a" {
		t.Errorf("a.txt = %q", got)
	}
}

// Only the archive's own name is redundant. Any other folder name, or a file
// beside the folder, is part of what the packer meant.
func TestAFolderThatAddsSomethingIsKept(t *testing.T) {
	cases := []struct {
		name    string
		entries []entry
		want    string
	}{
		{"another name", []entry{{"Other/a.txt", []byte("a")}}, filepath.Join("film", "Other", "a.txt")},
		{"a file beside it", []entry{{"film/a.txt", []byte("a")}, {"readme.txt", []byte("r")}}, filepath.Join("film", "film", "a.txt")},
		{"two folders", []entry{{"film/a.txt", []byte("a")}, {"extras/b.txt", []byte("b")}}, filepath.Join("film", "film", "a.txt")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, filepath.Join(dir, "film.zip"), zipBytes(t, c.entries...))
			if _, err := Run(context.Background(), Request{Path: filepath.Join(dir, "film.zip")}); err != nil {
				t.Fatal(err)
			}
			if !exists(filepath.Join(dir, c.want)) {
				t.Errorf("%s is missing", c.want)
			}
		})
	}
}

func TestAPlainTarIsUnwrappedToo(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "app-1.2.tar"), tarStream(t, "./app-1.2/bin/run", []byte("run")))

	if _, err := Run(context.Background(), Request{Path: filepath.Join(dir, "app-1.2.tar")}); err != nil {
		t.Fatal(err)
	}
	if got := readBody(t, filepath.Join(dir, "app-1.2", "bin", "run")); got != "run" {
		t.Errorf("bin/run = %q", got)
	}
}

// The collision policy still decides the folder: a folder of that name that
// belongs to something else is stepped around under rename, and under
// overwrite a second run lands on the first run's files instead of nesting a
// copy inside them.
func TestUnwrappingLeavesTheCollisionPolicyInCharge(t *testing.T) {
	dir := t.TempDir()
	arc := write(t, filepath.Join(dir, "film.zip"), zipBytes(t, entry{"film/a.txt", []byte("new")}))
	mine := write(t, filepath.Join(dir, "film", "mine.txt"), []byte("not yours"))

	out, err := Run(context.Background(), Request{Path: arc, Options: Options{Collision: collide.Rename}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Dir == filepath.Join(dir, "film") {
		t.Fatal("rename unpacked into the folder that was already there")
	}
	if got := readBody(t, filepath.Join(out.Dir, "a.txt")); got != "new" {
		t.Errorf("a.txt = %q", got)
	}
	if exists(filepath.Join(dir, "film", "a.txt")) || readBody(t, mine) != "not yours" {
		t.Error("the folder that was already there was touched")
	}

	for range 2 {
		if _, err := Run(context.Background(), Request{Path: arc, Options: Options{Collision: collide.Overwrite}}); err != nil {
			t.Fatal(err)
		}
	}
	if got := readBody(t, filepath.Join(dir, "film", "a.txt")); got != "new" {
		t.Errorf("film/a.txt = %q", got)
	}
	if exists(filepath.Join(dir, "film", "film")) {
		t.Error("overwrite nested the release inside the existing folder")
	}
}

// Under a collect folder the outer archive goes there and its nested archive
// still unpacks beside its parent, each of them unwrapped.
func TestUnwrappingKeepsNestedArchivesBesideTheirParent(t *testing.T) {
	dir := t.TempDir()
	collect := filepath.Join(dir, "collect")
	inner := zipBytes(t, entry{"Setup/setup.exe", []byte("exe")})
	arc := write(t, filepath.Join(dir, "dl", "Film.zip"), zipBytes(t,
		entry{"Film/film.mkv", []byte("film")},
		entry{"Film/Setup.zip", inner},
	))

	out, err := Run(context.Background(), Request{Path: arc, Options: Options{Dest: collect}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Nested != 1 {
		t.Errorf("Nested = %d, want 1", out.Nested)
	}
	if got := readBody(t, filepath.Join(collect, "Film", "film.mkv")); got != "film" {
		t.Errorf("film.mkv = %q", got)
	}
	if got := readBody(t, filepath.Join(collect, "Film", "Setup", "setup.exe")); got != "exe" {
		t.Errorf("setup.exe = %q", got)
	}
	if exists(filepath.Join(collect, "Setup")) {
		t.Error("the nested archive was unpacked into the collect folder")
	}
}

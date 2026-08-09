package extract

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
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

// TestSplitFileIsJoinedAndThenOpened is the whole of the split-file row: the
// parts are one file, and if that file turns out to be an archive the job goes
// on and unpacks it. A user who downloaded five ".001" parts wants the film,
// not five pieces of one.
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

// TestASplitSetWithAHoleIsRefused. Writing out the first half of a film as if it
// were the film is worse than saying nothing came of it: the file plays for
// twenty minutes and stops, and nothing on disk says why.
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

// TestDeepExtraction follows an archive into the archive inside it, which is how
// most of what people download is actually packed.
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

// TestACancelledJobTakesBackTheFolderItMade is the reason an extraction is a job
// at all. A half-written extraction folder is indistinguishable from a finished
// one, so a job that is called off has to leave nothing behind that the next
// deep pass would walk into.
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

// TestCleanUpLeavesWhatWasAlreadyThere is the other half of that promise. A
// folder the job did not create is not the job's to remove, and for a single
// compressed stream the folder in question is the download folder.
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

// TestProgressCountsEveryDepth. A job that reports only the outermost archive
// shows a bar that stops moving for the half of the work that happens inside it.
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

// TestTwoJobsDoNotReadEachOther. The byte tap is a single binding, so without
// the slot two jobs at once would each be told the other's progress and either
// could cancel the other's copy. Both still finish; what must not happen is one
// of them hearing about the other's archive.
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

// TestASingleStreamDoesNotDragInItsNeighbours. A gzipped file unpacks BESIDE the
// archive, so the "output folder" of that job is the whole download folder.
// Walking it for nested archives would quietly unpack everything else in there.
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

// TestVolumeRankFollowsTheReaderAndNotTheAlphabet is the trap a list numbering
// its parts off a plain sort walks into: a spanned rar STARTS at ".rar" and a
// spanned zip ENDS at ".zip".
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

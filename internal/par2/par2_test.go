package par2

import (
	"bytes"
	"context"
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The sets in testdata were made by par2cmdline (see testdata/make.sh).

// copySet copies a set from testdata into a folder of its own, so a test can
// damage it.
func copySet(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join("testdata", name)
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func par2Files(t *testing.T, dir string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(dir, "*.par2"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func dataFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".par2") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

func load(t *testing.T, paths ...string) *Set {
	t.Helper()
	s, err := Load(paths...)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// original is a file of a set as par2cmdline saw it.
func original(t *testing.T, set, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", set, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writeAt(t *testing.T, path string, off int64, b []byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteAt(b, off); err != nil {
		t.Fatal(err)
	}
}

// verifyAndRepair matches, verifies and repairs the set in dir, and checks
// that every file came back as par2cmdline made it.
func verifyAndRepair(t *testing.T, set, dir string, o Options) *Report {
	t.Helper()
	s := load(t, par2Files(t, dir)...)
	rep, err := Verify(context.Background(), s, s.Match(dataFiles(t, dir)), o)
	if err != nil {
		t.Fatal(err)
	}
	before := &Report{Files: append([]FileReport(nil), rep.Files...)}
	if err := Repair(context.Background(), s, rep, dir, o); err != nil {
		t.Fatal(err)
	}
	for i, f := range s.Files {
		got, err := os.ReadFile(rep.Files[i].Path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, original(t, set, f.Name)) {
			t.Errorf("%s differs from the original after the repair", f.Name)
		}
	}
	return before
}

func TestLoadReadsAPar2cmdlineSet(t *testing.T) {
	s := load(t, par2Files(t, filepath.Join("testdata", "release"))...)
	if s.SliceSize != 4096 || len(s.Files) != 3 || s.Slices() != 41 || len(s.Recovery) != 12 {
		t.Fatalf("slice %d, %d files, %d slices, %d recovery", s.SliceSize, len(s.Files), s.Slices(), len(s.Recovery))
	}
	if !strings.Contains(s.Creator, "par2cmdline") {
		t.Errorf("creator %q", s.Creator)
	}
	sizes := map[string]int64{}
	for _, f := range s.Files {
		sizes[f.Name] = f.Size
	}
	if sizes["movie.mkv"] != 150001 || sizes["subs.srt"] != 9000 || sizes["tiny.nfo"] != 100 {
		t.Errorf("sizes %v", sizes)
	}
}

func TestAVolumeAloneDescribesTheSet(t *testing.T) {
	s := load(t, filepath.Join("testdata", "release", "release.vol04+4.par2"))
	if len(s.Files) != 3 || len(s.Recovery) != 4 || s.Recovery[0].Exponent != 4 {
		t.Fatalf("%d files, %d recovery slices from exponent %d", len(s.Files), len(s.Recovery), s.Recovery[0].Exponent)
	}
}

func TestAnIndexWithoutItsMainPacketIsNoSet(t *testing.T) {
	dir := copySet(t, "release")
	index := filepath.Join(dir, "release.par2")
	b, _ := os.ReadFile(index)
	i := bytes.Index(b, []byte("PAR 2.0\x00Main"))
	b[i+20] ^= 0xff
	os.WriteFile(index, b, 0o600)
	if _, err := Load(index); !errors.Is(err, ErrNoSet) {
		t.Fatalf("got %v", err)
	}
	// The volumes repeat it.
	if s := load(t, par2Files(t, dir)...); len(s.Recovery) != 12 {
		t.Fatalf("%d recovery slices", len(s.Recovery))
	}
}

func TestADamagedVolumeLosesOnlyTheSlicesItHits(t *testing.T) {
	dir := copySet(t, "release")
	vol := filepath.Join(dir, "release.vol00+4.par2")
	// Zeros where an article is missing, in the middle of the second slice.
	writeAt(t, vol, 8000, make([]byte, 700))
	s := load(t, par2Files(t, dir)...)
	if len(s.Recovery) != 11 {
		t.Fatalf("%d recovery slices, want 11", len(s.Recovery))
	}
}

func TestAWholeSetVerifies(t *testing.T) {
	dir := copySet(t, "posted")
	s := load(t, par2Files(t, dir)...)
	rep, err := Verify(context.Background(), s, s.Match(dataFiles(t, dir)), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Whole(s) || rep.Damaged() != 0 {
		t.Fatalf("%+v", rep)
	}
}

func TestCorruptBytesAreRepaired(t *testing.T) {
	dir := copySet(t, "release")
	movie := filepath.Join(dir, "movie.mkv")
	writeAt(t, movie, 10, []byte("damage"))
	writeAt(t, movie, 4096*20+100, []byte{1, 2, 3})
	writeAt(t, movie, 150000, []byte{0xee})
	rep := verifyAndRepair(t, "release", dir, Options{})
	if got := rep.Damaged(); got != 3 {
		t.Fatalf("%d slices found damaged, want 3", got)
	}
}

func TestHolesOfMissingArticlesAreRepaired(t *testing.T) {
	dir := copySet(t, "posted")
	part1 := filepath.Join(dir, "show.part1.rar")
	// Two articles of 30 000 bytes that never arrived, written as the zeros
	// of a sparse file.
	writeAt(t, part1, 50_000, make([]byte, 30_000))
	writeAt(t, part1, 300_000, make([]byte, 30_000))
	rep := verifyAndRepair(t, "posted", dir, Options{})
	if got := rep.Damaged(); got != 9 {
		t.Fatalf("%d slices found damaged, want 9", got)
	}
}

func TestMissingFilesAreRebuilt(t *testing.T) {
	dir := copySet(t, "release")
	os.Remove(filepath.Join(dir, "subs.srt"))
	os.Remove(filepath.Join(dir, "tiny.nfo"))
	rep := verifyAndRepair(t, "release", dir, Options{})
	if got := rep.Damaged(); got != 4 {
		t.Fatalf("%d slices missing, want 4", got)
	}
}

func TestAFileTooShortOrTooLongIsPutRight(t *testing.T) {
	dir := copySet(t, "release")
	os.Truncate(filepath.Join(dir, "movie.mkv"), 110_000)
	f, _ := os.OpenFile(filepath.Join(dir, "subs.srt"), os.O_APPEND|os.O_WRONLY, 0)
	f.Write([]byte("trailing junk"))
	f.Close()
	verifyAndRepair(t, "release", dir, Options{})
}

func TestARenamedFileIsFoundByItsContent(t *testing.T) {
	dir := copySet(t, "posted")
	os.Rename(filepath.Join(dir, "show.part1.rar"), filepath.Join(dir, "a8f3c02d91"))
	// The other one also has its first slice damaged, so only a later slice
	// can tell what it is.
	os.Rename(filepath.Join(dir, "show.part2.rar"), filepath.Join(dir, "73be1f"))
	writeAt(t, filepath.Join(dir, "73be1f"), 0, []byte("not the start"))
	s := load(t, par2Files(t, dir)...)
	got := s.Match(dataFiles(t, dir))
	want := map[string]string{"show.part1.rar": "a8f3c02d91", "show.part2.rar": "73be1f"}
	for i, f := range s.Files {
		if filepath.Base(got[i]) != want[f.Name] {
			t.Errorf("%s matched %q", f.Name, got[i])
		}
	}
	verifyAndRepair(t, "posted", dir, Options{})
}

func TestTooFewRecoverySlicesFailWithTheCounts(t *testing.T) {
	dir := copySet(t, "release")
	os.Remove(filepath.Join(dir, "movie.mkv"))
	s := load(t, par2Files(t, dir)...)
	rep, err := Verify(context.Background(), s, s.Match(dataFiles(t, dir)), Options{})
	if err != nil {
		t.Fatal(err)
	}
	err = Repair(context.Background(), s, rep, dir, Options{})
	var short *NotEnoughError
	if !errors.As(err, &short) || short.Damaged != 37 || short.Recovery != 12 {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "movie.mkv")); err == nil {
		t.Fatal("a repair that could not work wrote a file")
	}
}

func TestATightMemoryBudgetAndManyWorkersGiveTheSameBytes(t *testing.T) {
	dir := copySet(t, "posted")
	rng := rand.New(rand.NewPCG(1, 2))
	for _, name := range []string{"show.part1.rar", "show.part2.rar"} {
		for range 10 {
			junk := make([]byte, 50)
			for i := range junk {
				junk[i] = byte(rng.IntN(256))
			}
			off := rng.Int64N(190_000)
			writeAt(t, filepath.Join(dir, name), off, junk)
		}
	}
	var calls int
	var last, total int64
	o := Options{Workers: 3, Memory: 64 << 10, Progress: func(done, all int64) { calls, last, total = calls+1, done, all }}
	verifyAndRepair(t, "posted", dir, o)
	if calls == 0 || last != total {
		t.Fatalf("progress heard %d times, ended at %d of %d", calls, last, total)
	}
}

func TestATrustedFileIsNotRead(t *testing.T) {
	dir := copySet(t, "release")
	movie := filepath.Join(dir, "movie.mkv")
	var total int64
	s := load(t, par2Files(t, dir)...)
	rep, err := Verify(context.Background(), s, s.Match(dataFiles(t, dir)), Options{
		Trusted:  func(p string) bool { return p == movie },
		Progress: func(_, all int64) { total = all },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Whole(s) || total != 9100 {
		t.Fatalf("whole %v, read %d bytes", rep.Whole(s), total)
	}
}

func TestADamagedTrustedFileIsCaughtAfterTheRepair(t *testing.T) {
	dir := copySet(t, "release")
	movie := filepath.Join(dir, "movie.mkv")
	writeAt(t, movie, 100_000, []byte("bad"))
	writeAt(t, filepath.Join(dir, "subs.srt"), 0, []byte("bad"))
	s := load(t, par2Files(t, dir)...)
	o := Options{Trusted: func(p string) bool { return p == movie }}
	rep, err := Verify(context.Background(), s, s.Match(dataFiles(t, dir)), o)
	if err != nil {
		t.Fatal(err)
	}
	if err := Repair(context.Background(), s, rep, dir, o); !errors.Is(err, ErrMismatch) {
		t.Fatalf("got %v", err)
	}
}

func TestACancelledVerifyStops(t *testing.T) {
	dir := copySet(t, "posted")
	s := load(t, par2Files(t, dir)...)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Verify(ctx, s, s.Match(dataFiles(t, dir)), Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestVolumeBlocks(t *testing.T) {
	for name, want := range map[string]int{
		"show.vol07+08.par2":   8,
		"show.VOL015+009.PAR2": 9,
		"release.vol00+4.par2": 4,
		"show.par2":            0,
		"show.vol07+08.rar":    0,
	} {
		if got := VolumeBlocks(name); got != want {
			t.Errorf("%s: %d, want %d", name, got, want)
		}
	}
}

func TestInputConstantsSkipExponentsSharingAFactorWithTheFieldOrder(t *testing.T) {
	got := inputConstants(5)
	want := []uint16{2, 4, 16, 128, 256}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestMulAddMatchesWordByWordMultiplication(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	for _, c := range []uint16{0, 1, 2, 0x1234, 0xffff} {
		src := make([]byte, 22)
		dst := make([]byte, 22)
		for i := range src {
			src[i], dst[i] = byte(rng.IntN(256)), byte(rng.IntN(256))
		}
		want := append([]byte(nil), dst...)
		for i := 0; i < len(src); i += 2 {
			v := gfMul(c, uint16(src[i])|uint16(src[i+1])<<8)
			want[i] ^= byte(v)
			want[i+1] ^= byte(v >> 8)
		}
		var tbl mulTable
		tbl.set(c)
		tbl.mulAdd(dst, src)
		if !bytes.Equal(dst, want) {
			t.Fatalf("constant %#x: got %x, want %x", c, dst, want)
		}
	}
}

func TestFieldInverses(t *testing.T) {
	for _, a := range []uint16{1, 2, 3, 0x8000, 0xffff, 0x1100} {
		if gfMul(gfDiv(1, a), a) != 1 {
			t.Errorf("1/%#x times %#x is not 1", a, a)
		}
		if gfPow(a, fieldMax) != 1 {
			t.Errorf("%#x to the group order is not 1", a)
		}
	}
	m := [][]uint16{{1, 2}, {3, 4}}
	inv, ok := invert(m)
	if !ok {
		t.Fatal("not invertible")
	}
	for i := range 2 {
		for j := range 2 {
			var x uint16
			for k := range 2 {
				x ^= gfMul(m[i][k], inv[k][j])
			}
			if (i == j) != (x == 1) || (i != j && x != 0) {
				t.Fatalf("m times its inverse at %d,%d is %#x", i, j, x)
			}
		}
	}
	if _, ok := invert([][]uint16{{1, 2}, {1, 2}}); ok {
		t.Fatal("a singular matrix was inverted")
	}
}

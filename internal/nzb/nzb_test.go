package nzb

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestParseRelease(t *testing.T) {
	data, err := os.ReadFile("testdata/release.nzb")
	if err != nil {
		t.Fatal(err)
	}
	n, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if n.Meta["title"] != "Some.Show.S01E01.1080p.WEB" || n.Meta["password"] != "hunter2" {
		t.Fatalf("meta: %v", n.Meta)
	}
	names := []string{
		"Some.Show.S01E01.part1.rar",
		"Some.Show.S01E01.part2.rar",
		"Some.Show.S01E01.par2",
		"Some.Show.S01E01.vol00+01.par2",
		"Some.Show.S01E01.vol01+02.PAR2",
	}
	if len(n.Files) != len(names) {
		t.Fatalf("got %d files, want %d: the entry without articles is left out", len(n.Files), len(names))
	}
	for i, want := range names {
		if n.Files[i].Name != want {
			t.Errorf("file %d: got %q, want %q", i, n.Files[i].Name, want)
		}
	}

	first := n.Files[0]
	if len(first.Segments) != 3 {
		t.Fatalf("a part listed twice is kept once, got %d segments", len(first.Segments))
	}
	for i, s := range first.Segments {
		if s.Number != i+1 {
			t.Fatalf("segments are sorted by number, got %v", first.Segments)
		}
	}
	if first.Segments[0].ID != "part1-1@example.invalid" {
		t.Errorf("angle brackets are dropped from ids, got %q", first.Segments[0].ID)
	}
	if first.Segments[1].ID != "part1-2@example.invalid" {
		t.Errorf("the first copy of a part listed twice is kept, got %q", first.Segments[1].ID)
	}
	if first.Bytes() != 768000+768000+120000 {
		t.Errorf("bytes: %d", first.Bytes())
	}
	if !first.Posted.Equal(time.Unix(1700000000, 0)) || len(first.Groups) != 2 {
		t.Errorf("posted %v, groups %v", first.Posted, first.Groups)
	}
}

func TestParseRefusesEmptyAndBroken(t *testing.T) {
	if _, err := Parse([]byte(`<nzb><file subject="x"><segments/></file></nzb>`)); !errors.Is(err, ErrEmpty) {
		t.Fatalf("got %v", err)
	}
	if _, err := Parse([]byte("not xml at all <")); err == nil {
		t.Fatal("broken XML parsed")
	}
}

func TestDuplicateNamesGetANumber(t *testing.T) {
	doc := `<nzb>
		<file subject="&quot;a.bin&quot; yEnc"><segments><segment number="1" bytes="1">1@x</segment></segments></file>
		<file subject="&quot;A.bin&quot; yEnc"><segments><segment number="1" bytes="1">2@x</segment></segments></file>
	</nzb>`
	n, err := Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if n.Files[0].Name != "a.bin" || n.Files[1].Name != "A (2).bin" {
		t.Fatalf("got %q and %q", n.Files[0].Name, n.Files[1].Name)
	}
}

func TestNameFromSubject(t *testing.T) {
	cases := map[string]string{
		`[01/10] - "Show.S01E01.mkv" yEnc (1/50)`: "Show.S01E01.mkv",
		`Show.S01E01.mkv (1/50) yEnc`:             "Show.S01E01.mkv",
		`[3/9] Show.r00 yEnc (1/50)`:              "Show.r00",
		`"../../etc/passwd" yEnc`:                 "passwd",
		`"..\..\boot.ini" yEnc`:                   "boot.ini",
		`"what?.txt" yEnc`:                        "what_.txt",
		`"..." yEnc`:                              "file",
		`no name in here`:                         "no name in here",
	}
	for subject, want := range cases {
		if got := NameFromSubject(subject); got != want {
			t.Errorf("%s: got %q, want %q", subject, got, want)
		}
	}
}

func TestIsRecoveryVolume(t *testing.T) {
	for name, want := range map[string]bool{
		"x.vol00+01.par2":   true,
		"x.VOL07-15.PAR2":   true,
		"x.par2":            false,
		"x.vol00+01.par2.1": false,
		"x.part01.rar":      false,
	} {
		if IsRecoveryVolume(name) != want {
			t.Errorf("%s: want %v", name, want)
		}
	}
}

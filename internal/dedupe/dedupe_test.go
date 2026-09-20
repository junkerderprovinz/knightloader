package dedupe

import (
	"fmt"
	"testing"
)

func seed(p Policy, entries ...Entry) *Set {
	s := New(p)
	for _, e := range entries {
		s.Add(e)
	}
	return s
}

// TestArchiveVolumesAreNeverMirrors checks every policy against volumes of
// one split archive, which share a base name and a size. Merging one would
// leave a set that cannot be unpacked.
func TestArchiveVolumesAreNeverMirrors(t *testing.T) {
	sets := []struct {
		name  string
		first string
		next  string
	}{
		{"rar part volumes", "Film.part01.rar", "Film.part02.rar"},
		{"old style rar volumes", "Film.rar", "Film.r00"},
		{"consecutive old style rar volumes", "Film.r00", "Film.r01"},
		{"7z volumes", "Film.7z.001", "Film.7z.002"},
		{"zip span segments", "Film.z01", "Film.z02"},
		{"zip span against its final segment", "Film.z01", "Film.zip"},
		{"generic split parts", "Film.mkv.001", "Film.mkv.002"},
		{"whole archive against a split part", "Film.7z", "Film.7z.001"},
		// Numbering shapes the marker table does not list.
		{"four digit split parts", "Film.mkv.0001", "Film.mkv.0002"},
		{"rar volumes past r99", "Film.r99", "Film.r100"},
		{"two digit split parts", "Film.tar.gz.01", "Film.tar.gz.02"},
		{"zip segments past z99", "Film.z100", "Film.z101"},
	}
	const size = 100 << 20
	for _, set := range sets {
		for _, p := range Policies() {
			t.Run(set.name+"/"+string(p), func(t *testing.T) {
				s := seed(p, Entry{ID: "1", URL: "https://a.example/1", Name: set.first, Size: size})
				got := s.Check(Entry{URL: "https://b.example/2", Name: set.next, Size: size})
				if got.Verdict != NotSeen {
					t.Fatalf("%q against %q: verdict = %v (%s, of %q), want %v; a volume of the archive would have been dropped",
						set.next, set.first, got.Verdict, got.Signal, got.Of.Name, NotSeen)
				}
			})
		}
	}
}

func TestNumberedSiblings(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{name: "four digit split parts", a: "film.mkv.0001", b: "film.mkv.0002", want: true},
		{name: "rar volumes past r99", a: "film.r99", b: "film.r100", want: true},
		{name: "numbered discs", a: "film.cd1.iso", b: "film.cd2.iso", want: true},
		{name: "version numbers", a: "setup_v1.2.exe", b: "setup_v1.3.exe", want: true},
		{name: "release years", a: "film.2023.mkv", b: "film.2024.mkv", want: true},
		{name: "the same name", a: "film.mkv", b: "film.mkv"},
		{name: "zero padding only", a: "film.part1.rar", b: "film.part01.rar"},
		{name: "leading zeros only, unpadded first", a: "film.001", b: "film.1"},
		{name: "no number on one side", a: "film.mkv", b: "film2.mkv"},
		{name: "no number at all", a: "one.bin", b: "two.bin"},
		{name: "an unresolved name", a: "", b: "film.002"},
		{name: "different in more than the number", a: "film.001", b: "other.002"},
		{name: "different extensions", a: "film.001.mkv", b: "film.002.avi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := numberedSiblings(tt.a, tt.b); got != tt.want {
				t.Fatalf("numberedSiblings(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
			if got := numberedSiblings(tt.b, tt.a); got != tt.want {
				t.Fatalf("numberedSiblings(%q, %q) = %v, want %v", tt.b, tt.a, got, tt.want)
			}
		})
	}
}

// TestAgreeingDigestsDoNotOverruleTheSiblingVeto: a 32-bit CRC from a release
// name is evidence, not proof, so it cannot merge two volumes.
func TestAgreeingDigestsDoNotOverruleTheSiblingVeto(t *testing.T) {
	const crc = "1a2b3c4d"
	s := seed(PolicyHashOnly, Entry{ID: "1", URL: "https://a.example/1", Name: "Film.mkv.0001",
		Hash: Hash{Kind: "crc32", Hex: crc}})
	got := s.Check(Entry{URL: "https://b.example/2", Name: "Film.mkv.0002",
		Hash: Hash{Kind: "crc32", Hex: crc}})
	if got.Verdict != NotSeen {
		t.Fatalf("verdict = %v, want %v: a colliding CRC must not merge two volumes", got.Verdict, NotSeen)
	}
}

func TestSameVolumeSpelledDifferentlyIsAMirror(t *testing.T) {
	s := seed(PolicyFilenameAndSize, Entry{ID: "1", URL: "https://a.example/1", Name: "Film.part1.rar", Size: 100})
	got := s.Check(Entry{URL: "https://b.example/2", Name: "FILM.PART01.RAR", Size: 100})
	if got.Verdict != Mirror || got.Of.ID != "1" {
		t.Fatalf("verdict = %v of %q, want %v of entry 1", got.Verdict, got.Of.ID, Mirror)
	}
}

func TestDuplicateURLIsRefusedUnderEveryPolicy(t *testing.T) {
	for _, p := range Policies() {
		t.Run(string(p), func(t *testing.T) {
			s := seed(p, Entry{ID: "1", URL: "https://host.example/file.rar", Name: "file.rar", Size: 10})
			// A re-paste carries only the URL, not the resolved name and size.
			got := s.Check(Entry{URL: "https://host.example/file.rar"})
			if got.Verdict != Duplicate {
				t.Fatalf("verdict = %v, want %v", got.Verdict, Duplicate)
			}
			if got.Signal != SignalURL {
				t.Fatalf("signal = %q, want %q", got.Signal, SignalURL)
			}
			if got.Of.ID != "1" {
				t.Fatalf("matched entry %q, want the one already staged", got.Of.ID)
			}
		})
	}
}

func TestURLIsFoldedOnlyWhereItIsCaseInsensitive(t *testing.T) {
	tests := []struct {
		name  string
		have  string
		cand  string
		want  Verdict
		about string
	}{
		{
			name: "host case", have: "https://Host.Example/File.rar", cand: "https://host.example/File.rar",
			want: Duplicate, about: "DNS is case-insensitive",
		},
		{
			name: "scheme case", have: "HTTPS://host.example/a", cand: "https://host.example/a",
			want: Duplicate, about: "the scheme is case-insensitive",
		},
		{
			name: "default port spelled out", have: "https://host.example:443/a", cand: "https://host.example/a",
			want: Duplicate, about: "the default port is the same endpoint",
		},
		{
			name: "surrounding whitespace", have: "https://host.example/a", cand: "  https://host.example/a  ",
			want: Duplicate, about: "a pasted link carries whitespace",
		},
		{
			name: "magnet infohash case", have: "magnet:?xt=urn:btih:0123456789ABCDEF0123456789abcdef01234567",
			cand: "magnet:?xt=urn:btih:0123456789abcdef0123456789ABCDEF01234567",
			want: Duplicate, about: "an infohash is case-insensitive in both its spellings",
		},
		{
			name: "different magnet", have: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
			cand: "magnet:?xt=urn:btih:fedcba9876543210fedcba9876543210fedcba98",
			want: NotSeen, about: "a different infohash is a different torrent",
		},
		{
			name: "path case", have: "https://host.example/File.rar", cand: "https://host.example/file.rar",
			want: NotSeen, about: "the path is case-sensitive and these can be two files",
		},
		{
			name: "fragment", have: "https://host.example/f#keyA", cand: "https://host.example/f#keyB",
			want: NotSeen, about: "hosters put the decryption key in the fragment",
		},
		{
			name: "non default port", have: "https://host.example:8443/a", cand: "https://host.example/a",
			want: NotSeen, about: "a different port is a different endpoint",
		},
		{
			name: "query", have: "https://host.example/get?id=1", cand: "https://host.example/get?id=2",
			want: NotSeen, about: "the query selects the file on plenty of hosters",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// PolicyOff so only the URL can produce a match.
			s := seed(PolicyOff, Entry{ID: "1", URL: tt.have})
			if got := s.Check(Entry{URL: tt.cand}); got.Verdict != tt.want {
				t.Fatalf("%q against %q: verdict = %v, want %v (%s)", tt.cand, tt.have, got.Verdict, tt.want, tt.about)
			}
		})
	}
}

func TestPolicyDecidesWhatCountsAsAMirror(t *testing.T) {
	const (
		md5A = "d41d8cd98f00b204e9800998ecf8427e"
		md5B = "0cc175b9c0f1b6a831c399e269772661"
	)
	have := Entry{ID: "1", URL: "https://a.example/1", Name: "Release.2024.mkv", Size: 4096,
		Hash: Hash{Kind: "md5", Hex: md5A}}

	tests := []struct {
		name   string
		policy Policy
		cand   Entry
		want   Verdict
		signal Signal
	}{
		{
			name: "filename only merges despite a different size", policy: PolicyFilenameOnly,
			cand: Entry{URL: "https://b.example/2", Name: "release.2024.mkv", Size: 9999},
			want: Mirror, signal: SignalName,
		},
		{
			name: "filename only refuses a different name", policy: PolicyFilenameOnly,
			cand: Entry{URL: "https://b.example/2", Name: "Other.2024.mkv", Size: 4096},
			want: NotSeen,
		},
		{
			name: "size only merges despite a different name", policy: PolicySizeOnly,
			cand: Entry{URL: "https://b.example/2", Name: "renamed-by-hoster.bin", Size: 4096},
			want: Mirror, signal: SignalSize,
		},
		{
			name: "size only refuses a different size", policy: PolicySizeOnly,
			cand: Entry{URL: "https://b.example/2", Name: "Release.2024.mkv", Size: 4097},
			want: NotSeen,
		},
		{
			name: "filename and size merges when both agree", policy: PolicyFilenameAndSize,
			cand: Entry{URL: "https://b.example/2", Name: "Release.2024.mkv", Size: 4096},
			want: Mirror, signal: SignalNameSize,
		},
		{
			name: "filename and size refuses on the name alone", policy: PolicyFilenameAndSize,
			cand: Entry{URL: "https://b.example/2", Name: "Release.2024.mkv", Size: 4097},
			want: NotSeen,
		},
		{
			name: "filename and size refuses when the size is unknown", policy: PolicyFilenameAndSize,
			cand: Entry{URL: "https://b.example/2", Name: "Release.2024.mkv"},
			want: NotSeen,
		},
		{
			name: "filename or hash merges on the hash alone", policy: PolicyFilenameOrHash,
			cand: Entry{URL: "https://b.example/2", Name: "renamed-by-hoster.bin",
				Hash: Hash{Kind: "MD5", Hex: md5A}},
			want: Mirror, signal: SignalHash,
		},
		{
			name: "filename or hash merges on the name alone", policy: PolicyFilenameOrHash,
			cand: Entry{URL: "https://b.example/2", Name: "Release.2024.mkv"},
			want: Mirror, signal: SignalName,
		},
		{
			name: "hash only ignores a matching name", policy: PolicyHashOnly,
			cand: Entry{URL: "https://b.example/2", Name: "Release.2024.mkv", Size: 4096},
			want: NotSeen,
		},
		{
			name: "hash only merges on a matching digest", policy: PolicyHashOnly,
			cand: Entry{URL: "https://b.example/2", Name: "whatever.bin",
				Hash: Hash{Kind: "md5", Hex: md5A}},
			want: Mirror, signal: SignalHash,
		},
		{
			name: "hash only refuses a different digest", policy: PolicyHashOnly,
			cand: Entry{URL: "https://b.example/2", Name: "Release.2024.mkv", Size: 4096,
				Hash: Hash{Kind: "md5", Hex: md5B}},
			want: NotSeen,
		},
		{
			name: "off merges nothing at all", policy: PolicyOff,
			cand: Entry{URL: "https://b.example/2", Name: "Release.2024.mkv", Size: 4096,
				Hash: Hash{Kind: "md5", Hex: md5A}},
			want: NotSeen,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := seed(tt.policy, have).Check(tt.cand)
			if got.Verdict != tt.want {
				t.Fatalf("verdict = %v, want %v", got.Verdict, tt.want)
			}
			if tt.want == Mirror {
				if got.Signal != tt.signal {
					t.Fatalf("signal = %q, want %q", got.Signal, tt.signal)
				}
				if got.Of.ID != "1" {
					t.Fatalf("matched entry %q, want the staged one", got.Of.ID)
				}
			}
		})
	}
}

// TestUnknownSignalsNeverMatchEachOther covers unresolved links, whose name,
// size and hash are absent. If absence were a bucket key, every link after
// the first would be dropped as its mirror.
func TestUnknownSignalsNeverMatchEachOther(t *testing.T) {
	tests := []struct {
		name   string
		policy Policy
		a, b   Entry
	}{
		{
			name: "names still holding the raw URL", policy: PolicyFilenameOnly,
			a: Entry{ID: "1", URL: "https://a.example/dl", Name: "https://a.example/dl"},
			b: Entry{URL: "https://b.example/dl", Name: "https://b.example/dl"},
		},
		{
			name: "no name at all", policy: PolicyFilenameOnly,
			a: Entry{ID: "1", URL: "https://a.example/1"},
			b: Entry{URL: "https://b.example/2"},
		},
		{
			name: "unknown sizes", policy: PolicySizeOnly,
			a: Entry{ID: "1", URL: "https://a.example/1", Name: "one.bin"},
			b: Entry{URL: "https://b.example/2", Name: "two.bin"},
		},
		{
			name: "no hashes", policy: PolicyHashOnly,
			a: Entry{ID: "1", URL: "https://a.example/1", Name: "one.bin", Size: 10},
			b: Entry{URL: "https://b.example/2", Name: "one.bin", Size: 10},
		},
		{
			name: "half filled hashes", policy: PolicyHashOnly,
			a: Entry{ID: "1", URL: "https://a.example/1", Hash: Hash{Kind: "md5"}},
			b: Entry{URL: "https://b.example/2", Hash: Hash{Kind: "sha256"}},
		},
		{
			name: "neither name nor hash", policy: PolicyFilenameOrHash,
			a: Entry{ID: "1", URL: "https://a.example/1"},
			b: Entry{URL: "https://b.example/2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := seed(tt.policy, tt.a).Check(tt.b); got.Verdict != NotSeen {
				t.Fatalf("verdict = %v (%s), want %v; an unresolved link was thrown away",
					got.Verdict, got.Signal, NotSeen)
			}
		})
	}
}

func TestConflictingDigestsOverruleAMatchingName(t *testing.T) {
	const (
		md5A = "d41d8cd98f00b204e9800998ecf8427e"
		md5B = "0cc175b9c0f1b6a831c399e269772661"
	)
	have := Entry{ID: "1", URL: "https://a.example/1", Name: "setup.exe", Size: 4096,
		Hash: Hash{Kind: "md5", Hex: md5A}}

	conflicting := Entry{URL: "https://b.example/2", Name: "setup.exe", Size: 4096,
		Hash: Hash{Kind: "md5", Hex: md5B}}
	if got := seed(PolicyFilenameOrHash, have).Check(conflicting); got.Verdict != NotSeen {
		t.Fatalf("verdict = %v, want %v: the digests prove two different files", got.Verdict, NotSeen)
	}
	if got := seed(PolicyFilenameOnly, have).Check(conflicting); got.Verdict != NotSeen {
		t.Fatalf("verdict = %v, want %v under filename-only too", got.Verdict, NotSeen)
	}

	// Digests of different algorithms cannot contradict each other.
	other := Entry{URL: "https://c.example/3", Name: "setup.exe", Size: 4096,
		Hash: Hash{Kind: "sha256", Hex: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}}
	if got := seed(PolicyFilenameOnly, have).Check(other); got.Verdict != Mirror {
		t.Fatalf("verdict = %v, want %v: an md5 and a sha256 cannot contradict each other", got.Verdict, Mirror)
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		in     string
		base   string
		volume string
	}{
		{in: "Film.2024.mkv", base: "film.2024.mkv"},
		{in: "  The   Big   File.bin  ", base: "the big file.bin"},
		{in: "sub/dir/film.mkv", base: "film.mkv"},
		{in: `sub\dir\film.mkv`, base: "film.mkv"},
		{in: "Film.part01.rar", base: "film", volume: "rar-part1"},
		{in: "Film.part1.rar", base: "film", volume: "rar-part1"},
		{in: "Film.PART007.RAR", base: "film", volume: "rar-part7"},
		{in: "Film.rar", base: "film", volume: "rar-first"},
		{in: "Film.r00", base: "film", volume: "rar-r0"},
		{in: "Film.7z", base: "film", volume: "7z-first"},
		{in: "Film.7z.002", base: "film", volume: "7z-part2"},
		{in: "Film.zip", base: "film", volume: "zip-last"},
		{in: "Film.z09", base: "film", volume: "zip-part9"},
		{in: "Film.mkv.003", base: "film.mkv", volume: "split-part3"},
		// Four digits are a release year, not volume 2024.
		{in: "Film.2024", base: "film.2024"},
		// Placeholders, not names.
		{in: "https://host.example/download", base: ""},
		{in: "", base: ""},
		{in: "   ", base: ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := Normalize(tt.in)
			if got.Display != tt.in {
				t.Fatalf("Display = %q, want the untouched original %q", got.Display, tt.in)
			}
			if got.Base != tt.base {
				t.Fatalf("Base = %q, want %q", got.Base, tt.base)
			}
			if got.Volume != tt.volume {
				t.Fatalf("Volume = %q, want %q", got.Volume, tt.volume)
			}
		})
	}
}

func TestCheckDoesNotAddTheCandidate(t *testing.T) {
	s := seed(PolicyFilenameAndSize)
	cand := Entry{URL: "https://a.example/1", Name: "film.mkv", Size: 10}
	for i := range 2 {
		if got := s.Check(cand); got.Verdict != NotSeen {
			t.Fatalf("check %d: verdict = %v, want %v", i+1, got.Verdict, NotSeen)
		}
	}
	if s.Len() != 0 {
		t.Fatalf("Len = %d after two checks, want 0", s.Len())
	}
}

func TestReAddReplacesAndRemoveForgets(t *testing.T) {
	s := New(PolicyFilenameOnly)
	s.Add(Entry{ID: "1", URL: "https://a.example/1", Name: "old-name.mkv"})
	s.Add(Entry{ID: "1", URL: "https://a.example/1", Name: "new-name.mkv"})
	if s.Len() != 1 {
		t.Fatalf("Len = %d after adding the same URL twice, want 1", s.Len())
	}
	if got := s.Check(Entry{URL: "https://b.example/2", Name: "old-name.mkv"}); got.Verdict != NotSeen {
		t.Fatalf("verdict = %v for the replaced name, want %v", got.Verdict, NotSeen)
	}
	if got := s.Check(Entry{URL: "https://b.example/2", Name: "new-name.mkv"}); got.Verdict != Mirror {
		t.Fatalf("verdict = %v for the current name, want %v", got.Verdict, Mirror)
	}

	// Removal has to clear the mirror buckets as well as the URL index.
	s.Remove("https://A.example/1")
	if s.Len() != 0 {
		t.Fatalf("Len = %d after Remove, want 0", s.Len())
	}
	if len(s.buckets) != 0 {
		t.Fatalf("buckets = %v after Remove, want the key space emptied too", s.buckets)
	}
	if got := s.Check(Entry{URL: "https://b.example/2", Name: "new-name.mkv"}); got.Verdict != NotSeen {
		t.Fatalf("verdict = %v after Remove, want %v", got.Verdict, NotSeen)
	}
	s.Remove("https://nowhere.example/x")
	s.Remove("")
}

func TestEntryWithoutURLIsIgnored(t *testing.T) {
	s := New(PolicyFilenameOnly)
	s.Add(Entry{ID: "1", Name: "film.mkv"})
	s.Add(Entry{ID: "2", URL: "   ", Name: "film.mkv"})
	if s.Len() != 0 {
		t.Fatalf("Len = %d, want 0: a link with no URL is not a download", s.Len())
	}
	if got := s.Check(Entry{Name: "film.mkv"}); got.Verdict != NotSeen {
		t.Fatalf("verdict = %v, want %v", got.Verdict, NotSeen)
	}
}

// TestCheckReadsOneBucketNotTheWholeList asserts the size of the bucket a
// query compares against rather than wall-clock time, which would only
// measure the machine.
func TestCheckReadsOneBucketNotTheWholeList(t *testing.T) {
	const n = 20000
	s := New(PolicyFilenameAndSize)
	for i := range n {
		s.Add(Entry{
			ID:   fmt.Sprint(i),
			URL:  fmt.Sprintf("https://host%d.example/file%d.bin", i, i),
			Name: fmt.Sprintf("file%d.bin", i),
			Size: int64(i + 1),
		})
	}
	if s.Len() != n {
		t.Fatalf("Len = %d, want %d", s.Len(), n)
	}

	// One key per entry; a constant signature would make every lookup a scan.
	if len(s.buckets) != n {
		t.Fatalf("the set holds %d keys for %d entries, want one each", len(s.buckets), n)
	}

	cand := Entry{URL: "https://mirror.example/x", Name: "file12345.bin", Size: 12346}
	sigs := s.signatures(newRecord(cand))
	if len(sigs) != 1 {
		t.Fatalf("signatures = %d, want 1 under %s", len(sigs), s.Policy())
	}
	if got := len(s.buckets[sigs[0].key]); got != 1 {
		t.Fatalf("the candidate's bucket holds %d records out of %d entries, want 1", got, n)
	}
	if got := s.Check(cand); got.Verdict != Mirror || got.Of.ID != "12345" {
		t.Fatalf("verdict = %v of %q, want %v of entry 12345", got.Verdict, got.Of.ID, Mirror)
	}

	miss := Entry{URL: "https://mirror.example/y", Name: "nothing-like-it.bin", Size: 7}
	missSigs := s.signatures(newRecord(miss))
	if len(missSigs) != 1 {
		t.Fatalf("signatures = %d, want 1 under %s", len(missSigs), s.Policy())
	}
	if got := len(s.buckets[missSigs[0].key]); got != 0 {
		t.Fatalf("a candidate that matches nothing lands in a bucket of %d, want 0", got)
	}
	if got := s.Check(miss); got.Verdict != NotSeen {
		t.Fatalf("verdict = %v, want %v", got.Verdict, NotSeen)
	}
}

func TestParsePolicy(t *testing.T) {
	tests := []struct {
		in   string
		want Policy
	}{
		{in: "off", want: PolicyOff},
		{in: "filename-only", want: PolicyFilenameOnly},
		{in: "size-only", want: PolicySizeOnly},
		{in: "filename-and-size", want: PolicyFilenameAndSize},
		{in: "filename-or-hash", want: PolicyFilenameOrHash},
		{in: "hash-only", want: PolicyHashOnly},
		{in: "  Filename-And-Size  ", want: PolicyFilenameAndSize},
		{in: "", want: DefaultPolicy},
		{in: "filename", want: DefaultPolicy},
		{in: "yes please", want: DefaultPolicy},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := ParsePolicy(tt.in); got != tt.want {
				t.Fatalf("ParsePolicy(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
	for _, p := range Policies() {
		if !p.Valid() {
			t.Fatalf("%q is offered but not valid", p)
		}
		if ParsePolicy(string(p)) != p {
			t.Fatalf("%q does not survive ParsePolicy", p)
		}
	}
	if got := New("nonsense").Policy(); got != DefaultPolicy {
		t.Fatalf("New(%q).Policy() = %q, want %q", "nonsense", got, DefaultPolicy)
	}
}

func BenchmarkCheck(b *testing.B) {
	s := New(PolicyFilenameAndSize)
	for i := range 20000 {
		s.Add(Entry{
			ID:   fmt.Sprint(i),
			URL:  fmt.Sprintf("https://host%d.example/file%d.bin", i, i),
			Name: fmt.Sprintf("file%d.bin", i),
			Size: int64(i + 1),
		})
	}
	cand := Entry{URL: "https://mirror.example/x", Name: "file12345.bin", Size: 12346}
	b.ResetTimer()
	for range b.N {
		if s.Check(cand).Verdict != Mirror {
			b.Fatal("the mirror was not found")
		}
	}
}

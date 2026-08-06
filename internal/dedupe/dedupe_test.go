package dedupe

import (
	"fmt"
	"testing"
)

// seed builds a set holding the given entries, which is how a caller uses it:
// feed in what is already staged, then ask about the candidate.
func seed(p Policy, entries ...Entry) *Set {
	s := New(p)
	for _, e := range entries {
		s.Add(e)
	}
	return s
}

// TestArchiveVolumesAreNeverMirrors is the failure that costs a user their
// download rather than their patience. Every volume of a split archive carries
// the same base name and, because the splitter cuts at a fixed size, the same
// byte count - so a mirror check that trusts either signal declares part 2 a
// mirror of part 1, never fetches it, and leaves behind a set that cannot be
// unpacked. The volume marker has to overrule the policy, including the policies
// that never look at a file name at all.
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
		// The shapes below are numbered by something the marker table does not
		// recognise, which is the point: a set that is safe only for the eight
		// spellings somebody thought to list is not safe. Each of these merged
		// part 2 into part 1 under size-only, where every part collides by
		// construction, and took the archive with it.
		{"four digit split parts", "Film.mkv.0001", "Film.mkv.0002"},
		{"rar volumes past r99", "Film.r99", "Film.r100"},
		{"two digit split parts", "Film.tar.gz.01", "Film.tar.gz.02"},
		{"zip segments past z99", "Film.z100", "Film.z101"},
	}
	// The same digest is deliberately not tested here: two volumes never have
	// one. Everything else a policy can key on is identical between the parts.
	const size = 100 << 20
	for _, set := range sets {
		for _, p := range Policies() {
			t.Run(set.name+"/"+string(p), func(t *testing.T) {
				s := seed(p, Entry{ID: "1", URL: "https://a.example/1", Name: set.first, Size: size})
				got := s.Check(Entry{URL: "https://b.example/2", Name: set.next, Size: size})
				if got.Verdict != NotSeen {
					t.Fatalf("%q against %q: verdict = %v (%s, of %q), want %v - a volume of the archive would have been dropped",
						set.next, set.first, got.Verdict, got.Signal, got.Of.Name, NotSeen)
				}
			})
		}
	}
}

// TestNumberedSiblings pins the veto that catches the part numbering the marker
// table does not know, in both directions: it has to fire on consecutive members
// of a set, and it must stay out of the way of two spellings of one name, or it
// would refuse the merges the whole package exists to make.
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
			// The question is symmetric; a set that answered it differently
			// depending on which link was pasted first would merge or refuse by
			// accident of ordering.
			if got := numberedSiblings(tt.b, tt.a); got != tt.want {
				t.Fatalf("numberedSiblings(%q, %q) = %v, want %v", tt.b, tt.a, got, tt.want)
			}
		})
	}
}

// TestAgreeingDigestsDoNotOverruleTheSiblingVeto records a deliberate choice: a
// digest is treated as evidence, not proof, because the digests this package is
// handed include the 32 bit CRC out of a release name. An extra download is the
// price; a wrongly merged archive part is unrecoverable.
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

// If this fails, two spellings of the same volume ("part1" and "part01", which
// packers use interchangeably) are downloaded twice, which is the missed-merge
// half of the same normalisation.
func TestSameVolumeSpelledDifferentlyIsAMirror(t *testing.T) {
	s := seed(PolicyFilenameAndSize, Entry{ID: "1", URL: "https://a.example/1", Name: "Film.part1.rar", Size: 100})
	got := s.Check(Entry{URL: "https://b.example/2", Name: "FILM.PART01.RAR", Size: 100})
	if got.Verdict != Mirror || got.Of.ID != "1" {
		t.Fatalf("verdict = %v of %q, want %v of entry 1", got.Verdict, got.Of.ID, Mirror)
	}
}

// If this fails, pasting the same list twice queues everything twice, whatever
// the user configured - duplicate detection is not a policy question.
func TestDuplicateURLIsRefusedUnderEveryPolicy(t *testing.T) {
	for _, p := range Policies() {
		t.Run(string(p), func(t *testing.T) {
			s := seed(p, Entry{ID: "1", URL: "https://host.example/file.rar", Name: "file.rar", Size: 10})
			// Nothing but the URL is repeated: a re-paste rarely carries the
			// name and size the resolver has since filled in.
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

// If this fails, either the same link written two equivalent ways is downloaded
// twice, or - far worse - two different links are collapsed into one because
// something that identifies the file was normalised away.
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

// TestPolicyDecidesWhatCountsAsAMirror pins each policy to the merges it
// promises and the ones it refuses, because a policy that quietly behaves like
// its neighbour makes the whole choice meaningless.
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

// TestUnknownSignalsNeverMatchEachOther is the bug that empties a paste. Links
// are staged before anything has resolved them, so name, size and hash are all
// routinely absent - and if "absent" is allowed to be a bucket key, every link
// after the first becomes a mirror of it and is silently thrown away.
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
				t.Fatalf("verdict = %v (%s), want %v - an unresolved link was thrown away",
					got.Verdict, got.Signal, NotSeen)
			}
		})
	}
}

// If this fails, a filename heuristic overrules proof: two files whose digests
// disagree are not the same file, however alike their names are.
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

	// Digests of different algorithms say nothing about each other, so they must
	// not be read as a conflict and must not block the name match.
	other := Entry{URL: "https://c.example/3", Name: "setup.exe", Size: 4096,
		Hash: Hash{Kind: "sha256", Hex: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}}
	if got := seed(PolicyFilenameOnly, have).Check(other); got.Verdict != Mirror {
		t.Fatalf("verdict = %v, want %v: an md5 and a sha256 cannot contradict each other", got.Verdict, Mirror)
	}
}

// TestNormalize pins the comparison form, including the parts that must survive
// it: the display name and the volume marker.
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
		// A four digit run is a release year far more often than it is volume
		// number 2024, and reading it as a volume would split a file off from
		// its own mirror.
		{in: "Film.2024", base: "film.2024"},
		// Placeholders, not names: comparing them merges unrelated links.
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

// If this fails, asking the set a question changes its answer, and a caller that
// checks a link twice - once to decide, once to report - gets two verdicts.
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

// If this fails, a set kept across a task being renamed or removed answers with
// entries that no longer exist, and the user is told their link is a duplicate
// of nothing.
func TestReAddReplacesAndRemoveForgets(t *testing.T) {
	s := New(PolicyFilenameOnly)
	s.Add(Entry{ID: "1", URL: "https://a.example/1", Name: "old-name.mkv"})
	s.Add(Entry{ID: "1", URL: "https://a.example/1", Name: "new-name.mkv"})
	if s.Len() != 1 {
		t.Fatalf("Len = %d after adding the same URL twice, want 1", s.Len())
	}
	// The superseded name must not keep matching, or the set reports a mirror of
	// a file that is no longer called that.
	if got := s.Check(Entry{URL: "https://b.example/2", Name: "old-name.mkv"}); got.Verdict != NotSeen {
		t.Fatalf("verdict = %v for the replaced name, want %v", got.Verdict, NotSeen)
	}
	if got := s.Check(Entry{URL: "https://b.example/2", Name: "new-name.mkv"}); got.Verdict != Mirror {
		t.Fatalf("verdict = %v for the current name, want %v", got.Verdict, Mirror)
	}

	// Removal is by URL and must take the mirror buckets with it, not just the
	// URL index.
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
	// Removing something the set never held must not disturb it.
	s.Remove("https://nowhere.example/x")
	s.Remove("")
}

// If this fails, an entry with nothing to identify it is filed anyway and every
// later entry like it collides with it.
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

// TestCheckReadsOneBucketNotTheWholeList pins the property the whole design
// exists for: a ten thousand entry download list is ordinary, and a check that
// walked it per pasted URL would turn a paste into a hang. The number of records
// a query compares is the size of the collision bucket its signature lands in,
// so that is what is asserted - measuring wall clock instead would only prove
// how fast the machine running the test is.
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

	// One key per entry rather than one shared key: if the signature ever
	// degenerated into a constant, every bucket lookup would return the whole
	// list and the map would be a scan wearing a hash table's clothes.
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

	// A candidate matching nothing must land in an empty bucket rather than in a
	// populated one it then has to walk to conclude nothing is there.
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

// If this fails, a settings file from another build either crashes the add path
// or silently picks a policy the user never chose.
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
	// An unusable policy handed to New must behave like the default rather than
	// like "off", which would disable mirror detection without saying so.
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

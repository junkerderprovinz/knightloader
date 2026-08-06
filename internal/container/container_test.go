package container

import (
	"errors"
	"strings"
	"testing"
)

// A DLC-shaped body: base64 throughout, longer than the key block, and ending
// in a key block that itself decodes. Built rather than embedded, because a
// real DLC is somebody's link list and does not belong in a repository.
func fakeDLC(payload string) string {
	if len(payload) < dlcKeyLen {
		payload = strings.Repeat("A", dlcKeyLen*2)
	}
	// 88 base64 characters that decode cleanly.
	key := "R052aURmUFhuR0JLUDJzOVRsbDRiYmYvREljWXJxME16R2ZzN2YxSnE4OXNTdEVQNUdwa09mZVZLUFFodms1Mw=="
	return payload + key
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name string
		file string
		body string
		want Kind
	}{
		{"plain link list", "links.txt", "https://example.com/a\nhttps://example.com/b", KindText},
		{"magnet list", "links.txt", "magnet:?xt=urn:btih:deadbeef", KindText},
		// The extension is a hint, not the answer: a text file saved with a
		// container's name must still be read as the text it is.
		{"text misnamed as a container", "links.dlc", "https://example.com/a", KindText},
		{"real dlc shape", "film.dlc", fakeDLC(""), KindDLC},
		// A .dlc that is not base64 is still reported as a DLC so the error
		// the user sees names the format they believe they have.
		{"broken dlc", "film.dlc", "<html>404 not found</html>", KindDLC},
		{"rsdf", "film.rsdf", "0123456789abcdef0123456789ABCDEF", KindRSDF},
		{"ccf trusts its name", "film.ccf", "\x00\x01\x02binary", KindCCF},
		{"a picture is not a container", "cover.jpg", "\xff\xd8\xff\xe0binary", KindUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Detect(tt.file, []byte(tt.body)); got != tt.want {
				t.Errorf("Detect(%q) = %q, want %q", tt.file, got, tt.want)
			}
		})
	}
}

// The encrypted formats must come back as ErrNeedsBackend and not as a
// failure. The difference is the whole design: one routes the file to the
// backend that can open it, the other tells the user their file is broken.
func TestEncryptedContainersAskForTheBackend(t *testing.T) {
	for _, tt := range []struct {
		file string
		body string
	}{
		{"film.dlc", fakeDLC("")},
		{"film.ccf", "\x00\x01binary"},
		{"film.rsdf", "0123456789abcdef"},
	} {
		_, err := Links(tt.file, []byte(tt.body))
		if !errors.Is(err, ErrNeedsBackend) {
			t.Errorf("Links(%q) = %v, want ErrNeedsBackend", tt.file, err)
		}
	}
}

// A DLC that is not a DLC must be rejected HERE, with a reason. Otherwise the
// failure surfaces inside the backend as an unexplained decryption error, and
// the actual cause — an HTML error page saved under a .dlc name, or a download
// that stopped halfway — is invisible.
func TestValidateDLCExplainsWhatIsWrong(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"too short", "AAAA", "too short"},
		{"an error page", strings.Repeat("<html>not found</html>", 20), "not a DLC"},
		{"damaged key block", strings.Repeat("A", 200) + strings.Repeat("!", dlcKeyLen), "not a DLC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDLC([]byte(tt.body))
			if err == nil {
				t.Fatalf("ValidateDLC accepted %s", tt.name)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q, so the user cannot act on it", err, tt.want)
			}
		})
	}
	if err := ValidateDLC([]byte(fakeDLC(""))); err != nil {
		t.Errorf("a well-formed DLC was rejected: %v", err)
	}
}

func TestParseText(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			"one per line",
			"https://example.com/a\nhttps://example.com/b\n",
			[]string{"https://example.com/a", "https://example.com/b"},
		},
		{
			// Pasted out of a chat client, where several links share a line.
			"several on one line",
			"https://example.com/a https://example.com/b",
			[]string{"https://example.com/a", "https://example.com/b"},
		},
		{
			// A byte-order mark leads every text file Windows writes. Left in,
			// it fuses with the first link and exactly one link per file fails.
			"byte-order mark does not eat the first link",
			"\ufeffhttps://example.com/a\nhttps://example.com/b",
			[]string{"https://example.com/a", "https://example.com/b"},
		},
		{
			"prose around the links",
			"Here you go: https://example.com/a, and also https://example.com/b.",
			[]string{"https://example.com/a", "https://example.com/b"},
		},
		{
			"duplicates collapse",
			"https://example.com/a\nhttps://example.com/a",
			[]string{"https://example.com/a"},
		},
		{
			// The whole point of requiring a scheme: a README must not become
			// a download list of its own words.
			"prose with no links",
			"This archive contains the film in 2160p.",
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseText(tt.body)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d links %v, want %d %v", len(got), got, len(tt.want), tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("link %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// An empty file and an unreadable one are different mistakes with different
// fixes, so they must not collapse into one error.
func TestEmptyAndUnknownAreDistinct(t *testing.T) {
	if _, err := Links("links.txt", nil); !errors.Is(err, ErrEmpty) {
		t.Errorf("an empty file gave %v, want ErrEmpty", err)
	}
	if _, err := Links("notes.txt", []byte("no links here at all")); !errors.Is(err, ErrEmpty) {
		t.Errorf("a text file with no links gave %v, want ErrEmpty", err)
	}
	_, err := Links("cover.jpg", []byte("\xff\xd8\xff\xe0binary"))
	if err == nil || !strings.Contains(err.Error(), "cover.jpg") {
		t.Errorf("error %v does not name the file, leaving the user nothing to act on", err)
	}
}

// The size cap exists so a hostile or mistaken upload cannot make the server
// allocate a hundred megabytes; it has to be enforced before anything is
// scanned, not after.
func TestOversizeIsRefused(t *testing.T) {
	_, err := Links("huge.txt", make([]byte, MaxBytes+1))
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("an oversize container gave %v, want a size refusal", err)
	}
}

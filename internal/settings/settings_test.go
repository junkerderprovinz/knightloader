package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidateDoesNotCreatePlaceholderFolders is the bug a live test found: the
// download folder may be a template, and validating it by creating it put
// directories literally named "<jd:date>" on the disk.
func TestValidateDoesNotCreatePlaceholderFolders(t *testing.T) {
	base := t.TempDir()
	tmpl := filepath.Join(base, "downloads", "<jd:date>", "<jd:packagename>")

	if err := Validate(tmpl); err != nil {
		t.Fatalf("a folder template was refused: %v", err)
	}
	// The fixed part is created, because that is what has to be writable.
	if _, err := os.Stat(filepath.Join(base, "downloads")); err != nil {
		t.Errorf("the fixed part of the template was not created: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(base, "downloads"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), "<") {
			t.Errorf("created a folder named %q from a placeholder", e.Name())
		}
	}
}

// TestFixedPrefix pins where a template stops being a real path.
func TestFixedPrefix(t *testing.T) {
	sep := string(filepath.Separator)
	cases := map[string]string{
		filepath.Join("/downloads", "movies"):         filepath.Join("/downloads", "movies"),
		filepath.Join("/downloads", "<jd:date>", "x"): filepath.Clean("/downloads"),
		filepath.Join("/downloads", "by-<jd:hoster>"): filepath.Clean("/downloads"),
		filepath.Join("/", "<jd:packagename>"):        sep,
	}
	for in, want := range cases {
		if got := fixedPrefix(in); got != want {
			t.Errorf("fixedPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestValidateRefusesARelativeFolder keeps a path nobody can locate from being
// accepted: it would resolve against whatever the working directory happens to
// be, which for a container is not something a user can reason about.
func TestValidateRefusesARelativeFolder(t *testing.T) {
	if err := Validate(filepath.Join("relative", "downloads")); err == nil {
		t.Error("a relative folder was accepted")
	}
	if err := Validate(""); err != nil {
		t.Errorf("the built-in default was refused: %v", err)
	}
}

// TestSanitizeKeepsLimitsUsable pins the guards that stop a saved setting from
// making the scheduler nonsensical.
func TestSanitizeKeepsLimitsUsable(t *testing.T) {
	got := sanitize(Settings{MaxConcurrent: 0, MaxPerHost: 99, SpeedLimit: -5, MaxRetries: -1})
	if got.MaxConcurrent < 1 {
		t.Errorf("MaxConcurrent = %d", got.MaxConcurrent)
	}
	if got.MaxPerHost > got.MaxConcurrent {
		t.Errorf("MaxPerHost %d exceeds MaxConcurrent %d, which would let the per-host limit do nothing",
			got.MaxPerHost, got.MaxConcurrent)
	}
	if got.SpeedLimit != 0 {
		t.Errorf("a negative speed limit became %d, want unlimited", got.SpeedLimit)
	}
	if got.MaxRetries != 0 {
		t.Errorf("negative retries became %d", got.MaxRetries)
	}
	// A relative watch folder is as unlocatable as a relative download folder.
	if w := sanitize(Settings{WatchDir: "watch"}).WatchDir; w != "" {
		t.Errorf("a relative watch folder survived as %q", w)
	}
}

// TestSanitizeRainbowPalette pins the rule that keeps saved colours out of the
// stylesheet as anything but colours. Every entry lands in a CSS custom
// property, so one that is not a plain hex triple is not a cosmetic problem.
func TestSanitizeRainbowPalette(t *testing.T) {
	full := []string{"#111111", "#222222", "#333333", "#444444",
		"#555555", "#666666", "#777777", "#888888"}

	if got := sanitize(Settings{RainbowPalette: full}).RainbowPalette; len(got) != RainbowSize {
		t.Fatalf("a complete palette was dropped: %v", got)
	}

	// All-or-nothing: seven good colours and one injection is not a palette
	// that is 87% safe, it is a palette that must not be stored.
	bad := append([]string(nil), full...)
	bad[3] = "red; background: url(http://evil/)"
	if got := sanitize(Settings{RainbowPalette: bad}).RainbowPalette; got != nil {
		t.Errorf("a palette carrying %q was kept as %v", bad[3], got)
	}

	// A short palette would leave positions undefined, which shows up as
	// colourless rows rather than as an error anybody can act on.
	if got := sanitize(Settings{RainbowPalette: full[:5]}).RainbowPalette; got != nil {
		t.Errorf("a five-colour palette survived as %v", got)
	}

	// The seed is read modulo the palette length, so it is folded on the way in
	// rather than every time it is used.
	for _, in := range []int{-3, 9, 800} {
		if s := sanitize(Settings{RainbowSeed: in}).RainbowSeed; s < 0 || s >= RainbowSize {
			t.Errorf("seed %d became %d, outside 0..%d", in, s, RainbowSize-1)
		}
	}
}

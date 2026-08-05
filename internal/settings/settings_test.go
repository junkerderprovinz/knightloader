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

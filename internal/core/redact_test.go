package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactURLKeepsTheAddressAndDropsTheSecrets(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"https://host.example/file.zip":                                      "https://host.example/file.zip",
		"https://user:s3cr'et@host.example/f?id=7":                           "https://host.example/f?id=7",
		"https://user:p@ss@host.example/f":                                   "https://host.example/f",
		"ftp://me@host.example/f":                                            "ftp://host.example/f",
		"https://host.example/dl.php?id=1&passkey=XYZ&x=2#part":              "https://host.example/dl.php?id=1&passkey=redacted&x=2#part",
		"https://host.example/f?Token=abc":                                   "https://host.example/f?Token=redacted",
		"https://host.example/@user/video":                                   "https://host.example/@user/video",
		"magnet:?xt=urn:btih:abc&tr=https%3A%2F%2Ft.example%2FPK%2Fannounce": "magnet:?xt=urn:btih:abc&tr=https%3A%2F%2Ft.example",
		"not an address":                                                     "not an address",
	} {
		if got := RedactURL(in); got != want {
			t.Errorf("RedactURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRedactTextRedactsEveryAddressInASentence(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		`Get "https://u:pw@host.example/f?apikey=K&x=1": dial tcp: refused`:                                     `Get "https://host.example/f?apikey=redacted&x=1": dial tcp: refused`,
		"https://host.example/?passkey=K":                                                                       "https://host.example/?passkey=redacted",
		"https://user:p@ss@host.example/":                                                                       "https://host.example/",
		"see (https://a:b@one.example/x) and magnet:?xt=urn:btih:abc&tr=udp%3A%2F%2Ft.example%2FPK%2Fannounce.": "see (https://one.example/x) and magnet:?xt=urn:btih:abc&tr=udp%3A%2F%2Ft.example.",
		"film.mkv":                   "film.mkv",
		"no address: user@host here": "no address: user@host here",
	} {
		if got := RedactText(in); got != want {
			t.Errorf("RedactText(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestATaskWithoutCredentialsCarriesNoSecretAnywhere plants a secret in every
// field of a task that can hold an address, the name of a link that names no
// file among them, and looks for any of them in the redacted task's JSON.
func TestATaskWithoutCredentialsCarriesNoSecretAnywhere(t *testing.T) {
	t.Parallel()
	task := Task{
		ID:               "t1",
		URL:              "https://host.example/?passkey=URLLEAK",
		Name:             "https://user:NAMELEAK@host.example/",
		Package:          "https://host.example/?token=PACKAGELEAK",
		Filename:         "https://host.example/?sid=FILENAMELEAK",
		Comment:          "from https://me:COMMENTLEAK@forum.example/thread",
		Source:           "https://reader:SOURCELEAK@page.example/thread",
		Password:         "ARCHIVELEAK",
		DownloadPassword: "DOWNLOADLEAK",
		Error:            `Get "https://u:ERRORLEAK@host.example/?apikey=ERRORKEYLEAK": dial tcp: refused`,
		ErrorParams:      map[string]string{"url": "https://host.example/?auth=PARAMLEAK"},
		RejectParams:     map[string]string{"url": "https://u:REJECTLEAK@host.example/"},
		SkipReason:       "already there as https://host.example/?pass=SKIPLEAK",
		SkipParams:       map[string]string{"url": "https://host.example/?secret=SKIPPARAMLEAK"},
		Note:             "waiting for https://u:NOTELEAK@host.example/",
		Trackers:         []string{"https://tracker.example/TRACKERLEAK/announce"},
	}
	b, err := json.Marshal(task.WithoutCredentials())
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{
		"URLLEAK", "NAMELEAK", "PACKAGELEAK", "FILENAMELEAK", "COMMENTLEAK", "SOURCELEAK", "ARCHIVELEAK",
		"DOWNLOADLEAK", "ERRORLEAK", "ERRORKEYLEAK", "PARAMLEAK", "REJECTLEAK", "SKIPLEAK", "SKIPPARAMLEAK",
		"NOTELEAK", "TRACKERLEAK",
	} {
		if strings.Contains(string(b), leak) {
			t.Errorf("the redacted task still carries %q", leak)
		}
	}
	if task.ErrorParams["url"] != "https://host.example/?auth=PARAMLEAK" {
		t.Error("WithoutCredentials() changed the error parameters of the task it was called on")
	}
}

func TestATaskWithoutCredentialsKeepsEverythingElse(t *testing.T) {
	t.Parallel()
	task := Task{
		ID: "t1", Name: "film.mkv", Password: "archive", DownloadPassword: "hoster",
		URL: "https://u:pw@host.example/f", Trackers: []string{"udp://tracker.example:1337/PK/announce"},
	}
	got := task.WithoutCredentials()
	if got.Password != "" || got.DownloadPassword != "" || got.URL != "https://host.example/f" ||
		len(got.Trackers) != 1 || got.Trackers[0] != "udp://tracker.example:1337" {
		t.Errorf("WithoutCredentials() = %+v", got)
	}
	if got.ID != "t1" || got.Name != "film.mkv" {
		t.Errorf("WithoutCredentials() lost the task's identity: %+v", got)
	}
	if task.Password != "archive" || task.Trackers[0] != "udp://tracker.example:1337/PK/announce" {
		t.Error("WithoutCredentials() changed the task it was called on")
	}
}

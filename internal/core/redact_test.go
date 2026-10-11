package core

import "testing"

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

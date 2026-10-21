package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadLanguageTakesTheInstallersChoice(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	cases := []struct {
		name string
		path string
		want string
	}{
		{"windows settings", write("settings.json", "{\"autoUpdate\":true,\"language\":\"de\"}\r\n"), "de"},
		{"linux state", write("install.json", `{"version":"2.2.0","startMenu":true,"language":"fr"}`), "fr"},
		{"no language", write("empty.json", `{"autoUpdate":false}`), ""},
		{"broken file", write("broken.json", "not json"), ""},
		{"missing file", filepath.Join(dir, "missing.json"), ""},
	}
	for _, c := range cases {
		if got := readLanguage(c.path); got != c.want {
			t.Errorf("%s: readLanguage = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestStartURLCarriesTheLanguageOnlyWhenThereIsOne(t *testing.T) {
	if got := startURL(""); got != "/" {
		t.Errorf("startURL(\"\") = %q, want /", got)
	}
	if got := startURL("de"); got != "/?lang=de" {
		t.Errorf("startURL(de) = %q, want /?lang=de", got)
	}
	if got := startURL("a&b=c"); got != "/?lang=a%26b%3Dc" {
		t.Errorf("startURL(a&b=c) = %q, want the value escaped", got)
	}
}

package main

import (
	"encoding/json"
	"net/url"
	"os"
)

// installerLanguage is the language the installer was set to, or "" when it
// left none. The page starts in it until somebody picks a language there; see
// detect in web/src/lib/i18n.tsx, which also checks the code.
func installerLanguage() string {
	return readLanguage(installerChoices())
}

// readLanguage reads the language from the installer's file at path. A missing
// or broken file reads as "".
func readLanguage(path string) string {
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var f struct {
		Language string `json:"language"`
	}
	if json.Unmarshal(body, &f) != nil {
		return ""
	}
	return f.Language
}

// startURL is the main window's first page, carrying lang for the page to
// take while it has no language of its own.
func startURL(lang string) string {
	if lang == "" {
		return "/"
	}
	return "/?" + url.Values{"lang": {lang}}.Encode()
}

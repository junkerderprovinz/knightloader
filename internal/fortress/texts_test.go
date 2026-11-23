package fortress

import (
	"regexp"
	"strings"
	"testing"
)

func TestSayFillsPlaceholders(t *testing.T) {
	got := say("en", "fortress.need.failed", map[string]string{"name": "film.mkv"})
	if got != "film.mkv failed" {
		t.Errorf("say = %q, want %q", got, "film.mkv failed")
	}
}

func TestSayPicksTheLanguage(t *testing.T) {
	for tag, want := range map[string]string{
		"de":    "film.mkv ist fehlgeschlagen",
		"de-AT": "film.mkv ist fehlgeschlagen",
		"DE_ch": "film.mkv ist fehlgeschlagen",
		"":      "film.mkv failed",
		"tlh":   "film.mkv failed",
	} {
		if got := say(tag, "fortress.need.failed", map[string]string{"name": "film.mkv"}); got != want {
			t.Errorf("say(%q) = %q, want %q", tag, got, want)
		}
	}
}

func TestLanguageFoldsNorwegian(t *testing.T) {
	for _, tag := range []string{"nb", "nn-NO", "no"} {
		if got := language(tag); got != "no" {
			t.Errorf("language(%q) = %q, want no", tag, got)
		}
	}
}

func TestSayFallsBackToEnglishThenToTheKey(t *testing.T) {
	c := texts{"en": {"a": "English"}, "de": {}}
	if got := c.say("de", "a", nil); got != "English" {
		t.Errorf("a key German lacks = %q, want the English text", got)
	}
	if got := c.say("de", "b", nil); got != "b" {
		t.Errorf("a key nobody has = %q, want the key", got)
	}
}

func TestSayLeavesAnUnknownPlaceholder(t *testing.T) {
	c := texts{"en": {"a": "{name} and {other}"}}
	if got := c.say("en", "a", map[string]string{"name": "x"}); got != "x and {other}" {
		t.Errorf("say = %q", got)
	}
}

func TestEveryLanguageHasEveryText(t *testing.T) {
	en := catalog()["en"]
	if len(catalog()) < 42 {
		t.Errorf("texts.json has %d languages, want 42", len(catalog()))
	}
	placeholder := regexp.MustCompile(`\{[a-zA-Z]+\}`)
	for lang, texts := range catalog() {
		for key, source := range en {
			got, ok := texts[key]
			if !ok || strings.TrimSpace(got) == "" {
				t.Errorf("%s has no text for %s", lang, key)
				continue
			}
			for _, p := range placeholder.FindAllString(source, -1) {
				if !strings.Contains(got, p) {
					t.Errorf("%s %s lacks %s", lang, key, p)
				}
			}
		}
	}
}

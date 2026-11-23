package fortress

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)

// texts.json is generated from the web locales by
// web/scripts/gen-fortress-texts.mjs, so a text is translated once for the
// interface and the status alike.
//
//go:embed texts.json
var textsJSON []byte

// texts maps a language to its texts by key.
type texts map[string]map[string]string

var catalog = sync.OnceValue(func() texts {
	var c texts
	if err := json.Unmarshal(textsJSON, &c); err != nil {
		panic("fortress: texts.json: " + err.Error())
	}
	return c
})

// say is key in the language tag names, with every {name} replaced from args.
func say(tag, key string, args map[string]string) string {
	return catalog().say(tag, key, args)
}

func (c texts) say(tag, key string, args map[string]string) string {
	s, ok := c[c.language(tag)][key]
	if !ok {
		if s, ok = c["en"][key]; !ok {
			s = key
		}
	}
	for name, value := range args {
		s = strings.ReplaceAll(s, "{"+name+"}", value)
	}
	return s
}

// language is the catalog's language for a tag such as "pt-BR", or English.
func language(tag string) string { return catalog().language(tag) }

func (c texts) language(tag string) string {
	base, _, _ := strings.Cut(strings.ToLower(strings.ReplaceAll(tag, "_", "-")), "-")
	if base == "nb" || base == "nn" {
		base = "no"
	}
	if _, ok := c[base]; ok {
		return base
	}
	return "en"
}

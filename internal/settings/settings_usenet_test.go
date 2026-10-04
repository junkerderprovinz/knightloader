package settings

import "testing"

func TestAUsenetHostIsANameOrAnAddress(t *testing.T) {
	for raw, want := range map[string]string{
		"News.Example.com ":    "news.example.com",
		"reader_2.example":     "reader_2.example",
		"192.0.2.7":            "192.0.2.7",
		"::1":                  "::1",
		"[2001:db8::1]":        "2001:db8::1",
		"news.example.com/x@y": "",
		"a.exa\nmple":          "",
		"news.example.com:563": "",
		"[news.example.com]":   "",
		"[::1":                 "",
		"":                     "",
	} {
		if got := UsenetHost(raw); got != want {
			t.Errorf("UsenetHost(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestARowThatCouldNeverConnectIsDropped(t *testing.T) {
	n := sanitizeUsenet(Settings{UsenetServers: []UsenetServer{
		{ID: "bad", Host: "a.exa\nmple"},
		{ID: "v6", Host: "[::1]"},
	}})
	if len(n.UsenetServers) != 1 || n.UsenetServers[0].Host != "::1" {
		t.Fatalf("rows = %+v, want only the IPv6 one, without its brackets", n.UsenetServers)
	}
}

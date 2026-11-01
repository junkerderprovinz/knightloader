package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

func TestALinkToAHostersOtherDomainShowsItsMainOne(t *testing.T) {
	for uri, want := range map[string]string{
		"https://rg.to/file/abc":              "rapidgator.net",
		"https://www.rapidgator.net/file/abc": "rapidgator.net",
		"https://host.example/a.bin":          "host.example",
	} {
		if got := torrentHost(uri); got != want {
			t.Errorf("host of %s = %q, want %q", uri, got, want)
		}
	}
}

func TestAStoredLinkComesBackUnderTheHostersMainDomain(t *testing.T) {
	f := newBootFixture(t, nil, core.Task{
		ID: "part1", URL: "https://rg.to/file/abc", Name: "https://rg.to/file/abc", Host: "rg.to",
		Status: core.StatusCollected, Enabled: true,
	})
	a := f.boot(t)
	if got := taskOf(t, a, "part1").Host; got != "rapidgator.net" {
		t.Errorf("host after a restart = %q, want rapidgator.net", got)
	}
	stored, err := a.Store.All()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stored {
		if s.ID == "part1" && s.Host != "rapidgator.net" {
			t.Errorf("the store still says %q, so the next start would read it again", s.Host)
		}
	}
}

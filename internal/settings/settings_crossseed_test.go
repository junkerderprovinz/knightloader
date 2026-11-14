package settings

import (
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/reconnect"
)

func crossSeedWithSecrets() CrossSeed {
	return CrossSeed{
		Enabled: true,
		RED:     GazelleSite{APIKey: "red-key", Announce: "https://flacsfor.me/red-passkey/announce"},
		OPS:     GazelleSite{APIKey: "ops-key", Announce: "https://home.opsfet.ch/ops-passkey/announce"},
		Indexers: []TorznabIndexer{
			{Name: "Prowlarr 1", URL: "http://prowlarr:9696/1/api", APIKey: "prowlarr-key"},
		},
		LinkMode: LinkHardlink,
		LinkDir:  "/data/cross-seed",
		Daemon:   CrossSeedDaemon{URL: "http://cross-seed:2468", APIKey: "daemon-key"},
	}
}

// The keys and the announce addresses with their passkeys never reach a
// browser, come back from the mask on a save, and an indexer's or the
// daemon's key does not follow its address to another host.
func TestCrossSeedSecretsStayOnTheServer(t *testing.T) {
	s, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	n := s.Get()
	n.Torrent.CrossSeed = crossSeedWithSecrets()
	if _, err := s.Set(n); err != nil {
		t.Fatal(err)
	}
	shown := s.Get().Redacted()
	c := shown.Torrent.CrossSeed
	for name, v := range map[string]string{
		"RED key": c.RED.APIKey, "RED announce": c.RED.Announce,
		"OPS key": c.OPS.APIKey, "OPS announce": c.OPS.Announce,
		"indexer key": c.Indexers[0].APIKey, "daemon key": c.Daemon.APIKey,
	} {
		if v != reconnect.RedactedPassword {
			t.Errorf("the browser is shown the %s as %q", name, v)
		}
	}

	saved, err := s.Set(shown)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := saved.Torrent.CrossSeed, crossSeedWithSecrets(); got.RED != want.RED || got.OPS != want.OPS ||
		got.Indexers[0] != want.Indexers[0] || got.Daemon != want.Daemon {
		t.Errorf("saving the page untouched changed the secrets to %+v", got)
	}

	shown.Torrent.CrossSeed.Indexers[0].URL = "http://elsewhere:9696/1/api"
	shown.Torrent.CrossSeed.Daemon.URL = "http://elsewhere:2468"
	saved, err = s.Set(shown)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Torrent.CrossSeed.Indexers[0].APIKey != "" {
		t.Error("the indexer's key followed its address to another host")
	}
	if saved.Torrent.CrossSeed.Daemon.APIKey != "" {
		t.Error("the daemon's key followed its address to another host")
	}
	if saved.Torrent.CrossSeed.RED.APIKey != "red-key" {
		t.Error("the RED key was lost with an indexer's address")
	}
}

// An empty key or announce address stays empty, which is how it is cleared.
func TestACrossSeedKeyIsClearedByEmptyingIt(t *testing.T) {
	s, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	n := s.Get()
	n.Torrent.CrossSeed = crossSeedWithSecrets()
	if _, err := s.Set(n); err != nil {
		t.Fatal(err)
	}
	shown := s.Get().Redacted()
	shown.Torrent.CrossSeed.RED = GazelleSite{}
	saved, err := s.Set(shown)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Torrent.CrossSeed.RED != (GazelleSite{}) {
		t.Errorf("RED kept %+v after both fields were emptied", saved.Torrent.CrossSeed.RED)
	}
}

func TestCrossSeedSettingsAreTidiedOnLoad(t *testing.T) {
	got := loadFrom(t, `{"torrent":{"crossSeed":{
		"linkMode":"copy",
		"linkDir":"  /data/links  ",
		"category":" cross-seed ",
		"red":{"apiKey":" k ","announce":" https://flacsfor.me/x/announce "},
		"indexers":[
			{"name":"","url":" http://prowlarr:9696/3/api ","apiKey":"a"},
			{"name":" ","url":" ","apiKey":""},
			{"name":"Jackett","url":"http://jackett:9117/api/v2.0/indexers/all/results/torznab","apiKey":"b"}
		]}}}`).Torrent.CrossSeed
	if got.LinkMode != LinkNone {
		t.Errorf("a link mode this build does not know became %q, want none", got.LinkMode)
	}
	if got.LinkDir != "/data/links" || got.Category != "cross-seed" {
		t.Errorf("link folder %q and category %q kept their spaces", got.LinkDir, got.Category)
	}
	if got.RED.APIKey != "k" || got.RED.Announce != "https://flacsfor.me/x/announce" {
		t.Errorf("RED kept its spaces: %+v", got.RED)
	}
	if len(got.Indexers) != 2 {
		t.Fatalf("got %d indexers, want the blank row dropped: %+v", len(got.Indexers), got.Indexers)
	}
	if got.Indexers[0].Name != "prowlarr" || got.Indexers[0].URL != "http://prowlarr:9696/3/api" {
		t.Errorf("an indexer without a name is %+v, want it named after its host", got.Indexers[0])
	}
}

func TestCrossSeedIsOffOnAFreshInstall(t *testing.T) {
	c := Defaults().Torrent.CrossSeed
	if c.Enabled || c.LinkMode != LinkNone {
		t.Errorf("a fresh install cross-seeds as %+v", c)
	}
}

func TestTheDiagnosticsBundleHoldsNoCrossSeedSecret(t *testing.T) {
	s := Defaults()
	s.Torrent.CrossSeed = crossSeedWithSecrets()
	s.Torrent.CrossSeed.Indexers[0].URL = "http://prowlarr:9696/1/api?apikey=url-key"
	out := s.Redacted().Scrubbed().Torrent.CrossSeed
	all := strings.Join([]string{out.RED.APIKey, out.RED.Announce, out.OPS.APIKey, out.OPS.Announce,
		out.Indexers[0].URL, out.Indexers[0].APIKey, out.Daemon.URL, out.Daemon.APIKey}, " ")
	for _, secret := range []string{"red-key", "ops-key", "red-passkey", "ops-passkey", "prowlarr-key", "url-key", "daemon-key"} {
		if strings.Contains(all, secret) {
			t.Errorf("the bundle holds %q: %s", secret, all)
		}
	}
}

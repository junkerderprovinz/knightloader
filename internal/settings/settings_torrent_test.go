package settings

import "testing"

// TestTorrentDefaultsMirrorGopeedsOwn pins the numbers in defaultTorrent
// against the vendored source they were read from (see the field doc
// comments on Torrent), so a dependency bump that changes gopeed's own
// DefaultConfig is a failing test here rather than a silent drift between
// what this app claims its default is and what gopeed's actually does.
func TestTorrentDefaultsMirrorGopeedsOwn(t *testing.T) {
	d := Defaults()
	if d.Torrent.SeedRatioTarget != 1.0 {
		t.Errorf("SeedRatioTarget = %v, want 1.0 (gopeed's own DefaultConfig)", d.Torrent.SeedRatioTarget)
	}
	if d.Torrent.SeedDurationSeconds != 7200 {
		t.Errorf("SeedDurationSeconds = %d, want 7200 (gopeed's own SeedTime: 120*60)", d.Torrent.SeedDurationSeconds)
	}
	if d.Torrent.Port != 0 {
		t.Errorf("Port = %d, want 0 (let gopeed/the OS pick, matching gopeed's own ListenPort: 0)", d.Torrent.Port)
	}
	if d.Torrent.UploadLimitKiBs != 0 {
		t.Errorf("UploadLimitKiBs = %d, want 0 (unlimited)", d.Torrent.UploadLimitKiBs)
	}
	if !d.Torrent.DHTEnabled {
		t.Error("DHTEnabled defaults to false, want true (ordinary public-swarm behaviour)")
	}
	if !d.Torrent.PEXEnabled {
		t.Error("PEXEnabled defaults to false, want true (ordinary public-swarm behaviour)")
	}
}

// TestSanitizeTorrentFloorsNegativesAndBadPort pins the one rule this domain
// has: nothing typed into a number field can produce a value with no honest
// meaning. It deliberately starts from non-default settings rather than
// Defaults(), the same as TestSanitizeKeepsLimitsUsable does for the older
// fields, so this is proven against an arbitrary bad document and not just
// the one this package happens to write out today.
func TestSanitizeTorrentFloorsNegativesAndBadPort(t *testing.T) {
	cases := []struct {
		name string
		in   Torrent
		want Torrent
	}{
		{
			name: "negative ratio and duration and upload limit are floored to zero",
			in:   Torrent{SeedRatioTarget: -2.5, SeedDurationSeconds: -100, UploadLimitKiBs: -50},
			want: Torrent{SeedRatioTarget: 0, SeedDurationSeconds: 0, UploadLimitKiBs: 0},
		},
		{
			name: "a negative port collapses to 0 (let the OS pick)",
			in:   Torrent{Port: -1},
			want: Torrent{Port: 0},
		},
		{
			name: "a port above 65535 collapses to 0",
			in:   Torrent{Port: 70000},
			want: Torrent{Port: 0},
		},
		{
			name: "a legitimate port is left exactly as typed",
			in:   Torrent{Port: 51413},
			want: Torrent{Port: 51413},
		},
		{
			name: "positive values of every field survive untouched",
			in:   Torrent{SeedRatioTarget: 2.0, SeedDurationSeconds: 3600, UploadLimitKiBs: 512, Port: 6881},
			want: Torrent{SeedRatioTarget: 2.0, SeedDurationSeconds: 3600, UploadLimitKiBs: 512, Port: 6881},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeTorrent(Settings{Torrent: c.in}).Torrent
			if got != c.want {
				t.Errorf("sanitizeTorrent(%+v) = %+v, want %+v", c.in, got, c.want)
			}
		})
	}
}

// TestEffectiveDHTPEXPrivateAlwaysWins is decision 5 of the grilling, as a
// table: whatever the instance default says, a private torrent's own
// EffectiveDHT/EffectivePEX is always false, and a non-private torrent's is
// exactly the instance default, in both directions - a setting that is off
// must not somehow read as on for an ordinary public torrent either.
func TestEffectiveDHTPEXPrivateAlwaysWins(t *testing.T) {
	cases := []struct {
		name              string
		dht, pex, private bool
		wantDHT, wantPEX  bool
	}{
		{"defaults, public torrent: both follow the setting", true, true, false, true, true},
		{"both enabled, private torrent: both forced off", true, true, true, false, false},
		{"both disabled, public torrent: both stay off", false, false, false, false, false},
		{"both disabled, private torrent: still off, not re-enabled by privacy", false, false, true, false, false},
		{"DHT only, private torrent: forced off despite the setting", true, false, true, false, false},
		{"PEX only, public torrent: PEX on, DHT off, independently", false, true, false, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := Torrent{DHTEnabled: c.dht, PEXEnabled: c.pex}
			if got := tr.EffectiveDHT(c.private); got != c.wantDHT {
				t.Errorf("EffectiveDHT(private=%v) with DHTEnabled=%v = %v, want %v", c.private, c.dht, got, c.wantDHT)
			}
			if got := tr.EffectivePEX(c.private); got != c.wantPEX {
				t.Errorf("EffectivePEX(private=%v) with PEXEnabled=%v = %v, want %v", c.private, c.pex, got, c.wantPEX)
			}
		})
	}
}

package settings

import (
	"reflect"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
)

// TestDefaultsYtdlpIsTheZeroValue pins the promise settings.go's own comment
// makes: an install that never opens the resolver options page downloads
// exactly as it always has, because Ytdlp.Defaults() is the zero value.
func TestDefaultsYtdlpIsTheZeroValue(t *testing.T) {
	if got := Defaults().Ytdlp; got != (ytdlp.Options{}) {
		t.Errorf("Defaults().Ytdlp = %+v, want the zero value", got)
	}
}

// TestSanitizeResolversFoldsUnknownQuality is the same guard
// TestSanitizeKeepsWhatOnlyTheUserCanFix already runs for MirrorPolicy and
// CollisionPolicy, extended to the new sub-struct: a value only the API can
// refuse (see routes_settings.go's validateRows) still must not be
// discarded outright by sanitize, but an enum with no matching case folds
// onto its safe default rather than being stored unusable.
func TestSanitizeResolversFoldsUnknownQuality(t *testing.T) {
	in := Defaults()
	in.Ytdlp.Quality = "does-not-exist"
	got := sanitize(in)
	if got.Ytdlp.Quality != ytdlp.QualityBest {
		t.Errorf("Ytdlp.Quality = %q, want %q", got.Ytdlp.Quality, ytdlp.QualityBest)
	}
}

// TestYtdlpOptionsSurviveTheStoreRoundTrip is the settings form's actual
// journey for this field: saved, reloaded from disk, still there.
func TestYtdlpOptionsSurviveTheStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := Defaults()
	n.Ytdlp = ytdlp.Options{
		Quality:        ytdlp.Quality1080p,
		SubtitleLangs:  "en,de",
		SubtitleAuto:   true,
		Playlist:       true,
		OutputTemplate: "%(uploader)s/%(title)s.%(ext)s",
	}
	saved, err := st.Set(n)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Ytdlp.Quality != ytdlp.Quality1080p || saved.Ytdlp.SubtitleLangs != "en,de" {
		t.Fatalf("Set() returned %+v, want the values just saved", saved.Ytdlp)
	}

	reloaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	back := reloaded.Get().Ytdlp
	if back != saved.Ytdlp {
		t.Errorf("after reload Ytdlp = %+v, want %+v", back, saved.Ytdlp)
	}
}

// TestYtdlpOptionsCarryNoSecretRedactedIsAPlainCopy guards against Ytdlp
// ever being added to Settings.Redacted without anyone noticing it needs
// to be: unlike Reconnect and Connections, nothing in Options is a
// credential, so a save must not silently need a merge-back for it. If a
// secret-bearing field is ever added to Options, this test is exactly the
// one that should start failing.
func TestYtdlpOptionsCarryNoSecretRedactedIsAPlainCopy(t *testing.T) {
	n := Defaults()
	n.Ytdlp.CustomFormat = "bestvideo+bestaudio"
	if got := n.Redacted().Ytdlp; got != n.Ytdlp {
		t.Errorf("Redacted().Ytdlp = %+v, want it unchanged from %+v", got, n.Ytdlp)
	}
}

// TestResolverOrderIsCleanedNotWhitelisted pins both halves of
// cleanResolverOrder at once: what it removes (blanks, repeats, casing) and
// what it deliberately leaves alone (an id this package has never heard of).
//
// The second half is the one worth a test. It is tempting to "fix" an unknown
// id by dropping it, and doing so would silently rewrite somebody's arranged
// order the moment they removed the key for a service they had ranked - see
// the field's own comment in settings.go.
func TestResolverOrderIsCleanedNotWhitelisted(t *testing.T) {
	got := sanitizeResolvers(Settings{
		ResolverOrder: []string{" TorBox ", "jd", "torbox", "", "   ", "a-service-this-build-never-heard-of"},
	}).ResolverOrder
	want := []string{"torbox", "jd", "a-service-this-build-never-heard-of"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ResolverOrder = %v, want %v (trimmed and lower-cased, repeat collapsed, blanks dropped, unknown id KEPT)", got, want)
	}
}

// TestEmptyResolverOrderBecomesNil is what the "Automatisch" button relies on:
// an order that is empty however it got there reads the same on disk, so
// "there is no hand order" is one state and not two.
func TestEmptyResolverOrderBecomesNil(t *testing.T) {
	for _, in := range [][]string{{}, {"", "  "}, nil} {
		if got := sanitizeResolvers(Settings{ResolverOrder: in}).ResolverOrder; got != nil {
			t.Errorf("sanitizeResolvers(%q).ResolverOrder = %#v, want nil", in, got)
		}
	}
}

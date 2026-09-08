package settings

// The stored addresses, and the one reference into them a drawer carries. Every
// claim below is one somebody has to be able to rely on before pointing a drawer
// at a media server: that an empty table changes nothing, that a table an older
// install has never seen loads as empty rather than as null, that a drawer
// pointing at nothing is refused where it can still be fixed, and that a drawer's
// reference survives a save that has nothing to do with it.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/mediahook"
)

func jellyfin() mediahook.Hook {
	return mediahook.Hook{ID: "jellyfin", Name: "Jellyfin", URL: "http://jellyfin.lan:8096/Library/Refresh", Method: mediahook.MethodPost, HeaderName: "X-Emby-Token", WaitSeconds: 60}
}

func plex() mediahook.Hook {
	return mediahook.Hook{ID: "plex", URL: "https://plex.example.org/library/sections/3/refresh", Method: mediahook.MethodGet}
}

// TestNoAddressesChangesNothing is the promise a fresh install and every
// existing one rely on: this key is empty, so nothing is called, and the
// document behaves exactly as it did before the key existed.
func TestNoAddressesChangesNothing(t *testing.T) {
	s := Defaults()
	if len(s.MediaHooks) != 0 {
		t.Fatalf("a fresh install ships %d stored addresses", len(s.MediaHooks))
	}
	if err := s.ValidateMediaHooks(); err != nil {
		t.Fatalf("an empty table was refused: %v", err)
	}
	// And a drawer with no opinion calls nothing, which is what every drawer
	// does until somebody changes it.
	s.Categories = []Category{{ID: "serien"}}
	if got := s.NotifyHookFor("serien"); got != "" {
		t.Errorf("an untouched drawer calls %q", got)
	}
	if err := s.ValidateMediaHooks(); err != nil {
		t.Fatalf("a drawer that calls nothing was refused: %v", err)
	}
}

// TestASettingsFileFromBeforeThisKeyLoadsAsEmpty. The field carries no omitempty
// so the server always SENDS it, but a document written by an older build simply
// does not have it, and decoding one has to answer an empty list rather than
// anything a page would have to guard against.
func TestASettingsFileFromBeforeThisKeyLoadsAsEmpty(t *testing.T) {
	var s Settings
	if err := json.Unmarshal([]byte(`{"downloadDir":"/downloads","categories":[{"id":"serien"}]}`), &s); err != nil {
		t.Fatal(err)
	}
	if len(s.MediaHooks) != 0 {
		t.Fatalf("MediaHooks = %v, want none", s.MediaHooks)
	}
	if s.Categories[0].Notify != "" {
		t.Errorf("a drawer written before this field calls %q", s.Categories[0].Notify)
	}
	// And it is written back as a list rather than as null, because the
	// frontend has no way to type a field that is sometimes simply absent.
	out, err := json.Marshal(sanitize(s))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"mediaHooks":[]`) && !strings.Contains(string(out), `"mediaHooks":null`) {
		t.Errorf("the key is missing from the encoded document: %s", out)
	}
}

func TestValidateMediaHooksRefusesWhatCannotWork(t *testing.T) {
	for _, c := range []struct {
		name string
		of   func() Settings
		want string
	}{
		{
			"two addresses under one name",
			func() Settings {
				s := Defaults()
				s.MediaHooks = []mediahook.Hook{jellyfin(), {ID: "JELLYFIN", URL: "http://x.lan/", Method: mediahook.MethodGet}}
				return s
			},
			"repeats",
		},
		{
			"a row the address rules refuse",
			func() Settings {
				s := Defaults()
				bad := jellyfin()
				bad.URL = "/Library/Refresh"
				s.MediaHooks = []mediahook.Hook{bad}
				return s
			},
			"http://",
		},
		{
			"a drawer pointing at an address that is not there",
			func() Settings {
				s := Defaults()
				s.MediaHooks = []mediahook.Hook{jellyfin()}
				s.Categories = []Category{{ID: "serien", Notify: "plex"}}
				return s
			},
			"plex",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := c.of().ValidateMediaHooks()
			if err == nil {
				t.Fatal("the document was accepted")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("the refusal is %q, which does not name %q", err, c.want)
			}
		})
	}
}

// TestADanglingDrawerReferenceIsRefusedAndNotCleared is the asymmetry this
// feature borrows from ValidateCategories, and it is worth pinning both halves.
//
// A drawer pointing at an address that is not stored calls NOTHING, silently, on
// every package ever filed in it - which is the exact failure the feature exists
// to end, so it is refused at the door. What must NOT happen is the sanitiser
// quietly clearing it instead: that would undo somebody's setting because they
// hand-edited their addresses list, with nothing anywhere to say so.
func TestADanglingDrawerReferenceIsRefusedAndNotCleared(t *testing.T) {
	s := Defaults()
	s.Categories = []Category{{ID: "serien", Notify: "Jellyfin"}}

	cleaned := sanitize(s)
	if got := cleaned.Categories[0].Notify; got != "jellyfin" {
		t.Fatalf("the drawer's reference is %q after a save, want it folded and kept", got)
	}
	if err := cleaned.ValidateMediaHooks(); err == nil {
		t.Error("a drawer pointing at nothing was accepted")
	}

	cleaned.MediaHooks = []mediahook.Hook{jellyfin()}
	if err := cleaned.ValidateMediaHooks(); err != nil {
		t.Errorf("the same drawer was refused once the address existed: %v", err)
	}
}

// TestAReferenceThatCouldNeverBeAnIDIsDropped. HookID answers empty for a
// spelling no address can ever be stored under, and empty is the honest reading:
// there is nothing to point at and nothing to fix.
func TestAReferenceThatCouldNeverBeAnIDIsDropped(t *testing.T) {
	s := Defaults()
	s.Categories = []Category{{ID: "serien", Notify: "jellyfin lan"}}
	if got := sanitize(s).Categories[0].Notify; got != "" {
		t.Errorf("Notify = %q, want it dropped", got)
	}
}

func TestSanitizeMediaHooksFoldsAndBounds(t *testing.T) {
	s := Defaults()
	s.MediaHooks = []mediahook.Hook{
		{ID: "  Jellyfin ", URL: " http://jellyfin.lan:8096/x ", Method: "post", WaitSeconds: -5},
	}
	got := sanitize(s).MediaHooks
	if len(got) != 1 {
		t.Fatalf("MediaHooks = %v, want one row", got)
	}
	if got[0].ID != "jellyfin" || got[0].Method != mediahook.MethodPost || got[0].WaitSeconds != 0 {
		t.Errorf("the row was not folded on the way to disk: %+v", got[0])
	}
}

func TestTheLookupsAnswerWhatTheRoutesAsk(t *testing.T) {
	s := Defaults()
	s.MediaHooks = []mediahook.Hook{jellyfin(), plex()}
	s.Categories = []Category{
		{ID: "serien", Notify: "jellyfin"},
		{ID: "filme", Notify: "jellyfin"},
		{ID: "musik"},
		{Name: "Hörspiele", Notify: "plex"},
	}

	if _, ok := s.MediaHookFor("JELLYFIN"); !ok {
		t.Error("MediaHookFor does not fold the id")
	}
	if _, ok := s.MediaHookFor("kodi"); ok {
		t.Error("MediaHookFor found an address that is not stored")
	}
	if got := s.NotifyHookFor("filme"); got != "jellyfin" {
		t.Errorf("NotifyHookFor(filme) = %q", got)
	}
	if got := s.NotifyHookFor("musik"); got != "" {
		t.Errorf("NotifyHookFor(musik) = %q, want nothing", got)
	}
	// Sorted, and by KEY rather than by name: the sentence this feeds is "set
	// these drawers to call nothing first", and only the key is a handle the
	// person can act on.
	users := s.MediaHookUsers("jellyfin")
	if len(users) != 2 || users[0] != "filme" || users[1] != "serien" {
		t.Errorf("MediaHookUsers(jellyfin) = %v, want [filme serien]", users)
	}
	// A drawer with only a name still names itself, through the key its name
	// would be stored under.
	if users := s.MediaHookUsers("plex"); len(users) != 1 || users[0] != "hörspiele" {
		t.Errorf("MediaHookUsers(plex) = %v", users)
	}
	if got := s.MediaHookUsers("kodi"); got != nil {
		t.Errorf("MediaHookUsers for an address nothing points at = %v, want none", got)
	}
}

// TestADrawersOtherFieldsSurviveTheNewOne. The reference is one more field on a
// struct several pages write, and a save from the Categories page must not lose
// it any more than it loses the folder.
func TestADrawersOtherFieldsSurviveTheNewOne(t *testing.T) {
	s := Defaults()
	s.MediaHooks = []mediahook.Hook{jellyfin()}
	s.Categories = []Category{{ID: "serien", Name: "Serien", Dir: absPath("media", "serien"), Collision: "skip", Notify: "jellyfin"}}
	got := sanitize(s).Categories[0]
	if got.Notify != "jellyfin" || got.Collision != "skip" || got.Name != "Serien" {
		t.Errorf("a drawer lost something on the way to disk: %+v", got)
	}
}

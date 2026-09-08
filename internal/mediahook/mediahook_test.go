package mediahook

import (
	"reflect"
	"strings"
	"testing"
)

func TestHookIDTakesOnlyWhatAnIDMayHold(t *testing.T) {
	for _, c := range []struct {
		in, want string
	}{
		{"jellyfin", "jellyfin"},
		{"  Jellyfin  ", "jellyfin"},
		{"plex.lan_2", "plex.lan_2"},
		{"kodi-wohnzimmer", "kodi-wohnzimmer"},
		// Refused rather than folded. CategoryID would turn this into
		// "jellyfin-lan", which is right for a key derived from a name and wrong
		// for one somebody typed: the person would be left wondering which of the
		// two spellings their drawer points at.
		{"jellyfin lan", ""},
		{"jellyfin/lan", ""},
		{"hörspiele", ""},
		{"", ""},
		{"   ", ""},
		{strings.Repeat("a", MaxHookID), strings.Repeat("a", MaxHookID)},
		{strings.Repeat("a", MaxHookID+1), ""},
	} {
		if got := HookID(c.in); got != c.want {
			t.Errorf("HookID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// good is the row every case below starts from, so each test changes exactly the
// one field it is about.
func good() Hook {
	return Hook{ID: "jellyfin", URL: "http://jellyfin.lan:8096/Library/Refresh", Method: MethodPost, HeaderName: "X-Emby-Token", WaitSeconds: 60}
}

func TestValidateRefusesWhatCannotBeCalled(t *testing.T) {
	for _, c := range []struct {
		name string
		edit func(*Hook)
		want string // a fragment the refusal has to name
	}{
		{"a relative address", func(h *Hook) { h.URL = "/Library/Refresh" }, "http://"},
		{"a scheme this build does not call", func(h *Hook) { h.URL = "ftp://jellyfin.lan/refresh" }, "http://"},
		{"an address with no host", func(h *Hook) { h.URL = "http:///Library/Refresh" }, "no host"},
		{"a method this build does not send", func(h *Hook) { h.Method = "DELETE" }, "GET"},
		{"a whole header line pasted into the name", func(h *Hook) { h.HeaderName = "X-Emby-Token: abc" }, "header name"},
		{"a name nothing could point at", func(h *Hook) { h.ID = "jellyfin lan" }, "letters, digits"},
		{"a negative wait", func(h *Hook) { h.WaitSeconds = -1 }, "outside"},
		{"a wait past the ceiling", func(h *Hook) { h.WaitSeconds = MaxWaitSeconds + 1 }, "outside"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := good()
			c.edit(&h)
			err := h.Validate()
			if err == nil {
				t.Fatalf("%+v was accepted", h)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("the refusal is %q, which does not say %q", err, c.want)
			}
		})
	}
}

func TestValidateAcceptsTheOrdinaryRows(t *testing.T) {
	for _, h := range []Hook{
		good(),
		{ID: "plex", URL: "https://plex.example.org/library/sections/3/refresh", Method: MethodGet},
		// No header at all is a complete row: a server on a trusted LAN that
		// asks for nothing is the common home case.
		{ID: "kodi", URL: "http://10.0.0.5:8080/jsonrpc", Method: MethodGet, WaitSeconds: 0},
	} {
		if err := h.Validate(); err != nil {
			t.Errorf("%+v was refused: %v", h, err)
		}
	}
}

func TestSanitizeDropsWhatValidateWouldHaveRefused(t *testing.T) {
	in := []Hook{
		{ID: "  Jellyfin ", URL: " http://jellyfin.lan:8096/x ", Method: "post", HeaderName: " X-Emby-Token ", WaitSeconds: 90},
		// A duplicate: the FIRST is kept, because it is the one a picker built
		// from this slice shows first.
		{ID: "JELLYFIN", URL: "http://elsewhere.lan/x", Method: MethodGet},
		{ID: "jellyfin lan", URL: "http://x.lan/y", Method: MethodGet},
		{ID: "plex", URL: "http://plex.lan/y", Method: "TRACE", HeaderName: "X-Plex-Token: leak", WaitSeconds: MaxWaitSeconds + 500},
	}
	got := Sanitize(in)
	want := []Hook{
		{ID: "jellyfin", URL: "http://jellyfin.lan:8096/x", Method: MethodPost, HeaderName: "X-Emby-Token", WaitSeconds: 90},
		// The method this build cannot send becomes a GET rather than taking the
		// whole row away, and the header name that is really a pasted line is
		// dropped without the address going with it.
		{ID: "plex", URL: "http://plex.lan/y", Method: MethodGet, WaitSeconds: MaxWaitSeconds},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Sanitize gave\n%+v\nwant\n%+v", got, want)
	}
}

func TestSanitizeStopsAtTheCeiling(t *testing.T) {
	in := make([]Hook, 0, MaxHooks+5)
	for i := range MaxHooks + 5 {
		in = append(in, Hook{ID: string(rune('a'+i%26)) + strings.Repeat("x", i), URL: "http://h.lan/", Method: MethodGet})
	}
	if got := len(Sanitize(in)); got != MaxHooks {
		t.Errorf("Sanitize kept %d rows, want the ceiling of %d", got, MaxHooks)
	}
}

// TestSanitizeDoesNotEditTheCallersSlice is the promise sanitizeCategories and
// sanitizeHostRules both make in writing: what the caller handed in is still
// holding the same backing array, and a settings document that keeps changing
// underneath whoever submitted it is a bug people find months later.
func TestSanitizeDoesNotEditTheCallersSlice(t *testing.T) {
	in := []Hook{{ID: "  Jellyfin ", URL: "http://jellyfin.lan/", Method: "post"}}
	Sanitize(in)
	if in[0].ID != "  Jellyfin " || in[0].Method != "post" {
		t.Errorf("Sanitize wrote through to the caller's row: %+v", in[0])
	}
}

func TestHostIsWhatTheCardPrints(t *testing.T) {
	for _, c := range []struct{ url, want string }{
		{"http://jellyfin.lan:8096/Library/Refresh", "jellyfin.lan:8096"},
		{"https://plex.example.org/library/sections/3/refresh", "plex.example.org"},
		{"not an address", ""},
	} {
		if got := (Hook{URL: c.url}).Host(); got != c.want {
			t.Errorf("Host(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

// TestIsPrivateTargetErrsTowardsSayingSoOutLoud pins the direction of the one
// answer this can be wrong about. False draws an extra sentence saying the call
// leaves this machine; true withholds it. Withholding it wrongly is the mistake
// nobody can see, so a host NAME - which could be anything - answers false.
func TestIsPrivateTargetErrsTowardsSayingSoOutLoud(t *testing.T) {
	for _, c := range []struct {
		url  string
		want bool
	}{
		{"http://127.0.0.1:8096/x", true},
		{"http://localhost:8096/x", true},
		{"http://192.168.1.10:8096/x", true},
		{"http://10.0.0.5/x", true},
		{"http://172.16.4.4/x", true},
		{"http://[fd12::1]:8096/x", true},
		{"https://media.example.org/x", false},
		{"http://jellyfin.lan:8096/x", false},
		{"http://8.8.8.8/x", false},
		{"nonsense", false},
	} {
		if got := (Hook{URL: c.url}).IsPrivateTarget(); got != c.want {
			t.Errorf("IsPrivateTarget(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}

// TestMethodsOffersOnlyWhatIsSent is the same promise every other menu in
// GET /api/options makes: a value this build cannot honour must never be
// selectable.
func TestMethodsOffersOnlyWhatIsSent(t *testing.T) {
	for _, m := range Methods() {
		if got := methodOf(Hook{Method: m}); got != m {
			t.Errorf("the menu offers %q but a call with it is sent as %q", m, got)
		}
	}
}

// TestHookCarriesNoValueField is the one structural promise this package makes,
// and it is asserted rather than commented because breaking it is one word.
//
// Hook is a settings field: it is serialised into settings.json, into the
// diagnostics bundle people attach to public bug reports, and reflected into the
// Advanced key table as an editable row. A value field here would put a media
// server token in all three.
func TestHookCarriesNoValueField(t *testing.T) {
	tp := reflect.TypeOf(Hook{})
	for i := range tp.NumField() {
		switch name := strings.ToLower(tp.Field(i).Name); name {
		case "value", "headervalue", "token", "apikey", "secret", "password":
			t.Errorf("Hook has a %s field; the header value belongs in the sealed store, never in a settings row", tp.Field(i).Name)
		}
	}
}

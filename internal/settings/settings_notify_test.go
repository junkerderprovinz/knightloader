package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/junkerderprovinz/parleyport/kit/notify"
)

func channelWithToken() notify.Channel {
	return notify.Channel{
		ID: "1", Kind: notify.KindNtfy, Name: "phone", Enabled: true, On: notify.OnFailure,
		Ntfy: &notify.Ntfy{URL: "https://ntfy.example/knightloader", Token: "tk_real"},
	}
}

// migratedTargets is testdata/eventtargets-2.7.json as channels.
var migratedTargets = []notify.Channel{
	{
		ID: "1", Kind: notify.KindWebhook, Name: "Phone", Enabled: true, On: notify.OnAlways,
		Events: []string{"task.done", "package.done"}, Attempts: 2, TimeoutSeconds: 20,
		Webhook: &notify.Webhook{
			URL: "https://ntfy.example/downloads?token=%%instance%%", Method: "POST",
			Headers: map[string]string{"Authorization": "Bearer tk_alpha", "Title": "%%task.name%%"},
			Body:    "%%task.name%% is done",
		},
	},
	{
		ID: "2", Kind: notify.KindDiscord, Name: "Discord", Enabled: true, On: notify.OnAlways,
		Events:  []string{"task.failed"},
		Discord: &notify.Discord{URL: "https://discord.com/api/webhooks/123/beta-token"},
	},
	{
		ID: "3", Kind: notify.KindWebhook, Name: "Matrix, being built", On: notify.OnNever,
		Webhook: &notify.Webhook{
			URL:     "https://matrix.example/_matrix/client/v3/rooms/!room:example/send/m.room.message",
			Method:  "PUT",
			Headers: map[string]string{"Authorization": "Bearer syt_gamma", "Content-Type": "application/json"},
			Body:    `{"msgtype":"m.text","body":"%%task.name%%"}`,
		},
	},
	{
		ID: "7", Kind: notify.KindWebhook, Name: "Healthcheck", Enabled: true, On: notify.OnAlways,
		Events: []string{"queue.idle"}, Attempts: 5, TimeoutSeconds: 60,
		Webhook: &notify.Webhook{URL: "https://hc.example/ping/abc", Method: "GET"},
	},
}

func oldSettings(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "eventtargets-2.7.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func sameChannels(t *testing.T, got, want []notify.Channel) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d channels, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			g, _ := json.Marshal(got[i])
			w, _ := json.Marshal(want[i])
			t.Errorf("channel %d\n got %s\nwant %s", i, g, w)
		}
	}
}

func TestOldEventTargetsBecomeChannels(t *testing.T) {
	dir := oldSettings(t)
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	sameChannels(t, st.Get().NotifyChannels, migratedTargets)

	raw, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["notifyChannels"]; !ok {
		t.Error("the channels were not written to the file")
	}
	var kept []LegacyEventTarget
	if err := json.Unmarshal(doc["eventTargets"], &kept); err != nil || len(kept) != 4 || kept[0].Headers["Authorization"] != "Bearer tk_alpha" {
		t.Errorf("the old targets did not stay in the file: %v %+v", err, kept)
	}
}

func TestOldSealedEventTargetsBecomeChannels(t *testing.T) {
	dir := oldSettings(t)
	key := appKey(t)
	if _, err := LoadWithKey(dir, key); err != nil {
		t.Fatal(err)
	}
	// The first load sealed the file, as an upgrade with an app key does.
	raw, _ := os.ReadFile(filepath.Join(dir, FileName))
	if strings.Contains(string(raw), "tk_alpha") {
		t.Fatal("the token is still in the clear")
	}
	var doc map[string]json.RawMessage
	_ = json.Unmarshal(raw, &doc)
	delete(doc, "notifyChannels")
	again, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(dir, FileName), again, 0o600); err != nil {
		t.Fatal(err)
	}

	st, err := LoadWithKey(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	sameChannels(t, st.Get().NotifyChannels, migratedTargets)
}

func TestDeletedChannelsDoNotComeBackFromTheOldTargets(t *testing.T) {
	dir := oldSettings(t)
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := st.Get()
	n.NotifyChannels = nil
	if _, err := st.Set(n); err != nil {
		t.Fatal(err)
	}
	again, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Get().NotifyChannels; len(got) != 0 {
		t.Errorf("deleted channels came back: %+v", got)
	}
	if len(again.Get().EventTargets) != 4 {
		t.Errorf("the old targets were lost: %+v", again.Get().EventTargets)
	}
}

func TestTheBrowserNeverSeesTheOldTargetsAndCannotDropThem(t *testing.T) {
	st, err := Load(oldSettings(t))
	if err != nil {
		t.Fatal(err)
	}
	shown := st.Get().Redacted()
	if shown.EventTargets != nil {
		t.Fatalf("the browser is served the old targets: %+v", shown.EventTargets)
	}
	back, err := st.Set(shown)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.EventTargets) != 4 || back.EventTargets[0].Headers["Authorization"] != "Bearer tk_alpha" {
		t.Errorf("a save from the browser changed the old targets: %+v", back.EventTargets)
	}
}

func TestAFreshInstallHasNoChannels(t *testing.T) {
	if Defaults().NotifyChannels != nil {
		t.Fatalf("a fresh install starts with %d channel(s)", len(Defaults().NotifyChannels))
	}
	dir := t.TempDir()
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Set(Defaults()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "eventTargets") {
		t.Errorf("a fresh settings.json names eventTargets:\n%s", b)
	}
}

// The browser is served Redacted(), edits an unrelated field and posts the
// whole document back, stars and all.
func TestAChannelSecretSurvivesTheRedactedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := Defaults()
	n.NotifyChannels = []notify.Channel{channelWithToken()}
	saved, err := st.Set(n)
	if err != nil {
		t.Fatal(err)
	}
	shown := saved.Redacted()
	if got := shown.NotifyChannels[0].Ntfy.Token; got != notify.RedactedValue {
		t.Fatalf("the browser is served %q", got)
	}
	if got := saved.NotifyChannels[0].Ntfy.Token; got != "tk_real" {
		t.Fatalf("Redacted() reached the stored token (%q)", got)
	}
	shown.MaxConcurrent = 7
	back, err := st.Set(shown)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.NotifyChannels[0].Ntfy.Token; got != "tk_real" {
		t.Errorf("after the round trip the token is %q", got)
	}
	reloaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Get().NotifyChannels[0].Ntfy.Token; got != "tk_real" {
		t.Errorf("after a restart the token is %q", got)
	}
}

func TestAChannelSecretDoesNotFollowAMovedAddress(t *testing.T) {
	st, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	n := Defaults()
	n.NotifyChannels = []notify.Channel{channelWithToken()}
	saved, err := st.Set(n)
	if err != nil {
		t.Fatal(err)
	}
	moved := saved.Redacted()
	moved.NotifyChannels[0].Ntfy.URL = "https://attacker.example/collect"
	back, err := st.Set(moved)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.NotifyChannels[0].Ntfy.Token; got == "tk_real" {
		t.Error("the token followed the channel to another address")
	}
}

func TestChannelSecretsAreSealedAtRest(t *testing.T) {
	dir := t.TempDir()
	st, err := LoadWithKey(dir, appKey(t))
	if err != nil {
		t.Fatal(err)
	}
	n := Defaults()
	n.NotifyChannels = []notify.Channel{
		channelWithToken(),
		{ID: "2", Kind: notify.KindSMTP, Enabled: true, SMTP: &notify.SMTP{Host: "mail.example", From: "a@example", To: "b@example", Password: "smtp-secret"}},
		{ID: "3", Kind: notify.KindMatrix, Enabled: true, Matrix: &notify.Matrix{Homeserver: "https://matrix.example", Room: "!r:example", Token: "matrix-secret"}},
		{ID: "4", Kind: notify.KindWebhook, Enabled: true, Webhook: &notify.Webhook{URL: "https://hook.example/in", Headers: map[string]string{"X-Key": "header-secret"}, Body: "body-secret"}},
	}
	if _, err := st.Set(n); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"tk_real", "smtp-secret", "matrix-secret", "header-secret", "body-secret", "hook.example/in"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("%s is in the clear in settings.json", secret)
		}
	}
}

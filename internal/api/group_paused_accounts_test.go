package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/app"
)

func TestAMemberReadsWhichAccountsArePausedButNotTheAccounts(t *testing.T) {
	t.Parallel()
	a, _, _, member, _ := groupServer(t)
	if err := a.Accounts.SetCredential("alldebrid", "", accounts.Credential{APIKey: "KEYLEAK"}); err != nil {
		t.Fatal(err)
	}

	code, body := member(http.MethodGet, "/api/accounts/paused", "")
	if code != http.StatusOK {
		t.Fatalf("GET /api/accounts/paused answered a member %d (%s)", code, body)
	}
	var paused app.PausedAccounts
	if err := json.Unmarshal(body, &paused); err != nil || paused.Benches == nil || len(paused.On) != 1 {
		t.Fatalf("the benches a member reads = %s (%v), want none and the one account in use", body, err)
	}

	for _, path := range []string{"/api/accounts", "/api/accounts/catalogue"} {
		if code, body := member(http.MethodGet, path, ""); !refusedForGroup(code, body) {
			t.Errorf("GET %s answered a member %d (%s), want it refused", path, code, body)
		}
	}
}

func TestAMemberReadsWhatAudioCanBeConvertedToButNotTheOptions(t *testing.T) {
	t.Parallel()
	_, _, _, member, _ := groupServer(t)
	code, body := member(http.MethodGet, "/api/ytdlp/conversions", "")
	if code != http.StatusOK {
		t.Fatalf("GET /api/ytdlp/conversions answered a member %d (%s)", code, body)
	}
	var got map[string][]string
	if err := json.Unmarshal(body, &got); err != nil || len(got) != 2 ||
		!slices.Contains(got["audioFormats"], "mp3") || !slices.Contains(got["audioBitrates"], "192") {
		t.Errorf("the conversions a member reads = %s (%v), want the audio formats and bitrates alone", body, err)
	}
	if code, body := member(http.MethodGet, "/api/options", ""); !refusedForGroup(code, body) {
		t.Errorf("GET /api/options answered a member %d (%s), want it refused", code, body)
	}
}

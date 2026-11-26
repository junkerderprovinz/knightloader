package api

import (
	"encoding/json"
	"net/http"
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
	var paused []app.PausedAccount
	if err := json.Unmarshal(body, &paused); err != nil || paused == nil {
		t.Fatalf("the benches a member reads do not decode as a list: %v (%s)", err, body)
	}

	for _, path := range []string{"/api/accounts", "/api/accounts/catalogue"} {
		if code, body := member(http.MethodGet, path, ""); !refusedForGroup(code, body) {
			t.Errorf("GET %s answered a member %d (%s), want it refused", path, code, body)
		}
	}
}

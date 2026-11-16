package app

import (
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/resolver/debrid"
)

func TestALinksnappySessionIsKeptSealedForEveryClientOfItsLogin(t *testing.T) {
	a := newCrawlApp(t, false)
	login := accounts.Credential{Username: "amy", Password: "hunter2"}
	first := newDebridClient("linksnappy", login).(*debrid.Linksnappy)
	first.Sessions.KeepSession("amy", "hunter2", "lslogin=s1")

	later := newDebridClient("linksnappy", login).(*debrid.Linksnappy)
	if got := later.Sessions.Session("amy", "hunter2"); got != "lslogin=s1" {
		t.Errorf("a client built later found %q, want the session the first one kept", got)
	}
	if got := later.Sessions.Session("amy", "changed"); got != "" {
		t.Errorf("a changed password found %q, want no session", got)
	}
	for _, id := range a.Accounts.AccountIDs(hosterSessionService) {
		cred, _ := a.Accounts.GetCredential(hosterSessionService, id)
		if cred.Password != "" || strings.Contains(cred.Username+id, "hunter2") {
			t.Errorf("the kept session %q carries the password", id)
		}
	}
	if len(a.Accounts.AccountIDs(hosterSessionService)) != 1 {
		t.Errorf("kept %v, want the one session in the app's sealed store", a.Accounts.AccountIDs(hosterSessionService))
	}

	first.Sessions.KeepSession("amy", "hunter2", "")
	if got := later.Sessions.Session("amy", "hunter2"); got != "" {
		t.Errorf("a forgotten session still reads %q", got)
	}
}

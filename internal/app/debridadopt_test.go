package app

import (
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/hosterauth"
)

// A MyDebrid login kept for JD before KnightLoader spoke to MyDebrid itself
// comes back as the MyDebrid account after a restart, and the ordinary hoster
// login next to it stays where it was.
func TestAMyDebridLoginKeptForJDBecomesTheOwnAccountAtBoot(t *testing.T) {
	dir := t.TempDir()
	before, err := accounts.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	logins := hosterauth.NewStore(before)
	if err := logins.Set("mydebrid.com", accounts.Credential{Username: "knight", Password: "s3cret"}); err != nil {
		t.Fatal(err)
	}
	if err := logins.Set("ddownload.com", accounts.Credential{Username: "knight", Password: "other"}); err != nil {
		t.Fatal(err)
	}

	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	got, err := a.Accounts.GetCredential("mydebrid", "")
	if err != nil || got != (accounts.Credential{Username: "knight", Password: "s3cret"}) {
		t.Fatalf("mydebrid account = %+v, %v, want the login JD had", got, err)
	}
	if !a.accountEnabled("mydebrid", "") {
		t.Error("the moved account is switched off")
	}
	hosts := hosterauth.NewStore(a.Accounts).Hosts()
	if len(hosts) != 1 || hosts[0] != "ddownload.com" {
		t.Errorf("hoster logins after the move = %v, want ddownload.com only", hosts)
	}
}

func TestASwitchedOffLoginMovesSwitchedOff(t *testing.T) {
	a := newQueueApp(t)
	if err := a.SetHosterLogin("mydebrid.com", "knight", "s3cret"); err != nil {
		t.Fatal(err)
	}
	if err := a.SetHosterLoginEnabled("mydebrid.com", false); err != nil {
		t.Fatal(err)
	}

	a.adoptJDDebridLogins()

	if a.accountEnabled("mydebrid", "") {
		t.Error("the moved account is switched on although the login was off")
	}
	if !a.accountEnabled(hosterauth.Service, "mydebrid.com") {
		t.Error("the old login's switch outlived it, so a new login for the host would start switched off")
	}
}

func TestALoginTheAccountAlreadyHasIsDropped(t *testing.T) {
	a := newQueueApp(t)
	own := accounts.Credential{Username: "Knight", Password: "newer"}
	if err := a.Accounts.SetCredential("mydebrid", "", own); err != nil {
		t.Fatal(err)
	}
	if err := a.SetHosterLogin("mydebrid.com", "knight", "older"); err != nil {
		t.Fatal(err)
	}

	a.adoptJDDebridLogins()

	if got, _ := a.Accounts.GetCredential("mydebrid", ""); got != own {
		t.Errorf("the account = %+v, want it left as it was", got)
	}
	if ids := a.Accounts.AccountIDs("mydebrid"); len(ids) != 0 {
		t.Errorf("a second account %v was made for the same username", ids)
	}
	if hosts := hosterauth.NewStore(a.Accounts).Hosts(); len(hosts) != 0 {
		t.Errorf("hoster logins = %v, want the duplicate gone", hosts)
	}
}

func TestALoginForAnotherUsernameBecomesASecondAccount(t *testing.T) {
	a := newQueueApp(t)
	if err := a.Accounts.SetCredential("mydebrid", "", accounts.Credential{Username: "first", Password: "one"}); err != nil {
		t.Fatal(err)
	}
	if err := a.SetHosterLogin("mydebrid.com", "second", "two"); err != nil {
		t.Fatal(err)
	}

	a.adoptJDDebridLogins()

	got, _ := a.Accounts.GetCredential("mydebrid", "second")
	if got != (accounts.Credential{Username: "second", Password: "two"}) {
		t.Errorf("account second = %+v, want the login JD had", got)
	}
	if hosts := hosterauth.NewStore(a.Accounts).Hosts(); len(hosts) != 0 {
		t.Errorf("hoster logins = %v, want the moved one gone", hosts)
	}
}

// Both slots taken by other logins leaves the JD login alone rather than
// overwriting an account.
func TestALoginWithNowhereToGoStays(t *testing.T) {
	a := newQueueApp(t)
	first := accounts.Credential{Username: "first", Password: "one"}
	squatter := accounts.Credential{Username: "someone-else", Password: "three"}
	if err := a.Accounts.SetCredential("mydebrid", "", first); err != nil {
		t.Fatal(err)
	}
	if err := a.Accounts.SetCredential("mydebrid", "second", squatter); err != nil {
		t.Fatal(err)
	}
	if err := a.SetHosterLogin("mydebrid.com", "second", "two"); err != nil {
		t.Fatal(err)
	}

	a.adoptJDDebridLogins()

	if got, _ := a.Accounts.GetCredential("mydebrid", "second"); got != squatter {
		t.Errorf("account second = %+v, want it left as it was", got)
	}
	if hosts := hosterauth.NewStore(a.Accounts).Hosts(); len(hosts) != 1 {
		t.Errorf("hoster logins = %v, want the login kept", hosts)
	}
}

// LeechAll has no client of KnightLoader's own, so its login stays an
// ordinary hoster login.
func TestALeechAllLoginStaysAHosterLogin(t *testing.T) {
	a := newQueueApp(t)
	if err := a.SetHosterLogin("leechall.io", "knight@example.com", "s3cret"); err != nil {
		t.Fatal(err)
	}

	a.adoptJDDebridLogins()

	if hosts := hosterauth.NewStore(a.Accounts).Hosts(); len(hosts) != 1 || hosts[0] != "leechall.io" {
		t.Errorf("hoster logins = %v, want leechall.io kept", hosts)
	}
}

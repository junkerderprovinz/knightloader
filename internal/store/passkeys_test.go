package store

import (
	"errors"
	"testing"
)

func aKey(name, rp string, credID byte) Passkey {
	return Passkey{
		Name:         name,
		RPID:         rp,
		CredentialID: []byte{credID, 0x01, 0x02},
		PublicKey:    []byte{0xa5, 0x01, 0x02},
		AAGUID:       []byte{0x00},
		Transports:   "internal,hybrid",
		BackedUp:     true,
	}
}

func TestPasskeyRoundTrip(t *testing.T) {
	s := open(t)
	saved, err := s.AddPasskey(aKey("my phone", "kl.example.com", 0x10))
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == "" {
		t.Error("AddPasskey handed back a row with no id")
	}
	if saved.CreatedAt == 0 {
		t.Error("AddPasskey handed back a row with no creation time")
	}
	all, err := s.ListPasskeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("ListPasskeys returned %d rows, want 1", len(all))
	}
	got := all[0]
	if got.Name != "my phone" || got.RPID != "kl.example.com" || !got.BackedUp {
		t.Errorf("the row came back as %+v", got)
	}
	if string(got.PublicKey) != string(saved.PublicKey) || string(got.CredentialID) != string(saved.CredentialID) {
		t.Error("the credential's bytes did not survive the round trip")
	}
	if got.Transports != "internal,hybrid" {
		t.Errorf("Transports = %q", got.Transports)
	}
}

// TestTheSameAuthenticatorCannotRegisterTwice. Two rows answering for one key
// would leave the sign-counter check comparing against whichever was found
// first, which is the check quietly stopping rather than failing.
func TestTheSameAuthenticatorCannotRegisterTwice(t *testing.T) {
	s := open(t)
	if _, err := s.AddPasskey(aKey("my phone", "kl.example.com", 0x10)); err != nil {
		t.Fatal(err)
	}
	_, err := s.AddPasskey(aKey("the same phone under another name", "kl.example.com", 0x10))
	if !errors.Is(err, ErrPasskeyExists) {
		t.Fatalf("the second registration = %v, want ErrPasskeyExists", err)
	}
	all, _ := s.ListPasskeys()
	if len(all) != 1 {
		t.Errorf("%d rows after a refused duplicate, want 1", len(all))
	}
}

func TestAPasskeyNeedsAnIDAndAPublicKey(t *testing.T) {
	s := open(t)
	p := aKey("half a key", "kl.example.com", 0x10)
	p.PublicKey = nil
	if _, err := s.AddPasskey(p); err == nil {
		t.Error("a credential with no public key was stored")
	}
	p = aKey("half a key", "kl.example.com", 0x10)
	p.CredentialID = nil
	if _, err := s.AddPasskey(p); err == nil {
		t.Error("a credential with no id was stored")
	}
}

// TestPasskeysForRPSeesOnlyItsOwnAddress. A browser will not offer a key whose
// relying-party id does not match the page, so a login ceremony that listed the
// others would raise a prompt that cannot succeed.
func TestPasskeysForRPSeesOnlyItsOwnAddress(t *testing.T) {
	s := open(t)
	if _, err := s.AddPasskey(aKey("through the proxy", "kl.example.com", 0x10)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPasskey(aKey("on the tunnel", "localhost", 0x20)); err != nil {
		t.Fatal(err)
	}
	here, err := s.PasskeysForRP("kl.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(here) != 1 || here[0].Name != "through the proxy" {
		t.Fatalf("PasskeysForRP returned %d rows: %+v", len(here), here)
	}
	// And the full list still has both, because a key registered elsewhere is
	// MARKED rather than hidden - hiding one would make a key somebody
	// deliberately created look lost.
	all, err := s.ListPasskeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("ListPasskeys returned %d rows, want both", len(all))
	}
}

func TestRenameAndDeletePasskey(t *testing.T) {
	s := open(t)
	saved, err := s.AddPasskey(aKey("my phone", "kl.example.com", 0x10))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RenamePasskey(saved.ID, "the work laptop"); err != nil {
		t.Fatal(err)
	}
	all, _ := s.ListPasskeys()
	if all[0].Name != "the work laptop" {
		t.Errorf("name is %q after a rename", all[0].Name)
	}
	if err := s.RenamePasskey("no-such-id", "nothing"); err == nil {
		t.Error("renaming a key that is not there succeeded")
	}
	if err := s.DeletePasskey(saved.ID); err != nil {
		t.Fatal(err)
	}
	all, _ = s.ListPasskeys()
	if len(all) != 0 {
		t.Errorf("%d rows after the delete", len(all))
	}
	// Deleting one that is not there is not an error: the caller wanted it gone
	// and it is gone.
	if err := s.DeletePasskey(saved.ID); err != nil {
		t.Errorf("deleting an absent key: %v", err)
	}
}

func TestTouchPasskeyRecordsTheCounter(t *testing.T) {
	s := open(t)
	saved, err := s.AddPasskey(aKey("my phone", "kl.example.com", 0x10))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TouchPasskey(saved.ID, 42, 1700000000); err != nil {
		t.Fatal(err)
	}
	all, _ := s.ListPasskeys()
	if all[0].SignCount != 42 || all[0].LastUsedAt != 1700000000 {
		t.Errorf("after a touch the row reads count=%d lastUsed=%d", all[0].SignCount, all[0].LastUsedAt)
	}
}

// TestAnAuthenticatorWithNoAAGUIDStillRegisters. Some security keys report
// none, which is a normal case rather than an error - and a nil slice reaches
// SQLite as NULL, which the column refuses.
func TestAnAuthenticatorWithNoAAGUIDStillRegisters(t *testing.T) {
	s := open(t)
	p := aKey("a plain security key", "kl.example.com", 0x30)
	p.AAGUID = nil
	if _, err := s.AddPasskey(p); err != nil {
		t.Fatalf("a key reporting no AAGUID was refused: %v", err)
	}
}

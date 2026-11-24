package api

import (
	"context"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/parleyport/kit/phrase"
	"github.com/junkerderprovinz/parleyport/kit/signin"
)

// A reset ends what somebody else may hold: the phrase opens the group's
// routes without a password, and an MCP client's token reaches the tools.
func TestAResetLeavesThePairingGroupAndEndsTheMCPClients(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	relayOff(t, a)
	old, _, err := phrase.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := enterGroup(a, old); err != nil {
		t.Fatal(err)
	}
	before, _ := a.Accounts.Get(federation.SeedAccount)
	tok, secret, err := scopedToken(a, "assistant", []apitoken.Scope{apitoken.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}

	if err := signInEnded(a)(context.Background(), signin.EndReset); err != nil {
		t.Fatal(err)
	}
	if after, _ := a.Accounts.Get(federation.SeedAccount); after == "" || after == before {
		t.Errorf("the connection secret after the reset is %q, want a new one", after)
	}
	if _, ok := a.APITokens.Check(secret); ok {
		t.Error("an MCP client's token outlived the reset")
	}
	if _, ok := a.MCPClients.ByToken(tok.ID); ok {
		t.Error("the MCP client is still set up after the reset")
	}
}

func TestAResetOutsideAGroupStartsNone(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	if err := signInEnded(a)(context.Background(), signin.EndReset); err != nil {
		t.Fatal(err)
	}
	if secret, _ := a.Accounts.Get(federation.SeedAccount); secret != "" {
		t.Error("a reset put an instance that was in no group into one")
	}
}

// A new password changes who may sign in, not who holds the phrase or the
// assistant's token.
func TestANewPasswordKeepsTheGroupAndTheMCPClients(t *testing.T) {
	t.Parallel()
	a := testApp(t)
	relayOff(t, a)
	old, _, _ := phrase.New()
	if err := enterGroup(a, old); err != nil {
		t.Fatal(err)
	}
	before, _ := a.Accounts.Get(federation.SeedAccount)
	_, secret, err := scopedToken(a, "assistant", []apitoken.Scope{apitoken.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	if err := signInEnded(a)(context.Background(), signin.EndPassword); err != nil {
		t.Fatal(err)
	}
	if after, _ := a.Accounts.Get(federation.SeedAccount); after != before {
		t.Error("a new password left the pairing group")
	}
	if _, ok := a.APITokens.Check(secret); !ok {
		t.Error("a new password ended an MCP client's token")
	}
}

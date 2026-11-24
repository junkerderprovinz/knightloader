package api

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/federation"
	"github.com/junkerderprovinz/parleyport/kit/group"
	"github.com/junkerderprovinz/parleyport/kit/member"
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

	if err := signInEnded(a)(context.Background(), "", signin.EndReset); err != nil {
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
	if err := signInEnded(a)(context.Background(), "", signin.EndReset); err != nil {
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
	if err := signInEnded(a)(context.Background(), "", signin.EndPassword); err != nil {
		t.Fatal(err)
	}
	if after, _ := a.Accounts.Get(federation.SeedAccount); after != before {
		t.Error("a new password left the pairing group")
	}
	if _, ok := a.APITokens.Check(secret); !ok {
		t.Error("a new password ended an MCP client's token")
	}
}

// The MCP clients and devices an account set up end with the account: a
// newcomer from a sign-in service who is blocked, removed or given a lower
// role keeps none of the ways in KnightLoader keeps itself.
func TestWhatAnAccountSetUpEndsWhenTheAccountDoes(t *testing.T) {
	for _, what := range []signin.Ending{signin.EndBlocked, signin.EndRemoved, signin.EndRole, signin.EndRevoked} {
		t.Run(string(what), func(t *testing.T) {
			t.Parallel()
			a, _, _, _, _ := groupServer(t)
			assistant := func(account string) string {
				tok, secret, err := a.APITokens.CreateScoped("assistant", []apitoken.Scope{apitoken.ScopeRead})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := a.MCPClients.Add("other", tok.ID, account); err != nil {
					t.Fatal(err)
				}
				return secret
			}
			gone, kept := assistant("newcomer"), assistant("owner")
			approve(t, a, "phone-of-newcomer", member.KindMobile)
			approve(t, a, "phone-of-owner", member.KindMobile)
			if err := a.Federation.NoteApprover(idAs("phone-of-newcomer", member.KindMobile), "newcomer"); err != nil {
				t.Fatal(err)
			}
			if err := a.Federation.NoteApprover(idAs("phone-of-owner", member.KindMobile), "owner"); err != nil {
				t.Fatal(err)
			}

			if err := signInEnded(a)(context.Background(), "newcomer", what); err != nil {
				t.Fatal(err)
			}
			if _, ok := mcpToken(a, gone); ok {
				t.Error("the MCP client of the account that ended still reaches the tools")
			}
			if _, ok := mcpToken(a, kept); !ok {
				t.Error("another account's MCP client ended with it")
			}
			if a.Federation.StillMember(idAs("phone-of-newcomer", member.KindMobile)) {
				t.Error("the phone the account let in is still in the group")
			}
			if !a.Federation.StillMember(idAs("phone-of-owner", member.KindMobile)) {
				t.Error("a phone another account let in left the group with it")
			}
		})
	}
}

// Setting up an MCP client and letting a device into the group note who did
// it, so the ways in can end with that account.
func TestTheAccountThatSetSomethingUpIsNoted(t *testing.T) {
	t.Parallel()
	a, _, _, _, _ := groupServer(t)
	session := setPassword(t, a, "a-good-password")
	me := kitCall(t, a, http.MethodGet, "me", session, nil)
	var who struct {
		Account string `json:"account"`
	}
	if err := json.Unmarshal(me.Body.Bytes(), &who); err != nil || who.Account == "" {
		t.Fatalf("me = %d %s", me.Code, me.Body)
	}
	srv := httptest.NewServer(Handler(a))
	t.Cleanup(srv.Close)
	if code, out := call(t, session, http.MethodPost, srv.URL+"/api/mcp/clients", map[string]any{"kind": "claude-code", "name": "assistant", "scopes": []string{"read"}}); code/100 != 2 {
		t.Fatalf("setting up an MCP client = %d %s", code, out)
	}
	if clients := a.MCPClients.List(); len(clients) != 1 || clients[0].Account != who.Account {
		t.Errorf("the client is noted as %+v, want account %q", clients, who.Account)
	}

	key := keyOf("phone-pending").Public().(ed25519.PublicKey)
	id := member.ID(member.KindMobile, key)
	if err := groupOf(t, a).Import(group.Roster{Members: []group.Member{{ID: id, Kind: member.KindMobile, Key: key, State: group.StatePending}}}); err != nil {
		t.Fatal(err)
	}
	if code, out := call(t, session, http.MethodPost, srv.URL+"/api/connect/devices/"+id+"/approve", nil); code != http.StatusNoContent {
		t.Fatalf("approving the phone = %d %s", code, out)
	}
	if got := a.Federation.ApprovedBy(who.Account); !slices.Contains(got, id) {
		t.Errorf("the account let in %v, want the phone", got)
	}
}

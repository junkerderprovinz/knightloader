package apitoken

import (
	"slices"
	"testing"
)

func TestAKitLevelGrantsWhatItsPresetDid(t *testing.T) {
	cases := []struct {
		kit  []string
		want []Scope
	}{
		{[]string{"status"}, []Scope{ScopeStatus}},
		{[]string{"read"}, []Scope{ScopeRead}},
		{[]string{"add"}, []Scope{ScopeRead, ScopeAdd}},
		{[]string{"admin"}, AllScopes()},
		{[]string{"read", "control"}, []Scope{ScopeRead, ScopeControl}},
		{[]string{"everything"}, nil},
	}
	for _, c := range cases {
		if got := Granted(c.kit); !slices.Equal(got, c.want) {
			t.Errorf("Granted(%v) = %v, want %v", c.kit, got, c.want)
		}
	}
}

func TestATokenMovesToTheKitAsItsPreset(t *testing.T) {
	cases := []struct {
		scopes []Scope
		want   []string
	}{
		{[]Scope{ScopeStatus}, []string{"status"}},
		{[]Scope{ScopeRead}, []string{"read"}},
		{[]Scope{ScopeRead, ScopeAdd}, []string{"add"}},
		{AllScopes(), []string{"admin"}},
		{[]Scope{ScopeRead, ScopeAdd, ScopeControl, ScopeAdmin}, []string{"admin"}},
		{[]Scope{ScopeRead, ScopeControl}, []string{"read", "control"}},
	}
	for _, c := range cases {
		if got := Level(c.scopes); !slices.Equal(got, c.want) {
			t.Errorf("Level(%v) = %v, want %v", c.scopes, got, c.want)
		}
		if got := Granted(Level(c.scopes)); !slices.Equal(got, c.scopes) && !(slices.Equal(c.want, []string{"admin"})) {
			t.Errorf("Granted(Level(%v)) = %v, want the same rights back", c.scopes, got)
		}
	}
}

func TestEveryTokenComesWithItsDigest(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tok, secret, err := s.CreateScoped("sonarr", []Scope{ScopeRead, ScopeAdd})
	if err != nil {
		t.Fatal(err)
	}
	all := s.WithDigests()
	if len(all) != 1 || all[0].ID != tok.ID || all[0].Digest != Digest(secret) || !slices.Equal(all[0].Scopes, tok.Scopes) {
		t.Errorf("WithDigests = %+v, want the token with Digest of its secret", all)
	}
}

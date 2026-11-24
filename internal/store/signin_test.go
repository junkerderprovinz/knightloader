package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/parleyport/kit/signin"
	"github.com/junkerderprovinz/parleyport/kit/signin/sqlstore"
)

func signInStore(t *testing.T, s *Store) *sqlstore.Store {
	t.Helper()
	docs, err := sqlstore.New(context.Background(), s.DB(), SignInTable)
	if err != nil {
		t.Fatal(err)
	}
	return docs
}

func TestARestoreCanKeepTheSignInItReplaces(t *testing.T) {
	dir := t.TempDir()
	live, err := Open(filepath.Join(dir, "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := signInStore(t, live).Apply(ctx, signin.Doc{Kind: "account", ID: "owner", Data: []byte(`{"id":"owner","password":"x"}`)}); err != nil {
		t.Fatal(err)
	}
	if ok, err := live.SignInHasPassword(); err != nil || !ok {
		t.Fatalf("SignInHasPassword = %v, %v, want a password", ok, err)
	}
	live.Close()

	restored, err := Open(filepath.Join(dir, "restored.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if ok, err := restored.SignInHasPassword(); err != nil || ok {
		t.Fatalf("a database without the sign-in has a password: %v, %v", ok, err)
	}
	if err := restored.ReplaceSignInFrom(filepath.Join(dir, "live.db")); err != nil {
		t.Fatal(err)
	}
	if ok, err := restored.SignInHasPassword(); err != nil || !ok {
		t.Fatalf("after ReplaceSignInFrom: %v, %v, want the live password", ok, err)
	}
	// The kit writes over a document it keeps, which needs the table's key.
	if err := signInStore(t, restored).Apply(ctx, signin.Doc{Kind: "account", ID: "owner", Data: []byte(`{"id":"owner"}`)}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := restored.SignInHasPassword(); ok {
		t.Error("the document was added twice instead of replaced")
	}
	if err := restored.DropSignIn(); err != nil {
		t.Fatal(err)
	}
	if ok, err := restored.hasSignIn("main"); err != nil || ok {
		t.Errorf("after DropSignIn the table is still there: %v, %v", ok, err)
	}
}

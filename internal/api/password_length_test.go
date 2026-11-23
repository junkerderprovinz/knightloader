package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// putPassword sets the first password on srv and returns the answer.
func putPassword(t *testing.T, url, password string) (int, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"current": "", "new": password})
	req, _ := http.NewRequest(http.MethodPut, url+"/api/auth/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := readAll(resp)
	return resp.StatusCode, string(raw)
}

func TestAShortPasswordIsTaken(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	if code, raw := putPassword(t, srv.URL, "kl"); code != http.StatusOK {
		t.Fatalf("a two character password answered %d %s, want 200", code, raw)
	}
	if !a.Auth.Enabled() {
		t.Error("the short password did not lock the instance")
	}
}

// A password bcrypt cannot hash whole is refused with a code, so the
// interface can say so in the reader's language rather than show the English.
func TestAPasswordTooLongIsRefusedWithACode(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	defer srv.Close()
	password := strings.Repeat("ä", 37)
	if code, raw := putPassword(t, srv.URL, password); code != http.StatusBadRequest || readRefusal(t, raw).Code != "passwordTooLong" {
		t.Errorf("37 characters in 74 bytes answered %d %s, want 400 with the code passwordTooLong", code, raw)
	}
}

func TestAResetWithAPasswordTooLongIsRefusedWithACode(t *testing.T) {
	srv, _, out := resetServer(t)
	code := askForCode(t, srv.URL, out)
	status, raw := finishReset(t, http.DefaultClient, srv.URL, code, strings.Repeat("a", 73))
	if status != http.StatusBadRequest || readRefusal(t, string(raw)).Code != "passwordTooLong" {
		t.Errorf("a 73 byte password answered %d %s, want 400 with the code passwordTooLong", status, raw)
	}
}

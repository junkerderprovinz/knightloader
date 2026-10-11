package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// A password of the wrong length is refused with a code, so the interface
// can say so in the reader's language rather than show bcrypt's English.
func TestAPasswordOfTheWrongLengthIsRefusedWithACode(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	defer srv.Close()
	for _, c := range []struct{ password, code string }{
		{"äöüßäöü", "passwordTooShort"},
		{strings.Repeat("ä", 37), "passwordTooLong"},
	} {
		body, _ := json.Marshal(map[string]string{"current": "", "new": c.password})
		req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/auth/password", bytes.NewReader(body))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := readAll(resp)
		if resp.StatusCode != http.StatusBadRequest || readRefusal(t, string(raw)).Code != c.code {
			t.Errorf("%d characters in %d bytes answered %d %s, want 400 with the code %s",
				len([]rune(c.password)), len(c.password), resp.StatusCode, raw, c.code)
		}
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

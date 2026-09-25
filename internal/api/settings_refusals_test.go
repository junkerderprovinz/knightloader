package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/reconnect"
)

// refusal is the body of a refused settings save.
type refusal struct {
	Error string `json:"error"`
	Code  string `json:"code"`
	Field string `json:"field"`
}

func readRefusal(t *testing.T, raw string) refusal {
	t.Helper()
	var r refusal
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("the refusal is not JSON: %q", raw)
	}
	return r
}

// A refused save names the field it is about, so the page can show the reason
// there and stop sending the value until it changes. Without a field the page
// can only show the reason for the save as a whole.
func TestARefusedSaveNamesItsField(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	defer srv.Close()

	cases := []struct {
		name, patch, field, code string
	}{
		{
			name:  "a reconnect method without an IP check URL",
			patch: `{"reconnect":{"method":"command","command":"/bin/true"}}`,
			field: "reconnect.checkUrl",
			code:  "reconnect.noCheckURL",
		},
		{
			name:  "a request without an address",
			patch: `{"reconnect":{"method":"http","checkUrl":"https://check.example","requests":[{"url":"http://192.0.2.1/a"},{"url":""}]}}`,
			field: "reconnect.requests.1.url",
			code:  "reconnect.requestNoURL",
		},
		{
			name:  "a proxy without a port",
			patch: `{"connections":[{"id":"a","type":"http","host":"proxy.lan","port":8080,"enabled":true},{"id":"b","type":"http","host":"proxy.lan","enabled":true}]}`,
			field: "connections.1",
		},
		{
			name:  "a window without a weekday",
			patch: `{"schedule":[{"start":"22:00","end":"06:00","action":"pause"}]}`,
			field: "schedule.0",
		},
		{
			name:  "a feed that is not on the web",
			patch: `{"feeds":[{"url":"https://feeds.example/rss"},{"url":"ftp://feeds.example/rss"}]}`,
			field: "feeds.1",
		},
		{
			name:  "an event target without an address",
			patch: `{"eventTargets":[{"id":"1","url":""}]}`,
			field: "eventTargets.0",
			code:  "eventTargets.problem.noUrl",
		},
		{
			name:  "a category calling an address that is not stored",
			patch: `{"categories":[{"id":"filme","name":"Filme"},{"id":"serien","name":"Serien","notify":"nas"}]}`,
			field: "categories.1",
		},
		{
			name:  "two categories with one id",
			patch: `{"categories":[{"id":"filme","name":"Filme"},{"id":"filme","name":"Filme"}]}`,
			field: "categories.1",
		},
		{
			name:  "a packagizer rule filing into a category that does not exist",
			patch: `{"packagizer":{"rules":[{"name":"Serien","action":{"category":"serien"}}]}}`,
			field: "packagizer.rules.0",
		},
		{
			name:  "a value of the wrong type",
			patch: `{"reconnect":[]}`,
			field: "reconnect",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _, raw := patchSettings(t, srv.URL, tc.patch)
			if code != http.StatusBadRequest {
				t.Fatalf("answered %d, want the save refused", code)
			}
			got := readRefusal(t, raw)
			if got.Field != tc.field {
				t.Errorf("field = %q, want %q (%s)", got.Field, tc.field, got.Error)
			}
			if tc.code != "" && got.Code != tc.code {
				t.Errorf("code = %q, want %q", got.Code, tc.code)
			}
			if got.Error == "" {
				t.Error("refused without a reason")
			}
		})
	}
}

// A stored row that went bad refuses only a save that sends it. Switching
// reconnect back on after its check URL was cleared stores such a row, and
// every save on every page would otherwise be refused over it.
func TestAStoredRowThatWentBadRefusesOnlyItsOwnSave(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	s := a.Settings.Get()
	s.Reconnect = reconnect.Config{Method: reconnect.MethodCommand, Command: "/bin/true"}
	if _, err := a.ApplySettings(s); err != nil {
		t.Fatal(err)
	}

	if code, _, raw := patchSettings(t, srv.URL, `{"maxConcurrent":2}`); code != http.StatusOK {
		t.Errorf("a patch of maxConcurrent answered %d: %s", code, raw)
	}
	stored, err := json.Marshal(a.Settings.Get().Reconnect)
	if err != nil {
		t.Fatal(err)
	}
	code, _, raw := patchSettings(t, srv.URL, `{"reconnect":`+string(stored)+`}`)
	if code != http.StatusBadRequest {
		t.Fatalf("sending the half-filled reconnect answered %d, want it refused", code)
	}
	if got := readRefusal(t, raw).Field; got != "reconnect.checkUrl" {
		t.Errorf("field = %q, want reconnect.checkUrl", got)
	}
}

// A check across two lists runs when either one is sent: deleting the category
// a rule files into is refused as much as a rule naming a missing one.
func TestDeletingACategoryARuleUsesIsRefused(t *testing.T) {
	t.Parallel()
	srv, a := testServer(t)
	defer srv.Close()
	if code, _, raw := patchSettings(t, srv.URL,
		`{"categories":[{"id":"serien","name":"Serien"}],"packagizer":{"rules":[{"name":"Serien","action":{"category":"serien"}}]}}`); code != http.StatusOK {
		t.Fatalf("storing a category and a rule filing into it answered %d: %s", code, raw)
	}

	code, _, raw := patchSettings(t, srv.URL, `{"categories":[]}`)
	if code != http.StatusBadRequest {
		t.Fatalf("deleting the category answered %d, want it refused", code)
	}
	if got := readRefusal(t, raw).Field; got != "packagizer.rules.0" {
		t.Errorf("field = %q, want the rule that still files into it", got)
	}
	if got := a.Settings.Get().Categories; len(got) != 1 || got[0].ID != "serien" {
		t.Errorf("stored categories = %+v, want the category kept", got)
	}
}

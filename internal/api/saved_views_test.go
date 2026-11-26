package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// browserDoc is what the web keeps in the shared interface bucket, read the
// way the web reads it.
func browserDoc(t *testing.T, owner func(method, path, body string) (int, []byte)) map[string]json.RawMessage {
	t.Helper()
	status, body := owner(http.MethodGet, "/api/uistate", "")
	if status != http.StatusOK {
		t.Fatalf("the owner reading the interface state = %d (%s)", status, body)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("the interface state %s is no object: %v", body, err)
	}
	return doc
}

func viewNames(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var views []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &views); err != nil {
		t.Fatalf("the views %s are no list: %v", raw, err)
	}
	names := []string{}
	for _, v := range views {
		names = append(names, v.Name)
	}
	return names
}

const bigFilms = `{"id":"v1","name":"Big films","state":{"search":{"text":">1gb","category":"any"},"filters":["running"],"facets":{"host":[],"fileType":[],"package":[]}}}`

func TestAMemberKeepsTheSavedViewsTheWebShows(t *testing.T) {
	t.Parallel()
	_, _, _, member, owner := groupServer(t)
	// A browser's layout already in the bucket, which the views route leaves be.
	if status, body := owner(http.MethodPut, "/api/uistate", `{"columns.downloads":{"name":240},"list.views.collector":[]}`); status != http.StatusNoContent {
		t.Fatalf("the owner storing a layout = %d (%s)", status, body)
	}

	status, body := member(http.MethodGet, "/api/uistate/views", "")
	if status != http.StatusOK {
		t.Fatalf("a member reading the saved views = %d (%s), want 200", status, body)
	}
	if string(body) != "{\"downloads\":[],\"collector\":[]}\n" {
		t.Errorf("an instance without views answered %s", body)
	}

	if status, body := member(http.MethodPut, "/api/uistate/views", `{"downloads":[`+bigFilms+`]}`); status != http.StatusOK {
		t.Fatalf("a member adding a view = %d (%s), want 200", status, body)
	}
	doc := browserDoc(t, owner)
	if got := viewNames(t, doc["list.views.downloads"]); len(got) != 1 || got[0] != "Big films" {
		t.Errorf("the web reads the download views as %v, want the one the member added", got)
	}
	if string(doc["columns.downloads"]) != `{"name":240}` {
		t.Errorf("the layout beside the views became %s", doc["columns.downloads"])
	}

	renamed := strings.Replace(bigFilms, "Big films", "  Large films  ", 1)
	status, body = member(http.MethodPut, "/api/uistate/views", `{"downloads":[`+renamed+`]}`)
	if status != http.StatusOK {
		t.Fatalf("a member renaming a view = %d (%s), want 200", status, body)
	}
	var answer struct {
		Downloads json.RawMessage `json:"downloads"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatal(err)
	}
	if got := viewNames(t, answer.Downloads); len(got) != 1 || got[0] != "Large films" {
		t.Errorf("the rename answered %v, want the trimmed new name", got)
	}

	if status, body := member(http.MethodPut, "/api/uistate/views", `{"downloads":[]}`); status != http.StatusOK {
		t.Fatalf("a member deleting the view = %d (%s), want 200", status, body)
	}
	doc = browserDoc(t, owner)
	if got := viewNames(t, doc["list.views.downloads"]); len(got) != 0 {
		t.Errorf("the web still reads %v after the member deleted the view", got)
	}
	if _, ok := doc["columns.downloads"]; !ok {
		t.Error("deleting a view took the layout with it")
	}
}

func TestTheViewsRouteTakesNothingButSavedViews(t *testing.T) {
	t.Parallel()
	_, _, _, member, owner := groupServer(t)
	if status, body := owner(http.MethodPut, "/api/uistate", `{"columns.downloads":{"name":240}}`); status != http.StatusNoContent {
		t.Fatalf("the owner storing a layout = %d (%s)", status, body)
	}
	view := func(id, name, state string) string {
		return fmt.Sprintf(`{"id":%q,"name":%q,"state":%s}`, id, name, state)
	}
	plain := `{"search":{"text":"","category":"any"},"filters":[],"facets":{"host":[],"fileType":[],"package":[]}}`
	thirteen := make([]string, 13)
	for i := range thirteen {
		thirteen[i] = view(fmt.Sprintf("v%d", i), fmt.Sprintf("View %d", i), plain)
	}
	huge := `{"search":{"text":"` + strings.Repeat("x", 5000) + `","category":"any"}}`
	for _, body := range []string{
		`{"downloads":[],"columns.downloads":{}}`,
		`{"downloads":[],"settings":{"speedLimit":1}}`,
		`{"torrents":[]}`,
		`{"downloads":[` + view("v1", "A", plain)[:len(view("v1", "A", plain))-1] + `,"extra":1}]}`,
		`{"downloads":[` + view("", "No id", plain) + `]}`,
		`{"downloads":[` + view("v1", "   ", plain) + `]}`,
		`{"downloads":[` + view("v1", strings.Repeat("n", 61), plain) + `]}`,
		`{"downloads":[` + view("v1", "A", plain) + `,` + view("v1", "B", plain) + `]}`,
		`{"downloads":[` + view("v1", "A", `"just text"`) + `]}`,
		`{"downloads":[` + view("v1", "A", huge) + `]}`,
		`{"downloads":[` + strings.Join(thirteen, ",") + `]}`,
		`{"downloads":{}}`,
		`{}`,
		`[]`,
		`not json`,
	} {
		if status, answer := member(http.MethodPut, "/api/uistate/views", body); status != http.StatusBadRequest {
			t.Errorf("a member putting %.80s = %d (%s), want 400", body, status, answer)
		}
	}
	doc := browserDoc(t, owner)
	if len(doc) != 1 || string(doc["columns.downloads"]) != `{"name":240}` {
		t.Errorf("a refused body changed the interface state to %v", doc)
	}
}

func TestAMemberStillDoesNotReachTheWholeInterfaceState(t *testing.T) {
	t.Parallel()
	_, _, _, member, _ := groupServer(t)
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/uistate", ""},
		{http.MethodPut, "/api/uistate", `{"list.views.downloads":[]}`},
		{http.MethodGet, "/api/uistate?key=feeds", ""},
		{http.MethodGet, "/api/settings", ""},
	} {
		if status, body := member(c.method, c.path, c.body); !refusedForGroup(status, body) {
			t.Errorf("a member calling %s %s = %d (%s), want ownLoginNeeded", c.method, c.path, status, body)
		}
	}
}

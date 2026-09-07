package api

// Taking a removal back. See app.RemoveTasksUndoable for why erasing the files
// earns no token, and why the bin is process-local.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
)

// deleteSelection removes rows through the route and hands back the answer.
func deleteSelection(t *testing.T, base string, sel []string, files bool) bulkResult {
	t.Helper()
	code, body := postJSON(t, http.MethodPost, base+"/api/tasks/delete",
		map[string]any{"ids": sel, "files": files})
	if code != http.StatusOK {
		t.Fatalf("delete = %d: %s", code, body)
	}
	var out bulkResult
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("delete answered unparseable JSON: %v (%s)", err, body)
	}
	return out
}

// undo presses the button and hands back what came home.
func undo(t *testing.T, base, token string) bulkResult {
	t.Helper()
	code, body := postJSON(t, http.MethodPost, base+"/api/tasks/undo-delete",
		map[string]any{"token": token})
	if code != http.StatusOK {
		t.Fatalf("undo = %d: %s", code, body)
	}
	var out bulkResult
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("undo answered unparseable JSON: %v (%s)", err, body)
	}
	return out
}

// TestRemovedRowsComeBack is the feature in one pass: the rows really leave, and
// the token really brings them home.
//
// It checks the list rather than only the answer, because a route that reports
// two ids and restores nothing is exactly the failure an answer-only test would
// call a pass.
func TestRemovedRowsComeBack(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	created := stage(t, a, "https://host.example/one.bin", "https://host.example/two.bin")
	sel := ids(created)

	gone := deleteSelection(t, srv.URL, sel, false)
	if gone.Count != 2 {
		t.Fatalf("removed %d of 2 rows", gone.Count)
	}
	if gone.Undo == "" {
		t.Fatal("a removal that left the files alone came back with no undo token")
	}
	if gone.UndoMs != app.UndoWindow.Milliseconds() {
		t.Errorf("the answer says the token is good for %d ms, the bin holds it for %d",
			gone.UndoMs, app.UndoWindow.Milliseconds())
	}
	if n := len(a.Tasks()); n != 0 {
		t.Fatalf("%d rows are still in the list after removing both", n)
	}

	back := undo(t, srv.URL, gone.Undo)
	if back.Count != 2 {
		t.Fatalf("the undo reports %d rows back, want 2", back.Count)
	}
	live := map[string]bool{}
	for _, task := range a.Tasks() {
		live[task.ID] = true
	}
	for _, id := range sel {
		if !live[id] {
			t.Errorf("task %s is not in the list after the undo said it restored it", id)
		}
	}
}

// TestErasingTheFilesOffersNoUndo is the one case the bin must refuse.
//
// The bytes are gone, so a token here would be a button promising a restore it
// cannot make: the row would come back pointing at a file that no longer exists,
// and the person who pressed it would find out at the next transfer.
func TestErasingTheFilesOffersNoUndo(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	created := stage(t, a, "https://host.example/one.bin")
	gone := deleteSelection(t, srv.URL, ids(created), true)
	if gone.Count != 1 {
		t.Fatalf("removed %d of 1 row", gone.Count)
	}
	if gone.Undo != "" || gone.UndoMs != 0 {
		t.Errorf("a removal that erased the files offered an undo (%q, %d ms)", gone.Undo, gone.UndoMs)
	}
}

// TestAnUndoTokenIsGoodOnce keeps a second press from inventing rows.
//
// Two browsers watch one instance and both show the message, so the same token
// genuinely does get pressed twice. The second press has to be a quiet "nothing
// came back", never a duplicate of a download that is already in the list.
func TestAnUndoTokenIsGoodOnce(t *testing.T) {
	srv, a := testServer(t)
	defer srv.Close()

	created := stage(t, a, "https://host.example/one.bin")
	gone := deleteSelection(t, srv.URL, ids(created), false)

	if first := undo(t, srv.URL, gone.Undo); first.Count != 1 {
		t.Fatalf("the first undo restored %d rows, want 1", first.Count)
	}
	if second := undo(t, srv.URL, gone.Undo); second.Count != 0 {
		t.Errorf("the same token restored %d more rows on a second press", second.Count)
	}
	if n := len(a.Tasks()); n != 1 {
		t.Errorf("the list holds %d rows after one removal and two undos, want 1", n)
	}
}

// TestAnUnknownUndoTokenIsNotAnError pins the shape of "too late".
//
// The window closing is the ordinary end of a token's life, not a fault, and a
// 404 here would be reported as a broken button by everybody who pressed one
// second after the bin emptied.
func TestAnUnknownUndoTokenIsNotAnError(t *testing.T) {
	srv, _ := testServer(t)
	defer srv.Close()

	if out := undo(t, srv.URL, "no-such-token"); out.Count != 0 {
		t.Errorf("an unknown token restored %d rows", out.Count)
	}
}

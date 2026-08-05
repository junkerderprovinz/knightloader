package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

// TestFederationProxy runs two real instances and drives instance B entirely
// through instance A's proxy routes: register, list tasks, add a link, remove.
func TestFederationProxy(t *testing.T) {
	aApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer aApp.Close()
	bApp, err := app.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bApp.Close()

	aSrv := httptest.NewServer(Handler(aApp))
	defer aSrv.Close()
	bSrv := httptest.NewServer(Handler(bApp))
	defer bSrv.Close()

	// Register B as a peer of A; the add response reports it online.
	body, _ := json.Marshal(map[string]string{"name": "cellar", "url": bSrv.URL})
	resp, err := http.Post(aSrv.URL+"/api/instances", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var added struct {
		Online bool `json:"online"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&added)
	resp.Body.Close()
	if !added.Online {
		t.Fatal("peer not reported online")
	}

	// A task added directly on B must be visible through A's proxy.
	seed := bApp.AddLinks([]string{"https://example.com/direct-file.zip"}, "fed")
	if len(seed) != 1 {
		t.Fatalf("seed task not created on B")
	}
	resp, err = http.Get(aSrv.URL + "/api/instances/cellar/tasks")
	if err != nil {
		t.Fatal(err)
	}
	var tasks []core.Task
	_ = json.NewDecoder(resp.Body).Decode(&tasks)
	resp.Body.Close()
	if len(tasks) != 1 || tasks[0].Name != "direct-file.zip" {
		t.Fatalf("proxied tasks = %+v, want the seeded B task", tasks)
	}

	// Adding links through the proxy lands on B, not on A.
	body, _ = json.Marshal(map[string]string{"links": "https://example.com/second.zip", "package": "fed"})
	resp, err = http.Post(aSrv.URL+"/api/instances/cellar/links", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := len(bApp.Tasks()); got != 2 {
		t.Fatalf("B has %d tasks after proxied add, want 2", got)
	}
	if got := len(aApp.Tasks()); got != 0 {
		t.Fatalf("A has %d tasks, proxied add must not create local tasks", got)
	}

	// Deleting through the proxy removes on B.
	req, _ := http.NewRequest(http.MethodDelete, aSrv.URL+"/api/instances/cellar/tasks/"+seed[0].ID, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := len(bApp.Tasks()); got != 1 {
		t.Fatalf("B has %d tasks after proxied delete, want 1", got)
	}

	// Non-task routes must not be proxied.
	resp, err = http.Get(aSrv.URL + "/api/instances/cellar/settings")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("settings proxy = HTTP %d, want 403", resp.StatusCode)
	}

	// Peer list is persisted + removable.
	resp, _ = http.Get(aSrv.URL + "/api/instances")
	b, _ := readAll(resp)
	if !strings.Contains(string(b), "cellar") {
		t.Fatalf("instances list %s missing peer", b)
	}
	req, _ = http.NewRequest(http.MethodDelete, aSrv.URL+"/api/instances/cellar", nil)
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	resp, _ = http.Get(aSrv.URL + "/api/instances")
	b, _ = readAll(resp)
	if strings.Contains(string(b), "cellar") {
		t.Fatalf("peer still listed after delete: %s", b)
	}
}

func readAll(r *http.Response) ([]byte, error) {
	defer r.Body.Close()
	var buf bytes.Buffer
	_, err := buf.ReadFrom(r.Body)
	return buf.Bytes(), err
}

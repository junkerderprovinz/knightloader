package api

// The disk readout as it reaches a browser, and the one thing about it that is
// easier to get wrong later than now: it describes THIS machine, so it must not
// travel to a peer.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// diskSpaceServer is the readout's route on a throwaway app.
func diskSpaceServer(t *testing.T) *httptest.Server {
	t.Helper()
	reg := newRegistry()
	registerDiskSpace(reg, testApp(t))
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestTheDiskReportAlwaysCarriesAListOfVolumes is the shape assertion, and the
// list is what it is about: a nil slice encodes as JSON null, and a page that
// walks over the answer throws on it rather than drawing nothing. The download
// folder is always one of the rows, because there is always one - configured or
// the built-in default.
func TestTheDiskReportAlwaysCarriesAListOfVolumes(t *testing.T) {
	srv := diskSpaceServer(t)
	code, raw := getRaw(t, srv.URL+"/api/diskspace")
	if code != http.StatusOK {
		t.Fatalf("GET /api/diskspace answered %d: %s", code, raw)
	}
	var got struct {
		Volumes []struct {
			Dir      string `json:"dir"`
			Measured string `json:"measured"`
			Role     string `json:"role"`
		} `json:"volumes"`
		SampledAt string `json:"sampledAt"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Volumes == nil {
		t.Fatalf("volumes came back as null rather than as a list: %s", raw)
	}
	if len(got.Volumes) == 0 {
		t.Fatal("no folder at all was reported, though every instance has a download folder")
	}
	for _, v := range got.Volumes {
		if v.Dir == "" || v.Measured == "" || v.Role == "" {
			t.Errorf("a row arrived without one of the three fields that say what it describes: %+v", v)
		}
	}
	if got.SampledAt == "" {
		t.Error("the reading carries no timestamp; it is shared for a few seconds and the interface has to be able to say so")
	}
}

// TestTheDiskReportIsNotForwardedToAPeer is the trap this route sets for
// whoever widens the relay allowlist next. The list is what a sibling holding
// the group phrase may reach, and it is deliberately narrow: tasks, links, the
// queue. A disk row is not a task - it describes the volumes of the machine
// that answers - so a peer's reply drawn under that peer's name would be this
// box's disks, or the other way round, with nothing on screen to say which.
func TestTheDiskReportIsNotForwardedToAPeer(t *testing.T) {
	if relayForwardable(http.MethodGet, "/api/diskspace") {
		t.Error("GET /api/diskspace is forwardable to a peer; a reading of one machine's disks answered under another machine's name is a wrong number nobody can spot")
	}
}

// TestTheDiskReportNeedsASession keeps the route behind the guard everything
// else under /api/ is behind. It has no credential of its own in the request,
// and what it answers with is folder paths off this host's filesystem.
func TestTheDiskReportNeedsASession(t *testing.T) {
	reg := newRegistry()
	registerDiskSpace(reg, testApp(t))
	if reg.open("/api/diskspace") {
		t.Error("/api/diskspace answers without a session; only the routes the login flow itself depends on may")
	}
}

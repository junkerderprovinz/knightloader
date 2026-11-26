package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/buildinfo"
	"github.com/junkerderprovinz/knightloader/internal/container"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver/torrent"
	"github.com/junkerderprovinz/knightloader/internal/testenv"
)

// pickedFile is the body the phone app sends a picked file in.
func pickedFile(t *testing.T, name string, data []byte, pkg string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"name": name, "data": data, "package": pkg})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAMemberStagesAContainerFileFromThePhone(t *testing.T) {
	t.Parallel()
	a, _, _, member, _ := groupServer(t)

	status, body := member(http.MethodPost, "/api/containers/phone", pickedFile(t, "release.rsdf", []byte(sampleRSDF), "Release"))
	if status != http.StatusOK {
		t.Fatalf("a member sending an RSDF = %d (%s), want 200", status, body)
	}
	var got struct {
		Kind    string
		Links   int
		Created []core.Task
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Kind != "rsdf" || got.Links != 2 || len(got.Created) != 2 || len(a.Tasks()) != 2 {
		t.Fatalf("answered %+v with %d tasks, want both links of the rsdf staged", got, len(a.Tasks()))
	}
	if got.Created[0].Package != "Release" {
		t.Errorf("the links landed in %q, want the package the phone named", got.Created[0].Package)
	}
}

func TestAMemberReviewsAndStagesATorrentFromThePhone(t *testing.T) {
	t.Parallel()
	a, _, _, member, _ := groupServer(t)
	data := testMultiFileTorrent(t, "Pack", []testenv.TorrentFile{
		{Length: 900, Path: []string{"one.mkv"}},
		{Length: 12, Path: []string{"two.srt"}},
	})

	status, body := member(http.MethodPost, "/api/torrents/parse/phone", pickedFile(t, "pack.torrent", data, ""))
	if status != http.StatusOK {
		t.Fatalf("a member parsing a .torrent = %d (%s), want 200", status, body)
	}
	var tree torrentTree
	if err := json.Unmarshal(body, &tree); err != nil {
		t.Fatal(err)
	}
	if tree.Name != "Pack" || len(tree.Files) != 2 || tree.URI == "" {
		t.Fatalf("the tree is %+v, want Pack with two files and a uri", tree)
	}
	if len(a.Tasks()) != 0 {
		t.Fatalf("parsing staged %d tasks, want none", len(a.Tasks()))
	}

	stageBody, _ := json.Marshal(map[string]any{"uri": tree.URI, "package": "", "selectedPaths": []string{"one.mkv"}})
	status, body = member(http.MethodPost, "/api/torrents", string(stageBody))
	if status != http.StatusOK {
		t.Fatalf("a member staging the torrent = %d (%s), want 200", status, body)
	}
	var task core.Task
	if err := json.Unmarshal(body, &task); err != nil {
		t.Fatal(err)
	}
	staged := stagedTask(t, a, task.ID)
	for _, f := range staged.TorrentFiles {
		if f.Selected != (f.Path == "one.mkv") {
			t.Errorf("file %q selected=%v, want only one.mkv selected", f.Path, f.Selected)
		}
	}
}

// The multipart uploads stay the web interface's: a member reaches the same
// work only through the routes that take the file as JSON.
func TestAMemberIsStillRefusedTheWebUploadsAndTheHandoverAddress(t *testing.T) {
	t.Parallel()
	_, _, _, member, _ := groupServer(t)
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/containers"},
		{http.MethodPost, "/api/torrents/parse"},
		{http.MethodGet, "/api/containers/relay/abc.dlc"},
		{http.MethodGet, "/api/torrents/trackers"},
	} {
		if status, body := member(c.method, c.path, ""); !refusedForGroup(status, body) {
			t.Errorf("%s %s answered a member %d (%s), want ownLoginNeeded", c.method, c.path, status, body)
		}
	}
}

func TestAPhoneFileThatIsNotBase64IsRefused(t *testing.T) {
	t.Parallel()
	a, _, _, member, _ := groupServer(t)
	for _, path := range []string{"/api/containers/phone", "/api/torrents/parse/phone"} {
		status, body := member(http.MethodPost, path, `{"name":"x.dlc","data":"not base64 at all!"}`)
		if status != http.StatusBadRequest {
			t.Errorf("%s with bytes that are no base64 = %d (%s), want 400", path, status, body)
		}
	}
	if len(a.Tasks()) != 0 {
		t.Errorf("%d tasks were staged from bad bodies", len(a.Tasks()))
	}
}

func TestAPhoneFileWithoutBytesIsRefused(t *testing.T) {
	t.Parallel()
	_, _, _, member, _ := groupServer(t)
	for _, path := range []string{"/api/containers/phone", "/api/torrents/parse/phone"} {
		if status, body := member(http.MethodPost, path, `{"name":"x.dlc"}`); status != http.StatusBadRequest {
			t.Errorf("%s with no file = %d (%s), want 400", path, status, body)
		}
	}
}

// The caps the web upload applies hold for a file sent as JSON too. The owner
// sends these, since a member's direct call is capped below them on arrival.
func TestAPhoneFileOverTheUploadCapIsRefused(t *testing.T) {
	t.Parallel()
	_, _, _, _, owner := groupServer(t)
	cases := []struct {
		path, name string
		size       int
	}{
		{"/api/containers/phone", "big.dlc", container.MaxBytes + 1},
		{"/api/torrents/parse/phone", "big.torrent", torrent.MaxTorrentBytes + 1},
	}
	for _, c := range cases {
		status, body := owner(http.MethodPost, c.path, pickedFile(t, c.name, make([]byte, c.size), ""))
		if status != http.StatusRequestEntityTooLarge {
			t.Errorf("%s with %d bytes = %d (%s), want 413", c.path, c.size, status, body)
		}
	}
}

// A call from the group carries no Host, so the JD backend is pointed at this
// machine's own listener rather than at an address with no host in it.
func TestAHandoverFromTheGroupNamesThisMachinesListener(t *testing.T) {
	prevPort, prevBase := buildinfo.ListenPort, buildinfo.BasePath
	t.Cleanup(func() { buildinfo.ListenPort, buildinfo.BasePath = prevPort, prevBase })
	buildinfo.ListenPort, buildinfo.BasePath = 8484, "/kl"

	browser := httptest.NewRequest(http.MethodPost, "/kl/api/containers", nil)
	browser.Host = "nas.local:9000"
	browser = browser.WithContext(context.WithValue(browser.Context(), basePathKey, "/kl"))
	if got, ok := handoverOrigin(browser); !ok || got != "http://nas.local:9000/kl" {
		t.Errorf("a browser's upload hands over at %q (%v), want the address it reached", got, ok)
	}

	call := httptest.NewRequest(http.MethodPost, "/api/containers/phone", nil)
	call.Host = ""
	got, ok := handoverOrigin(call)
	ips := localIPv4s()
	if len(ips) == 0 {
		if ok {
			t.Errorf("with no network address the handover named %q", got)
		}
		return
	}
	if want := "http://" + ips[0] + ":" + strconv.Itoa(8484) + "/kl"; !ok || got != want {
		t.Errorf("a call from the group hands over at %q (%v), want %q", got, ok, want)
	}

	buildinfo.ListenPort = 0
	if got, ok := handoverOrigin(call); ok || strings.Contains(got, "://") {
		t.Errorf("without a listener the handover named %q", got)
	}
}

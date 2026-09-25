package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/resolver/ytdlp"
)

// getHostFormats asks the preset menus route about host and decodes the
// answer under the field names the web client reads.
func getHostFormats(t *testing.T, srv *httptest.Server, host string) (int, ytdlp.HostMenus) {
	t.Helper()
	resp, err := http.Get(srv.URL + "/api/ytdlp/formats?host=" + host)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		VideoFormats []string `json:"videoFormats"`
		AudioFormats []string `json:"audioFormats"`
		Known        bool     `json:"known"`
	}
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, ytdlp.HostMenus(got)
}

func TestThePresetMenusRouteAnswersPerHost(t *testing.T) {
	reg := newRegistry()
	registerResolvers(reg, testApp(t))
	mux := http.NewServeMux()
	reg.attach(mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	status, yt := getHostFormats(t, srv, "youtube.com")
	if status != http.StatusOK || !yt.Known || !slices.Equal(yt.AudioFormats, []string{"best", "aac", "opus"}) {
		t.Errorf("youtube.com: %d %+v, want YouTube's own formats", status, yt)
	}
	status, unknown := getHostFormats(t, srv, "example.org")
	if status != http.StatusOK || unknown.Known || !slices.Equal(unknown.AudioFormats, ytdlp.AudioFormats()) {
		t.Errorf("example.org: %d %+v, want every format and nothing known", status, unknown)
	}
	if status, _ := getHostFormats(t, srv, ""); status != http.StatusBadRequest {
		t.Errorf("no host: %d, want %d", status, http.StatusBadRequest)
	}
}

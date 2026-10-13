package debrid

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

func TestRealDebridListsItsNewestTorrents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/torrents" || r.URL.Query().Get("limit") != "100" {
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Error("the list went out without the token")
		}
		fmt.Fprint(w, `[{"id":"RD2","filename":"Show","hash":"ABCDEF","bytes":730,"added":"2026-09-25T10:00:00.000Z","status":"downloaded"}]`)
	}))
	defer srv.Close()
	rd := NewRealDebrid("tok")
	rd.base = srv.URL

	got, complete, err := rd.List(context.Background())
	want := []Listed{{ID: "RD2", Name: "Show", Size: 730, Hash: "abcdef", Added: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)}}
	if err != nil || !complete || !listedEqual(got, want) {
		t.Errorf("List = %+v, %v, %v; want %+v, complete", got, complete, err, want)
	}
}

func TestAllDebridListsEveryMagnet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v4.1/magnet/status" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.FormValue("id") != "" {
			t.Error("the list asked for one magnet")
		}
		fmt.Fprint(w, `{"status":"success","data":{"magnets":[{"id":7,"filename":"Show","size":730,"hash":"ABCDEF","uploadDate":1790000000,"statusCode":1}]}}`)
	}))
	defer srv.Close()
	ad := NewAllDebrid("key")
	ad.base = srv.URL + "/v4"

	got, complete, err := ad.List(context.Background())
	want := []Listed{{ID: "7", Name: "Show", Size: 730, Hash: "abcdef", Added: time.Unix(1790000000, 0)}}
	if err != nil || !complete || !listedEqual(got, want) {
		t.Errorf("List = %+v, %v, %v; want %+v, complete", got, complete, err, want)
	}
}

// Premiumize names a transfer's source only through a proxy link on its own
// API, whatever was added.
func TestPremiumizeListsEveryTransfer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/transfer/list" {
			t.Errorf("unexpected %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"status":"success","transfers":[
			{"id":"PM1","name":"Show","status":"running","src":"https://www.premiumize.me/api/job/src?id=PM1"},
			{"id":"PM2","name":"file.zip","status":"finished","src":"https://www.premiumize.me/api/job/src?id=PM2"}]}`)
	}))
	defer srv.Close()
	pm := NewPremiumize("key")
	pm.base = srv.URL

	got, complete, err := pm.List(context.Background())
	want := []Listed{{ID: "PM1", Name: "Show"}, {ID: "PM2", Name: "file.zip"}}
	if err != nil || !complete || !listedEqual(got, want) {
		t.Errorf("List = %+v, %v, %v; want %+v, complete", got, complete, err, want)
	}
}

func TestDebridLinkListsItsNewestTorrents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/seedbox/list" || r.URL.Query().Get("perPage") != "50" {
			t.Errorf("unexpected %s", r.URL)
		}
		fmt.Fprint(w, `{"success":true,"value":[{"id":"DL1","name":"Show","hashString":"ABCDEF","totalSize":730,"created":1790000000}],
			"pagination":{"page":0,"pages":1,"next":-1,"previous":-1}}`)
	}))
	defer srv.Close()
	dl := NewDebridLink("key")
	dl.base = srv.URL

	got, complete, err := dl.List(context.Background())
	want := []Listed{{ID: "DL1", Name: "Show", Size: 730, Hash: "abcdef", Added: time.Unix(1790000000, 0)}}
	if err != nil || !complete || !listedEqual(got, want) {
		t.Errorf("List = %+v, %v, %v; want %+v, complete", got, complete, err, want)
	}
}

func listedEqual(a, b []Listed) bool {
	return slices.EqualFunc(a, b, func(x, y Listed) bool {
		return x.ID == y.ID && x.Name == y.Name && x.Size == y.Size && x.Hash == y.Hash && x.Added.Equal(y.Added)
	})
}

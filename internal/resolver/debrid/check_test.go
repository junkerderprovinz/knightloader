package debrid

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
)

// The check must use the free /link/infos and never /link/unlock, which spends
// traffic.
func TestAllDebridCheckLinks(t *testing.T) {
	var unlocked int
	mux := http.NewServeMux()
	mux.HandleFunc("/v4/link/unlock", func(http.ResponseWriter, *http.Request) { unlocked++ })
	mux.HandleFunc("/v4/link/infos", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer AD-KEY" {
			t.Errorf("auth = %q, want the documented Bearer header", got)
		}
		_ = r.ParseForm()
		if got := r.PostForm["link[]"]; len(got) != 4 {
			t.Errorf("link[] = %v, want all four links in one call", got)
		}
		// Out of order and one link short.
		_, _ = w.Write([]byte(`{"status":"success","data":{"infos":[
			{"link":"https://h.example/pass","error":{"code":"LINK_PASS_PROTECTED","message":"Link is password protected"}},
			{"link":"https://h.example/dead","error":{"code":"LINK_DOWN","message":"This link is not available on the file hoster website"}},
			{"link":"https://h.example/live","filename":"live.mkv","size":4096}]}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ad := NewAllDebrid("AD-KEY")
	ad.base = srv.URL + "/v4"

	links := []string{
		"https://h.example/live",
		"https://h.example/dead",
		"https://h.example/pass",
		"https://h.example/silent",
	}
	got, err := ad.CheckLinks(context.Background(), links)
	if err != nil {
		t.Fatal(err)
	}
	want := []core.Availability{
		core.AvailOnline,
		core.AvailOffline,
		// Password-protected: the file exists but may not be obtainable.
		core.AvailUncheckable,
		// No entry came back for it.
		core.AvailUncheckable,
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s = %q, want %q", links[i], got[i], want[i])
		}
	}
	if unlocked != 0 {
		t.Errorf("the check called /link/unlock %d times; a check must never spend the user's traffic", unlocked)
	}
}

// Splitting a large batch must not drop the links at its tail.
func TestAllDebridCheckChunksTheBatch(t *testing.T) {
	var calls int
	seen := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = r.ParseForm()
		var entries []string
		for _, l := range r.PostForm["link[]"] {
			seen[l] = true
			entries = append(entries, fmt.Sprintf(`{"link":%q,"filename":"f.bin","size":1}`, l))
		}
		_, _ = fmt.Fprintf(w, `{"status":"success","data":{"infos":[%s]}}`, strings.Join(entries, ","))
	}))
	defer srv.Close()

	ad := NewAllDebrid("K")
	ad.base = srv.URL

	links := make([]string, adBatch+adBatch/2)
	for i := range links {
		links[i] = fmt.Sprintf("https://h.example/f%d.bin", i)
	}
	got, err := ad.CheckLinks(context.Background(), links)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("%d links went out in %d calls, want 2", len(links), calls)
	}
	if len(seen) != len(links) {
		t.Errorf("the service was asked about %d of %d links", len(seen), len(links))
	}
	for i, a := range got {
		if a != core.AvailOnline {
			t.Fatalf("link %d = %q, want online (a dropped tail reads exactly like this)", i, a)
		}
	}
}

func TestRealDebridCheckLinks(t *testing.T) {
	var unrestricted int
	mux := http.NewServeMux()
	mux.HandleFunc("/unrestrict/link", func(http.ResponseWriter, *http.Request) { unrestricted++ })
	mux.HandleFunc("/unrestrict/check", func(w http.ResponseWriter, r *http.Request) {
		// Without a token the check cannot be attributed to the account.
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("check sent %q; it is called anonymously on purpose", got)
		}
		_ = r.ParseForm()
		switch r.FormValue("link") {
		case "https://h.example/live":
			_, _ = w.Write([]byte(`{"host":"h.example","link":"https://h.example/live",
				"filename":"live.mkv","filesize":4096,"supported":1}`))
		case "https://h.example/dead":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"file_unavailable","error_code":24}`))
		case "https://h.example/maintenance":
			// Same 503 as above; only the error code differs.
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"hoster_unavailable","error_code":19}`))
		case "https://h.example/unsupported":
			_, _ = w.Write([]byte(`{"host":"h.example","supported":0}`))
		default:
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"slow_down","error_code":5}`))
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rd := NewRealDebrid("RD-TOKEN")
	rd.base = srv.URL

	links := []string{
		"https://h.example/live",
		"https://h.example/dead",
		"https://h.example/maintenance",
		"https://h.example/unsupported",
		"https://h.example/busy",
	}
	got, err := rd.CheckLinks(context.Background(), links)
	if err != nil {
		t.Fatal(err)
	}
	want := []core.Availability{
		core.AvailOnline,
		core.AvailOffline,
		core.AvailUncheckable,
		core.AvailUncheckable,
		core.AvailUncheckable,
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s = %q, want %q", links[i], got[i], want[i])
		}
	}
	if unrestricted != 0 {
		t.Errorf("the check called /unrestrict/link %d times; that is the call that spends traffic", unrestricted)
	}
}

// noCheckService can unlock but not check.
type noCheckService struct{}

func (noCheckService) ID() string    { return "nocheck" }
func (noCheckService) Label() string { return "No Check" }
func (noCheckService) Hosts(context.Context) (map[string]bool, error) {
	return map[string]bool{"h.example": true}, nil
}
func (noCheckService) Unlock(context.Context, string) (Direct, error) { return Direct{}, nil }

// Without a free check, every link is uncheckable and there is no error.
func TestResolverCheckWithoutAProvider(t *testing.T) {
	links := []string{"https://h.example/a", "https://h.example/b"}
	for _, r := range []Resolver{
		{ServiceID: "nocheck", Svc: noCheckService{}},
		{ServiceID: "unwired"}, // Svc never set, as in every routing-only test
	} {
		got, err := r.Check(context.Background(), links)
		if err != nil {
			t.Fatalf("%s: %v", r.ServiceID, err)
		}
		if len(got) != len(links) {
			t.Fatalf("%s answered %d of %d links", r.ServiceID, len(got), len(links))
		}
		for i, a := range got {
			if a != core.AvailUncheckable {
				t.Errorf("%s link %d = %q, want uncheckable", r.ServiceID, i, a)
			}
		}
	}
}

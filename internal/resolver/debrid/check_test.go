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

// TestAllDebridCheckLinks drives /link/infos against a payload shaped like the
// live v4 API (verified against docs.alldebrid.com).
//
// The endpoint was chosen because it is free: it reports the name and size a
// hoster gives and hands back no download link, so there is nothing to bill. The
// call that spends traffic is /link/unlock, and this test fails if the check
// ever goes near it.
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
		// Answered out of order and one link short, which is the shape this
		// decoder has to survive: nothing in AllDebrid's reply promises the order
		// of the request, and reading it back by position puts every verdict after
		// the first surprise on the wrong row.
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
		// Password-protected is a file that demonstrably exists and that we still
		// cannot promise anybody can have. That is the fourth state exactly.
		core.AvailUncheckable,
		// No entry came back for it at all.
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

// TestAllDebridCheckChunksTheBatch pins the split. AllDebrid documents no
// ceiling on the size of the array, so the batch is cut at a size no answer to
// that question can break - and a cut that drops its own tail is the failure
// mode: the links at the end come back as "not checked" and nobody notices,
// because that is also what an unchecked link looks like.
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

// TestRealDebridCheckLinks drives /unrestrict/check against the live API's
// shapes (verified against api.real-debrid.com), including its error codes.
func TestRealDebridCheckLinks(t *testing.T) {
	var unrestricted int
	mux := http.NewServeMux()
	mux.HandleFunc("/unrestrict/link", func(http.ResponseWriter, *http.Request) { unrestricted++ })
	mux.HandleFunc("/unrestrict/check", func(w http.ResponseWriter, r *http.Request) {
		// The whole reason this endpoint is safe to call unasked is that it needs
		// no account. A token on it is a request Real-Debrid could attribute to
		// somebody, which is the one way this could ever start costing them.
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
			// 503 as well, and the status alone cannot tell it from the one above.
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

// noCheckService is a provider that can unlock and cannot check, which is the
// case the split between Service and LinkChecker exists for.
type noCheckService struct{}

func (noCheckService) ID() string    { return "nocheck" }
func (noCheckService) Label() string { return "No Check" }
func (noCheckService) Hosts(context.Context) (map[string]bool, error) {
	return map[string]bool{"h.example": true}, nil
}
func (noCheckService) Unlock(context.Context, string) (Direct, error) { return Direct{}, nil }

// TestResolverCheckWithoutAProvider pins what a resolver answers when it has no
// free way to ask: uncheckable for every link, and no error. Both halves matter.
// An error would be reported as a fault; core.AvailUnknown would put the links
// back among the ones nobody has looked at, seconds after somebody looked.
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

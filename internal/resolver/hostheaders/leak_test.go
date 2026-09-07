package hostheaders

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/accounts"
	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/resolver"
	"github.com/junkerderprovinz/knightloader/internal/rules"
)

// leaked is the value every assertion in this file hunts for. It is one
// distinctive string so that a hit anywhere is unambiguous.
const leaked = "SECRET-tR7q-nobody-may-print-this"

// renderings is every way a value can end up in a log line, a JSON response or
// a diagnostics bundle. fmt reaches Stringer for %v, %s and %+v and GoStringer
// for %#v; encoding/json reaches MarshalJSON. A type that closes all five
// cannot be printed wrongly by accident, which is the property this package
// needs and a "remember to redact at the call site" rule can never have.
func renderings(t *testing.T, v any) map[string]string {
	t.Helper()
	out := map[string]string{
		"%v":  fmt.Sprintf("%v", v),
		"%+v": fmt.Sprintf("%+v", v),
		"%#v": fmt.Sprintf("%#v", v),
		"%s":  fmt.Sprintf("%s", v),
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	out["json"] = string(b)
	return out
}

func mustNotLeak(t *testing.T, what string, v any) {
	t.Helper()
	for verb, text := range renderings(t, v) {
		if strings.Contains(text, leaked) {
			t.Errorf("%s printed with %s carries the header value: %s", what, verb, text)
		}
	}
}

func leakProfile(t *testing.T, origin string) Set {
	t.Helper()
	set, err := Normalize(Set{
		Origin: origin,
		Headers: []Header{
			{Name: "X-Auth-Token", Value: leaked},
			{Name: "Cookie", Value: "session=" + leaked},
			{Name: "Authorization", Value: "Bearer " + leaked},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

// TestTheTypesThemselvesCannotPrintAHeader is the first line of defence: no
// call site has to remember anything, because the value has no printable form.
func TestTheTypesThemselvesCannotPrintAHeader(t *testing.T) {
	set := leakProfile(t, "https://box.lan")
	mustNotLeak(t, "a Set", set)
	mustNotLeak(t, "a Set's header slice", set.Headers)
	for _, h := range set.Headers {
		mustNotLeak(t, "one Header", h)
	}
	// The names still come through, or a settings page could not say what a
	// profile holds.
	if got := fmt.Sprintf("%v", set); !strings.Contains(got, "X-Auth-Token") {
		t.Errorf("a redacted Set prints as %q, and a settings page needs the names in it", got)
	}
}

// TestARuleSetCarriesTheNameAndNeverTheHeaders is the diagnostics-bundle case
// and the reason rules.Action.Headers is a profile name.
//
// internal/api/routes_diagnostics.go serialises settings.Settings, which holds
// the Packagizer rule set, into the file a person attaches to a public bug
// report. A header value in a rule action would be in every one of those
// forever.
func TestARuleSetCarriesTheNameAndNeverTheHeaders(t *testing.T) {
	set := rules.Set{Rules: []rules.Rule{{
		Name:       "forum attachments",
		Conditions: []rules.Condition{{Field: rules.FieldHoster, Op: rules.OpEquals, Value: "forum.example.org"}},
		Action:     rules.Action{Headers: "forum", DownloadDir: "/downloads/forum"},
	}}}
	mustNotLeak(t, "a rule set", set)

	m, problems := rules.Compile(set)
	if len(problems) > 0 {
		t.Fatalf("Compile: %v", problems)
	}
	effect := m.Apply(rules.Candidate{URL: "https://forum.example.org/attachments/1/x.rar", Filename: "x.rar"})
	if effect.Headers != "forum" {
		t.Fatalf("Effect.Headers = %q, want the profile name the rule set", effect.Headers)
	}
	mustNotLeak(t, "a rule effect", effect)
}

// TestTheSealedFileHoldsCiphertextOnly is the at-rest half. It is the same
// assertion internal/accounts' own round-trip test makes, repeated here
// because this package is what decides which file the values land in - and
// landing in settings.json instead would defeat every other test above.
func TestTheSealedFileHoldsCiphertextOnly(t *testing.T) {
	dir := t.TempDir()
	acc, err := accounts.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(acc)
	if err := store.Save("forum", leakProfile(t, "https://forum.example.org")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), leaked) {
		t.Fatal("the header value is in accounts.json in the clear")
	}
	// And it really is stored, or the assertion above would be satisfied by
	// the feature simply not working.
	if got := store.Get("forum").Attach("https://forum.example.org/x"); got["X-Auth-Token"] != leaked {
		t.Fatalf("the profile did not survive the round trip: %v", store.Get("forum"))
	}
}

// TestTheSettingsListingHasNoValuesInIt is the HTTP-response case: whatever
// route eventually renders the profile list serialises this, and a value in it
// would go straight to a browser.
func TestTheSettingsListingHasNoValuesInIt(t *testing.T) {
	store := NewStore(mustAccounts(t))
	if err := store.Save("forum", leakProfile(t, "https://forum.example.org")); err != nil {
		t.Fatal(err)
	}
	list := store.List()
	if len(list) != 1 || list[0].ID != "forum" || list[0].Origin != "https://forum.example.org:443" {
		t.Fatalf("List = %v, want one entry for the forum profile", list)
	}
	if len(list[0].Headers) != 3 {
		t.Errorf("Headers = %v, want the three names a settings page has to show", list[0].Headers)
	}
	mustNotLeak(t, "the settings listing", list)
}

// TestNoUpdateTheAppWOULDPUBLISHCarriesAHeader walks every answer this package
// gives on a download path and serialises the core.Update the dispatcher
// builds from it, the way internal/app does on a resolve failure
// (core.Update{Status: StatusError, Err: err.Error()}) and on a success.
//
// An Update is the value that reaches the task list, the log ring and from
// there the diagnostics bundle, so this is the shape that actually has to be
// clean - not just the types.
func TestNoUpdateTheAppWOULDPUBLISHCarriesAHeader(t *testing.T) {
	cdn := newSite(t)
	cdn.serve(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusForbidden) })

	forum := newSite(t)
	forum.serve(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/away":
			http.Redirect(w, r, cdn.URL+"/file.zip", http.StatusFound)
		case "/gone":
			http.NotFound(w, r)
		default:
			_, _ = w.Write([]byte("bytes"))
		}
	})

	store := NewStore(mustAccounts(t))
	if err := store.Save("forum", leakProfile(t, forum.URL)); err != nil {
		t.Fatal(err)
	}
	res := Resolver{Profiles: store}

	// Every path a link can take through this resolver, including the two that
	// fail. A dead host is included because an unreachable server is where a
	// naive implementation puts the whole request into the error string.
	links := []string{
		forum.URL + "/file.zip",
		forum.URL + "/away",
		forum.URL + "/gone",
		"http://127.0.0.1:1/file.zip",
	}
	for _, link := range links {
		result, err := res.Resolve(context.Background(), resolver.Request{URL: link, Headers: "forum"})
		if err != nil {
			mustNotLeak(t, "the error for "+link, core.Update{Status: core.StatusError, Err: err.Error()})
			continue
		}
		// The Update the dispatcher publishes. Result.Headers is deliberately
		// NOT part of it: it goes to engine.Job and nowhere else, which is the
		// one place a header value is allowed to be.
		mustNotLeak(t, "the update for "+link, core.Update{
			Status: core.StatusRunning,
			Name:   result.Name,
			Size:   result.Size,
		})
		mustNotLeak(t, "the direct URL for "+link, result.DirectURL)
		mustNotLeak(t, "the availability for "+link, string(result.Available))
	}
}

// TestAnErrorNeverQuotesTheValueItRefused: the error paths are where a value
// most easily escapes, because the natural sentence to write names the thing
// that was wrong with it.
func TestAnErrorNeverQuotesTheValueItRefused(t *testing.T) {
	long := leaked + strings.Repeat("x", MaxValueLen)
	cases := []struct {
		what string
		err  error
	}{
		{"an oversized value", errOf(Normalize(Set{Origin: "https://box.lan", Headers: []Header{{Name: "X-A", Value: long}}}))},
		{"a line break in a value", errOf(Normalize(Set{Origin: "https://box.lan", Headers: []Header{{Name: "X-A", Value: leaked + "\r\nX-B: y"}}}))},
		{"a bad origin", errOf(Normalize(Set{Origin: "ftp://box.lan", Headers: []Header{{Name: "X-A", Value: leaked}}}))},
	}
	for _, c := range cases {
		if c.err == nil {
			t.Fatalf("%s: expected an error", c.what)
		}
		if strings.Contains(c.err.Error(), leaked) {
			t.Errorf("%s: the error quotes the value: %v", c.what, c.err)
		}
	}
}

func errOf(_ Set, err error) error { return err }

func mustAccounts(t *testing.T) *accounts.Store {
	t.Helper()
	a, err := accounts.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return a
}

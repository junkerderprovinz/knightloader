package proxycfg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func proxy() Entry {
	return Entry{ID: "p", Kind: KindHTTP, Host: "proxy.lan", Port: 8080, Enabled: true}
}

// TestSanitizeDropsUnusableEntries pins the rule the package comment makes: a
// half-configured proxy is dropped, not kept. A kept one would be enabled with
// no endpoint, and the traffic the user was hiding would go out over the plain
// connection with nothing to say so.
func TestSanitizeDropsUnusableEntries(t *testing.T) {
	cases := []struct {
		name string
		in   Entry
		keep bool
	}{
		{"configured proxy", proxy(), true},
		{"direct row with a filter", Entry{Kind: KindDirect, Filter: []string{"nas.local"}, Enabled: true}, true},
		{"inert row", Entry{Kind: KindNone}, true},
		{"row with no type yet", Entry{}, true},
		{"socks5 with credentials", Entry{Kind: KindSOCKS5, Host: "10.0.0.5", Port: 1080, Username: "u", Password: "s", Enabled: true}, true},
		{"no host", Entry{Kind: KindHTTP, Port: 8080, Enabled: true}, false},
		{"no port", Entry{Kind: KindSOCKS5, Host: "proxy.lan", Enabled: true}, false},
		{"port out of range", Entry{Kind: KindHTTP, Host: "proxy.lan", Port: 70000, Enabled: true}, false},
		{"negative port", Entry{Kind: KindHTTP, Host: "proxy.lan", Port: -1, Enabled: true}, false},
		{"unknown type", Entry{Kind: "socks9", Host: "proxy.lan", Port: 1080, Enabled: true}, false},
		{"a whole URL pasted into the host field", Entry{Kind: KindHTTP, Host: "http://proxy.lan:8080/", Port: 8080, Enabled: true}, false},
		{"host with a space", Entry{Kind: KindHTTP, Host: "proxy lan", Port: 8080, Enabled: true}, false},
		{"host with credentials in it", Entry{Kind: KindHTTP, Host: "u@proxy.lan", Port: 8080, Enabled: true}, false},
		// A filter that can never match is refused rather than kept, because a
		// kept one turns the entry into a row that claims nothing: the picker
		// finds no owner for the host and answers with a plain download, which is
		// the leak the filter was typed in to prevent.
		{"a filter path.Match cannot parse", Entry{Kind: KindHTTP, Host: "proxy.lan", Port: 8080, Filter: []string{"[oops"}, Enabled: true}, false},
		{"a whole URL pasted into the filter", Entry{Kind: KindDirect, Filter: []string{"http://example.org"}, Enabled: true}, false},
		{"a filter with a space", Entry{Kind: KindDirect, Filter: []string{"example org"}, Enabled: true}, false},
		{"one good filter and one broken one", Entry{Kind: KindHTTP, Host: "proxy.lan", Port: 8080, Filter: []string{"example.org", "[oops"}, Enabled: true}, false},
		{"a wildcard filter is fine", Entry{Kind: KindHTTP, Host: "proxy.lan", Port: 8080, Filter: []string{"*.example.org"}, Enabled: true}, true},
		// A disabled row is configuration the user still wants, not rubbish.
		{"disabled but complete", Entry{Kind: KindHTTP, Host: "proxy.lan", Port: 8080}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Sanitize([]Entry{c.in})
			if kept := len(got) == 1; kept != c.keep {
				t.Fatalf("Sanitize(%v) kept %d entries, want kept=%v", c.in, len(got), c.keep)
			}
			// Validate has to agree, or the API would accept a save whose row
			// Sanitize then throws away without a word.
			if err := Validate(c.in); (err == nil) != c.keep {
				t.Fatalf("Validate(%v) = %v, want error=%v", c.in, err, !c.keep)
			}
		})
	}
}

// TestSanitizeReadsAnEmptyTypeAsInert covers the row a UI creates before the
// user has chosen anything. Rejecting it would delete it on the next save.
func TestSanitizeReadsAnEmptyTypeAsInert(t *testing.T) {
	got := Sanitize([]Entry{{Enabled: true}})
	if len(got) != 1 || got[0].Kind != KindNone {
		t.Fatalf("Sanitize(empty type) = %+v, want one entry of kind none", got)
	}
}

// TestSanitizeClearsWhatCannotBeSent checks the fields that are meaningless for
// their kind. Each one left in place would persist a secret or an endpoint that
// no code path can ever use, which is how a settings file grows things that look
// like configuration and are not.
func TestSanitizeClearsWhatCannotBeSent(t *testing.T) {
	cases := []struct {
		name  string
		in    Entry
		check func(*testing.T, Entry)
	}{
		{
			"a password with no user name",
			Entry{Kind: KindHTTP, Host: "proxy.lan", Port: 8080, Password: "s", Enabled: true},
			func(t *testing.T, e Entry) {
				if e.Password != "" {
					t.Errorf("password kept without a user name: %q", e.Password)
				}
			},
		},
		{
			"socks4 has no password field",
			Entry{Kind: KindSOCKS4, Host: "proxy.lan", Port: 1080, Username: "u", Password: "s", Enabled: true},
			func(t *testing.T, e Entry) {
				if e.Password != "" {
					t.Errorf("socks4 kept a password: %q", e.Password)
				}
				if e.Username != "u" {
					t.Errorf("socks4 lost its user id: %q", e.Username)
				}
			},
		},
		{
			"direct keeps only its filter",
			Entry{Kind: KindDirect, Host: "proxy.lan", Port: 8080, Username: "u", Password: "s", Filter: []string{"nas.local"}, Enabled: true},
			func(t *testing.T, e Entry) {
				if e.Host != "" || e.Port != 0 || e.Username != "" || e.Password != "" {
					t.Errorf("direct kept an endpoint: %+v", e)
				}
				if len(e.Filter) != 1 || e.Filter[0] != "nas.local" {
					t.Errorf("direct lost its filter: %v", e.Filter)
				}
			},
		},
		{
			"a password keeps its spaces",
			Entry{Kind: KindHTTP, Host: "proxy.lan", Port: 8080, Username: " u ", Password: " s ", Enabled: true},
			func(t *testing.T, e Entry) {
				if e.Password != " s " {
					t.Errorf("password was trimmed to %q", e.Password)
				}
				if e.Username != "u" {
					t.Errorf("user name was not trimmed: %q", e.Username)
				}
			},
		},
		{
			"filter is folded and deduplicated",
			Entry{Kind: KindHTTP, Host: "proxy.lan", Port: 8080, Filter: []string{" Example.ORG ", "example.org", "", "other.net."}, Enabled: true},
			func(t *testing.T, e Entry) {
				if !reflect.DeepEqual(e.Filter, []string{"example.org", "other.net"}) {
					t.Errorf("filter = %v", e.Filter)
				}
			},
		},
		{
			"an absurd limit is capped",
			Entry{Kind: KindHTTP, Host: "proxy.lan", Port: 8080, MaxDownloads: 5000, Enabled: true},
			func(t *testing.T, e Entry) {
				if e.MaxDownloads != maxDownloadsCap {
					t.Errorf("MaxDownloads = %d, want %d", e.MaxDownloads, maxDownloadsCap)
				}
			},
		},
		{
			"a negative limit means the default",
			Entry{Kind: KindHTTP, Host: "proxy.lan", Port: 8080, MaxDownloads: -3, Enabled: true},
			func(t *testing.T, e Entry) {
				if e.MaxDownloads != 0 {
					t.Errorf("MaxDownloads = %d, want 0", e.MaxDownloads)
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Sanitize([]Entry{c.in})
			if len(got) != 1 {
				t.Fatalf("Sanitize dropped %+v", c.in)
			}
			c.check(t, got[0])
		})
	}
}

// TestSanitizeIsIdempotent matters because NewPicker sanitizes again behind the
// caller. If a second pass changed anything, the list the user sees and the list
// the picker walks would drift apart.
func TestSanitizeIsIdempotent(t *testing.T) {
	in := []Entry{
		{Kind: "HTTP ", Host: " Proxy.LAN. ", Port: 8080, Username: " u ", Password: "s", Order: 9, Enabled: true},
		{Kind: KindDirect, Filter: []string{"NAS.local", "nas.local"}, Order: 3, Enabled: true},
		{Kind: KindHTTP, Port: 8080, Order: 1, Enabled: true}, // dropped
		{ID: "keep", Kind: KindSOCKS5, Host: "10.0.0.5", Port: 1080, Order: 3, Enabled: true},
	}
	once := Sanitize(in)
	twice := Sanitize(once)
	if !reflect.DeepEqual(once, twice) {
		t.Fatalf("Sanitize is not idempotent:\nonce  = %+v\ntwice = %+v", once, twice)
	}
	if len(once) != 3 {
		t.Fatalf("Sanitize kept %d entries, want 3", len(once))
	}
}

// TestSanitizeAssignsStableIDsAndCompactOrder pins both halves of identify: an
// ID the API already handed out survives, and the order becomes the sequence the
// user is actually looking at rather than whatever the sort left behind.
func TestSanitizeAssignsStableIDsAndCompactOrder(t *testing.T) {
	in := []Entry{
		{Kind: KindHTTP, Host: "a.lan", Port: 1, Order: 5, Enabled: true},
		{ID: "keep", Kind: KindHTTP, Host: "b.lan", Port: 2, Order: 2, Enabled: true},
		{ID: "keep", Kind: KindHTTP, Host: "c.lan", Port: 3, Order: 2, Enabled: true},
	}
	got := Sanitize(in)
	wantHost := []string{"b.lan", "c.lan", "a.lan"}
	wantID := []string{"keep", "1", "2"}
	if len(got) != 3 {
		t.Fatalf("Sanitize kept %d entries, want 3", len(got))
	}
	for i, e := range got {
		if e.Host != wantHost[i] {
			t.Errorf("entry %d host = %q, want %q", i, e.Host, wantHost[i])
		}
		if e.ID != wantID[i] {
			t.Errorf("entry %d id = %q, want %q", i, e.ID, wantID[i])
		}
		if e.Order != i {
			t.Errorf("entry %d order = %d, want %d", i, e.Order, i)
		}
	}
}

// TestURLCarriesCredentials is the builder's contract: the scheme http.Transport
// and x/net/proxy expect, an IPv6 host that stays one host, and credentials in
// the userinfo where a dialer looks for them.
func TestURLCarriesCredentials(t *testing.T) {
	cases := []struct {
		name string
		in   Entry
		want string // "" means no URL at all
	}{
		{"http", Entry{Kind: KindHTTP, Host: "proxy.lan", Port: 8080}, "http://proxy.lan:8080"},
		{"https with a user only", Entry{Kind: KindHTTPS, Host: "proxy.lan", Port: 8443, Username: "u"}, "https://u@proxy.lan:8443"},
		{"http with user and password", Entry{Kind: KindHTTP, Host: "proxy.lan", Port: 8080, Username: "u", Password: "s"}, "http://u:s@proxy.lan:8080"},
		{"socks5", Entry{Kind: KindSOCKS5, Host: "10.0.0.5", Port: 1080, Username: "u", Password: "s"}, "socks5://u:s@10.0.0.5:1080"},
		// SOCKS4 has a user id and no password field, so a password must not be
		// written into a URL that would only ever carry it as far as a log file.
		{"socks4 drops the password", Entry{Kind: KindSOCKS4, Host: "10.0.0.5", Port: 1080, Username: "u", Password: "s"}, "socks4://u@10.0.0.5:1080"},
		{"socks4a", Entry{Kind: KindSOCKS4A, Host: "10.0.0.5", Port: 1080}, "socks4a://10.0.0.5:1080"},
		{"ipv6 is bracketed", Entry{Kind: KindHTTP, Host: "2001:db8::1", Port: 8080}, "http://[2001:db8::1]:8080"},
		// nil is what http.Transport.Proxy wants back for "do not proxy this".
		{"direct has no URL", Entry{Kind: KindDirect}, ""},
		{"none has no URL", Entry{Kind: KindNone}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := c.in.URL()
			got := ""
			if u != nil {
				got = u.String()
			}
			if got != c.want {
				t.Errorf("URL() = %q, want %q", got, c.want)
			}
		})
	}
	// A dialer reads the password off the userinfo, so it has to survive the
	// round trip through url.URL rather than merely appear in the string.
	u := Entry{Kind: KindSOCKS5, Host: "10.0.0.5", Port: 1080, Username: "u", Password: "s3:cr@t"}.URL()
	if pw, ok := u.User.Password(); !ok || pw != "s3:cr@t" {
		t.Errorf("URL().User.Password() = %q,%v, want the password back verbatim", pw, ok)
	}
}

// TestNeedsOwnDialer pins the one fact a caller cannot guess: net/http drives
// socks5 by itself but has never understood socks4, so those entries need a
// dialer the caller supplies instead of Transport.Proxy.
func TestNeedsOwnDialer(t *testing.T) {
	cases := map[Kind]bool{
		KindHTTP:    false,
		KindHTTPS:   false,
		KindSOCKS5:  false,
		KindDirect:  false,
		KindNone:    false,
		KindSOCKS4:  true,
		KindSOCKS4A: true,
	}
	for k, want := range cases {
		if got := (Entry{Kind: k}).NeedsOwnDialer(); got != want {
			t.Errorf("%s: NeedsOwnDialer() = %v, want %v", k, got, want)
		}
	}
}

// TestPasswordNeverLeavesTheEntry is the proof the password stays put. String
// never assembles it, so no %v anywhere in the app can spill it, and Redacted
// removes it from anything on its way to a client.
func TestPasswordNeverLeavesTheEntry(t *testing.T) {
	const secret = "hunter2-schwarzpulver"
	e := Entry{ID: "1", Kind: KindHTTPS, Host: "proxy.lan", Port: 8443, Username: "u", Password: secret, Enabled: true}

	rendered := map[string]string{
		"String()":        e.String(),
		"%v":              fmt.Sprintf("%v", e),
		"%+v":             fmt.Sprintf("%+v", e),
		"%s":              fmt.Sprintf("%s", e),
		"inside a slice":  fmt.Sprintf("%v", []Entry{e}),
		"inside an error": fmt.Errorf("connect via %v failed", e).Error(),
		"Redacted()":      fmt.Sprint(e.Redacted()),
		"sanitized":       fmt.Sprint(Sanitize([]Entry{e})),
	}
	for what, got := range rendered {
		if strings.Contains(got, secret) {
			t.Errorf("%s leaked the password: %q", what, got)
		}
		if !strings.Contains(got, "proxy.lan") {
			t.Errorf("%s does not identify the entry at all: %q", what, got)
		}
	}

	if pw := e.Redacted().Password; pw != "" {
		t.Errorf("Redacted().Password = %q, want empty", pw)
	}
	// The API marshals the redacted entry, which is the path that would put the
	// password in a browser's network tab.
	b, err := json.Marshal(e.Redacted())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if bytes.Contains(b, []byte(secret)) {
		t.Errorf("redacted JSON leaked the password: %s", b)
	}
	// Redacting must not cost the identity of the row, or the client cannot
	// match it back to what it sent.
	if e.Redacted().ID != e.ID || e.Redacted().Host != e.Host {
		t.Errorf("Redacted() lost identifying fields: %+v", e.Redacted())
	}
	// Persisting is the one place the password does belong, otherwise a restart
	// silently loses it.
	stored, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Contains(stored, []byte(secret)) {
		t.Errorf("the password is not persisted at all: %s", stored)
	}
}

// TestMatchesHostFilter pins the filter language, including the two rules a user
// will rely on without reading anything: a bare domain covers its subdomains,
// and a target that arrives with a port still matches.
func TestMatchesHostFilter(t *testing.T) {
	cases := []struct {
		name   string
		filter []string
		host   string
		want   bool
	}{
		{"no filter matches everything", nil, "example.org", true},
		{"exact", []string{"example.org"}, "example.org", true},
		{"subdomain", []string{"example.org"}, "dl2.example.org", true},
		{"not a suffix by accident", []string{"example.org"}, "notexample.org", false},
		{"another host", []string{"example.org"}, "other.net", false},
		{"wildcard", []string{"*.example.org"}, "dl2.example.org", true},
		{"wildcard does not cover the bare domain", []string{"*.example.org"}, "example.org", false},
		{"any of several patterns", []string{"a.net", "example.org"}, "example.org", true},
		{"case is folded on both sides", []string{"Example.ORG"}, "EXAMPLE.org", true},
		{"trailing root dot", []string{"example.org"}, "example.org.", true},
		{"target with a port", []string{"example.org"}, "example.org:443", true},
		{"ipv6 target with a port", []string{"2001:db8::1"}, "[2001:db8::1]:443", true},
		{"bracketed ipv6 filter", []string{"[2001:db8::1]"}, "[2001:db8::1]:443", true},
		{"a filter typed with a port still matches", []string{"example.org:443"}, "example.org", true},
		{"empty host", []string{"example.org"}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw := Entry{Kind: KindDirect, Enabled: true, Filter: c.filter}
			got := Sanitize([]Entry{raw})
			if len(got) != 1 {
				t.Fatalf("Sanitize dropped the entry")
			}
			if m := got[0].Matches(c.host); m != c.want {
				t.Errorf("Matches(%q) with filter %v = %v, want %v", c.host, got[0].Filter, m, c.want)
			}
			// The same answer off an entry that never went through Sanitize.
			// Matches is exported and app.go holds entries straight off a request
			// and out of settings.json; a filter that only works once it has been
			// folded is a filter that silently matches nothing wherever anybody
			// forgets, and the download leaves over the wrong connection.
			if m := raw.Matches(c.host); m != c.want {
				t.Errorf("unsanitized Matches(%q) with filter %v = %v, want %v", c.host, c.filter, m, c.want)
			}
		})
	}
}

// TestFilterThatCanNeverMatchIsRefusedNotKept is the silent-swallow case: a
// filter the user mistyped must come back as an error, not be persisted as
// configuration that quietly does nothing. Keeping it would leave an entry that
// claims no host at all, so the picker would find no owner for the hoster the
// user pointed at it and answer with a plain unproxied download.
func TestFilterThatCanNeverMatchIsRefusedNotKept(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		ok      bool
	}{
		{"plain host", "example.org", true},
		{"wildcard", "*.example.org", true},
		{"single character wildcard", "dl?.example.org", true},
		{"character class", "dl[12].example.org", true},
		{"ipv6 literal", "2001:db8::1", true},
		// The bracketed form is what a user copies out of a URL bar. It has to
		// survive, because the brackets otherwise read as a character class.
		{"bracketed ipv6 literal", "[2001:db8::1]", true},
		{"unterminated class", "[oops", false},
		{"pasted url", "http://example.org", false},
		{"pasted url with a port", "https://example.org:443/files", false},
		{"credentials", "u@example.org", false},
		{"space", "example org", false},
		{"backslash", `example\.org`, false},
		{"stray colon", "example.org:oops:443", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := Entry{Kind: KindDirect, Enabled: true, Filter: []string{c.pattern}}
			err := Validate(e)
			if (err == nil) != c.ok {
				t.Fatalf("Validate(filter %q) = %v, want ok=%v", c.pattern, err, c.ok)
			}
			if kept := len(Sanitize([]Entry{e})); (kept == 1) != c.ok {
				t.Fatalf("Sanitize kept %d entries for filter %q, want ok=%v", kept, c.pattern, c.ok)
			}
			// The API repeats this back to the user, who has to be able to tell
			// which of several rows it is about, and it has to be the pattern as
			// typed rather than the folded form nobody entered.
			if err != nil && !strings.Contains(err.Error(), fmt.Sprintf("%q", c.pattern)) {
				t.Errorf("error does not name the pattern: %v", err)
			}
		})
	}
	// Defence in depth for an entry built in code and never validated: a pattern
	// that cannot be parsed matches nothing rather than everything, because
	// widening it would push the whole queue through one connection.
	if matchPattern("[oops", "example.org") {
		t.Errorf("an unparseable pattern widened to match everything")
	}
}

// TestMergeRestoresPasswordsTheClientNeverSaw covers the round trip that would
// otherwise wipe every proxy password: the API sends Redacted entries, the
// settings page posts them back unchanged, and without Merge the save clears
// them all.
func TestMergeRestoresPasswordsTheClientNeverSaw(t *testing.T) {
	prev := Sanitize([]Entry{{ID: "p", Kind: KindHTTP, Host: "proxy.lan", Port: 8080, Username: "u", Password: "s", Enabled: true}})
	sent := prev[0].Redacted()

	t.Run("unchanged row keeps its password", func(t *testing.T) {
		got := Sanitize(Merge([]Entry{sent}, prev))
		if got[0].Password != "s" {
			t.Errorf("password = %q, want it carried over", got[0].Password)
		}
	})
	t.Run("a new password wins", func(t *testing.T) {
		next := sent
		next.Password = "fresh"
		got := Sanitize(Merge([]Entry{next}, prev))
		if got[0].Password != "fresh" {
			t.Errorf("password = %q, want the one the client sent", got[0].Password)
		}
	})
	t.Run("a changed user name means different credentials", func(t *testing.T) {
		next := sent
		next.Username = "other"
		got := Sanitize(Merge([]Entry{next}, prev))
		if got[0].Password != "" {
			t.Errorf("password = %q, want it dropped with the user name it belonged to", got[0].Password)
		}
	})
	t.Run("clearing the user name clears the password", func(t *testing.T) {
		next := sent
		next.Username = ""
		got := Sanitize(Merge([]Entry{next}, prev))
		if got[0].Password != "" {
			t.Errorf("password = %q, want empty", got[0].Password)
		}
	})
	// The client posting this list back is the one the password was withheld
	// from. If it could move the row somewhere else and keep the secret, it could
	// point a password it was never allowed to read at a machine it controls and
	// have the app hand it over on the next download.
	t.Run("a moved row does not take the password with it", func(t *testing.T) {
		cases := []struct {
			name string
			edit func(Entry) Entry
		}{
			{"a different host", func(e Entry) Entry { e.Host = "attacker.example"; return e }},
			{"a different port", func(e Entry) Entry { e.Port = 9999; return e }},
			{"a different type", func(e Entry) Entry { e.Kind = KindSOCKS5; return e }},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				got := Merge([]Entry{c.edit(sent)}, prev)
				if got[0].Password != "" {
					t.Errorf("password = %q, want it left behind with the old connection", got[0].Password)
				}
			})
		}
	})
	t.Run("a row that only moved in the list keeps its password", func(t *testing.T) {
		next := sent
		next.Order = 7
		next.Enabled = !next.Enabled
		got := Sanitize(Merge([]Entry{next}, prev))
		if got[0].Password != "s" {
			t.Errorf("password = %q, want it carried over: nothing about the connection changed", got[0].Password)
		}
	})
	t.Run("an unknown row gets nothing", func(t *testing.T) {
		next := sent
		next.ID = "elsewhere"
		got := Merge([]Entry{next}, prev)
		if got[0].Password != "" {
			t.Errorf("password = %q, want empty for an id we never issued", got[0].Password)
		}
	})
}

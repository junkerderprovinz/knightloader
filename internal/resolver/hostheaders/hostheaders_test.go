package hostheaders

import (
	"testing"
)

func TestOriginOfFillsInThePortAndRefusesEverythingElse(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://cloud.example.org/s/abc/download", "https://cloud.example.org:443"},
		{"https://cloud.example.org:443/x", "https://cloud.example.org:443"},
		{"HTTPS://Cloud.Example.ORG/x", "https://cloud.example.org:443"},
		{"http://box.lan:8080/file.zip", "http://box.lan:8080"},
		{"http://box.lan/file.zip", "http://box.lan:80"},
		// A downgrade is a different origin on purpose: a credential forwarded
		// onto a plaintext hop is a credential given to everyone on the path.
		{"http://cloud.example.org/x", "http://cloud.example.org:80"},
		{"ftp://box.lan/file.zip", ""},
		{"magnet:?xt=urn:btih:abc", ""},
		{"/relative/path", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := OriginOf(c.in); got != c.want {
			t.Errorf("OriginOf(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAttachSendsNothingOffItsOwnOrigin(t *testing.T) {
	set, err := Normalize(Set{
		Origin:  "https://cloud.example.org/s/abc",
		Headers: []Header{{Name: "authorization", Value: secretBasic}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := set.Attach("https://cloud.example.org/remote.php/file.zip"); got["Authorization"] != secretBasic {
		t.Errorf("Attach on the profile's own origin returned %d headers, want the stored one", len(got))
	}
	// Every way a URL can be somewhere else: another host, another port,
	// another scheme.
	for _, off := range []string{
		"https://cdn.example.net/file.zip",
		"https://cloud.example.org:8443/file.zip",
		"http://cloud.example.org/file.zip",
		"https://evil.cloud.example.org/file.zip",
		"ftp://cloud.example.org/file.zip",
		"not a url at all",
	} {
		if got := set.Attach(off); got != nil {
			t.Errorf("Attach(%q) returned headers; it is not the profile's origin", off)
		}
	}
}

// TestNormalizeCollapsesOneHeaderToOneEntry pins the reason normalising
// happens at save time: two spellings of one name are one header to every
// server, and a profile holding both would send whichever the map iteration
// reached last.
func TestNormalizeCollapsesOneHeaderToOneEntry(t *testing.T) {
	set, err := Normalize(Set{
		Origin: "https://box.lan:8080",
		Headers: []Header{
			{Name: "x-auth-token", Value: "first"},
			{Name: "  Referer ", Value: "https://box.lan:8080/forum"},
			{Name: "X-Auth-Token", Value: "second"},
			{Name: "", Value: "orphan"},
			{Name: "X-Blank", Value: "   "},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	names := set.Names()
	if len(names) != 2 || names[0] != "Referer" || names[1] != "X-Auth-Token" {
		t.Fatalf("Names = %v, want [Referer X-Auth-Token] sorted and de-duplicated", names)
	}
	if got := set.Attach("https://box.lan:8080/x"); got["X-Auth-Token"] != "second" {
		t.Errorf("X-Auth-Token = %q, want the last value written to win", got["X-Auth-Token"])
	}
}

func TestNormalizeRefusesWhatCannotBeSent(t *testing.T) {
	cases := []struct {
		name string
		set  Set
	}{
		{"no origin", Set{Headers: []Header{{Name: "A", Value: "b"}}}},
		{"origin is not http", Set{Origin: "ftp://box.lan", Headers: []Header{{Name: "A", Value: "b"}}}},
		{"a line break in the value is request splitting", Set{
			Origin:  "https://box.lan",
			Headers: []Header{{Name: "X-A", Value: "one\r\nX-Injected: two"}},
		}},
		{"an oversized value", Set{
			Origin:  "https://box.lan",
			Headers: []Header{{Name: "X-A", Value: string(make([]byte, MaxValueLen+1))}},
		}},
	}
	for _, c := range cases {
		if _, err := Normalize(c.set); err == nil {
			t.Errorf("%s: Normalize accepted it", c.name)
		}
	}
}

// TestNormalizeRefusesTooManyHeaders bounds a paste that went in whole.
func TestNormalizeRefusesTooManyHeaders(t *testing.T) {
	s := Set{Origin: "https://box.lan"}
	for i := 0; i <= MaxHeaders; i++ {
		s.Headers = append(s.Headers, Header{Name: "X-N" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Value: "v"})
	}
	if _, err := Normalize(s); err == nil {
		t.Fatalf("Normalize accepted %d headers, the limit is %d", len(s.Headers), MaxHeaders)
	}
}

func TestProfileIDIsWhatARuleCanAddress(t *testing.T) {
	cases := []struct{ in, want string }{
		{"forum", "forum"},
		{"  Forum  ", "forum"},
		{"my-box_2.0", "my-box_2.0"},
		{"", ""},
		{"has space", ""},
		{"slash/es", ""},
		{"nul\x00", ""},
		{string(make([]byte, MaxProfileID+1)), ""},
	}
	for _, c := range cases {
		if got := ProfileID(c.in); got != c.want {
			t.Errorf("ProfileID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

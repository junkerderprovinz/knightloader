package linkscan

import "testing"

func TestExtract(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			"one per line",
			"https://example.com/a\nhttps://example.com/b\n",
			[]string{"https://example.com/a", "https://example.com/b"},
		},
		{
			// Pasted out of a chat client, where several links share a line.
			"several on one line",
			"https://example.com/a https://example.com/b",
			[]string{"https://example.com/a", "https://example.com/b"},
		},
		{
			"byte-order mark does not eat the first link",
			byteOrderMark + "https://example.com/a\nhttps://example.com/b",
			[]string{"https://example.com/a", "https://example.com/b"},
		},
		{
			"prose around the links",
			"Here you go: https://example.com/a, and also https://example.com/b.",
			[]string{"https://example.com/a", "https://example.com/b"},
		},
		{
			"duplicates collapse, first-seen order kept",
			"https://example.com/b\nhttps://example.com/a\nhttps://example.com/b",
			[]string{"https://example.com/b", "https://example.com/a"},
		},
		{
			// The whole point of requiring a scheme or a domain shape: a
			// README must not become a download list of its own words.
			"prose with no links",
			"This archive contains the film in 2160p.",
			nil,
		},
		{
			"magnet link",
			"magnet:?xt=urn:btih:deadbeefdeadbeefdeadbeefdeadbeefdeadbeef&dn=Some+File",
			[]string{"magnet:?xt=urn:btih:deadbeefdeadbeefdeadbeefdeadbeefdeadbeef&dn=Some+File"},
		},
		{
			"scheme case is folded but the match keeps the original case",
			"HTTPS://Example.com/File.ZIP",
			[]string{"HTTPS://Example.com/File.ZIP"},
		},
		{
			"a matched trailing bracket survives, Wikipedia-style",
			"See https://en.wikipedia.org/wiki/Go_(programming_language) for more.",
			[]string{"https://en.wikipedia.org/wiki/Go_(programming_language)"},
		},
		{
			"an unmatched trailing bracket from prose wrapping is stripped",
			"(see https://example.org/page)",
			[]string{"https://example.org/page"},
		},
		{
			// Three trailing closes, one opener inside the token (the outer
			// "(" sits before "https" and so is never part of the token at
			// all): strip the two excess, keep the one that is matched.
			"excess trailing brackets collapse to the one actually opened",
			"(https://en.wikipedia.org/wiki/Foo_(bar)))",
			[]string{"https://en.wikipedia.org/wiki/Foo_(bar)"},
		},
		{
			"markdown and chat wrapping around a link is stripped",
			"`https://example.org/file.zip` and *https://example.org/other.zip*",
			[]string{"https://example.org/file.zip", "https://example.org/other.zip"},
		},
		{
			"curly quotes around a link are stripped",
			"“https://example.org/file.zip”",
			[]string{"https://example.org/file.zip"},
		},
		{
			"a URL is never trimmed of its own trailing slash",
			"https://example.org/downloads/",
			[]string{"https://example.org/downloads/"},
		},
		{
			"bare host with no scheme falls back to the whole line",
			"example.org/file.zip",
			[]string{"https://example.org/file.zip"},
		},
		{
			"a bare host needs no path at all",
			"example.org",
			[]string{"https://example.org"},
		},
		{
			"bare host with a port and a path",
			"example.org:8080/some/file.zip",
			[]string{"https://example.org:8080/some/file.zip"},
		},
		{
			"a bare host wrapped in angle brackets, the plain-text convention",
			"<example.org/file.zip>",
			[]string{"https://example.org/file.zip"},
		},
		{
			"a single word is never read as a bare host",
			"just-one-word",
			nil,
		},
		{
			"a version number is never read as a bare host",
			"2.0.1",
			nil,
		},
		{
			"a bare domain floating inside a sentence is not pulled out",
			"See example.org for the file.",
			nil,
		},
		{
			"a mail client's hard wrap is rejoined into one link",
			"Grab it here:\n" +
				"https://example.com/a/very/long/path/that/got/wrapped/right/here/by/the/\n" +
				"mail/client/and/continues/on/this/line.zip\n" +
				"Thanks!",
			[]string{"https://example.com/a/very/long/path/that/got/wrapped/right/here/by/the/mail/client/and/continues/on/this/line.zip"},
		},
		{
			// The false positive this design accepts is the mirror image of
			// the one above: a wrap landing exactly where a NEW sentence also
			// starts lower-case. Documented as a known limit, not fixed here.
			"a capitalised new sentence right after a wrap is not absorbed",
			"https://example.com/a/wrapped/link/that/ends/right/here/at/the/break\nThanks for downloading!",
			[]string{"https://example.com/a/wrapped/link/that/ends/right/here/at/the/break"},
		},
		{
			// The one bug this package exists to not have: two complete,
			// independent links pasted one per line must never fuse into one
			// just because the first happens to end lower-case, which nearly
			// every URL does.
			"two independent links never fuse into one",
			"https://example.com/first\nhttps://example.com/second",
			[]string{"https://example.com/first", "https://example.com/second"},
		},
		{
			"a quoted-printable soft break is undone even without a scheme wrap",
			"https://example.com/file=\r\nname.zip",
			[]string{"https://example.com/filename.zip"},
		},
		{
			"an IRI in its own script is not truncated mid-character",
			"https://xn--e1aybc.xn--p1ai/файл.zip",
			[]string{"https://xn--e1aybc.xn--p1ai/файл.zip"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Extract(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("Extract(%q) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("link %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestExtract_EmptyAndWhitespaceOnlyInputYieldNothing(t *testing.T) {
	for _, in := range []string{"", "   ", "\n\n\n", "\r\n\r\n"} {
		if got := Extract(in); len(got) != 0 {
			t.Errorf("Extract(%q) = %v, want none", in, got)
		}
	}
}

func TestLogicalLines_QuotedPrintableBothLineEndings(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"CRLF soft break", "abc=\r\ndef", []string{"abcdef"}},
		{"LF-only soft break", "abc=\ndef", []string{"abcdef"}},
		{"CR-only line ending normalises like LF", "one\rtwo", []string{"one", "two"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := logicalLines(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("logicalLines(%q) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("line %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestTrimToken(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://example.org/page", "https://example.org/page"},
		{"https://example.org/page.", "https://example.org/page"},
		{"https://example.org/page),", "https://example.org/page"},
		{"https://example.org/wiki/Foo_(bar)", "https://example.org/wiki/Foo_(bar)"},
		{"https://example.org/wiki/Foo_(bar))", "https://example.org/wiki/Foo_(bar)"},
		{"https://example.org/a[1]", "https://example.org/a[1]"},
		{"https://example.org/a[1])", "https://example.org/a[1]"},
	}
	for _, tt := range tests {
		if got := trimToken(tt.in); got != tt.want {
			t.Errorf("trimToken(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTrimTokenDoesNotBlowUpOnAPathologicalBracketRun(t *testing.T) {
	// One opener followed by ten thousand closers: strip down to the single
	// balanced pair, not to nothing and not by recomputing strings.Count on
	// every one of them. This is a correctness-of-complexity test as much as
	// a value one - it must finish, not just finish correctly.
	tok := "https://example.org/a("
	for i := 0; i < 10000; i++ {
		tok += ")"
	}
	got := trimToken(tok)
	if got != "https://example.org/a()" {
		t.Errorf("trimToken of a long unmatched run = %q, want the one balanced pair kept", got)
	}
}

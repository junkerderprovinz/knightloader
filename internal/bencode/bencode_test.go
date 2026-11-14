package bencode

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"testing"
)

func TestValuesDecodeIntoGoTypes(t *testing.T) {
	v, err := Decode([]byte("d1:ai-3e1:bl4:spami7ee1:c0:e"))
	if err != nil {
		t.Fatal(err)
	}
	d := v.(map[string]any)
	if d["a"] != int64(-3) || string(d["c"].([]byte)) != "" {
		t.Fatalf("decoded %#v", d)
	}
	l := d["b"].([]any)
	if string(l[0].([]byte)) != "spam" || l[1] != int64(7) {
		t.Fatalf("decoded list %#v", l)
	}
}

func TestEncodingSortsTheKeysOfADictionary(t *testing.T) {
	b, err := Encode(map[string]any{"zeta": int64(1), "alpha": "x", "mid": []any{[]byte("y"), 2}})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "d5:alpha1:x3:midl1:yi2ee4:zetai1ee" {
		t.Fatalf("encoded %s", b)
	}
}

func TestACanonicalDocumentComesBackByteForByte(t *testing.T) {
	src := "d8:announce3:url4:infod6:lengthi5e4:name1:a12:piece lengthi16384e6:pieces20:" + strings.Repeat("x", 20) + "6:source3:REDee"
	v, err := Decode([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Encode(v)
	if err != nil || string(out) != src {
		t.Fatalf("Encode = %q, %v", out, err)
	}
}

func TestBrokenDocumentsAreRefused(t *testing.T) {
	for _, in := range []string{
		"", "i", "i-0e", "i03e", "ie", "4:abc", "l", "d1:a", "d1:ai1e", "di1ei2ee",
		"d1:bi1e1:ai2ee", "d1:ai1e1:ai2ee", "i1ei2e", "-1:a", "99999999999999999999:x",
	} {
		if _, err := Decode([]byte(in)); err == nil {
			t.Errorf("Decode(%q) took it", in)
		}
	}
}

func TestDeepNestingIsRefusedRatherThanFollowed(t *testing.T) {
	in := strings.Repeat("l", 10000) + strings.Repeat("e", 10000)
	if _, err := Decode([]byte(in)); err == nil {
		t.Fatal("a list nested ten thousand deep was decoded")
	}
}

func TestTheRawInfoDictionaryHashesToTheInfoHash(t *testing.T) {
	info := "d6:lengthi5e4:name1:a12:piece lengthi16384e6:pieces20:" + strings.Repeat("x", 20) + "e"
	doc := "d8:announce3:url4:info" + info + "e"
	raw, err := Raw([]byte(doc), "info")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, []byte(info)) {
		t.Fatalf("Raw = %q", raw)
	}
	want := sha1.Sum([]byte(info))
	if got := InfoHash(raw); got != hex.EncodeToString(want[:]) {
		t.Fatalf("InfoHash = %s", got)
	}
}

func TestRawSaysWhenTheKeyIsMissing(t *testing.T) {
	if _, err := Raw([]byte("d1:ai1ee"), "info"); err == nil {
		t.Fatal("found a key that is not there")
	}
}

func FuzzWhatDecodesEncodesBackTheSame(f *testing.F) {
	for _, s := range []string{"i1e", "4:spam", "le", "de", "d1:ali1e2:bcee1:b0:e", "d4:infod6:lengthi5eee"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		v, err := Decode(in)
		if err != nil {
			return
		}
		out, err := Encode(v)
		if err != nil {
			t.Fatalf("Encode of a decoded value: %v", err)
		}
		if !bytes.Equal(out, in) {
			t.Fatalf("Decode then Encode turned %q into %q", in, out)
		}
	})
}

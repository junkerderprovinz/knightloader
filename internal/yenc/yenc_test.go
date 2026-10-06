package yenc

import (
	"bytes"
	"errors"
	"fmt"
	"hash/crc32"
	"strings"
	"testing"
)

// "123456789" is the standard check input for CRC32, whose check value is
// cbf43926. Each byte plus 42 is a printable character, so nothing is escaped.
const checkArticle = "=ybegin line=128 size=9 name=check.bin\r\n" +
	"[\\]^_`abc\r\n" +
	"=yend size=9 crc32=cbf43926\r\n"

func TestDecodeKnownVector(t *testing.T) {
	p, err := Decode([]byte(checkArticle))
	if err != nil {
		t.Fatal(err)
	}
	if string(p.Data) != "123456789" || p.Name != "check.bin" || p.FileSize != 9 || p.Begin != 0 {
		t.Fatalf("got %+v", p)
	}
	if p.Number != 1 || p.Total != 1 {
		t.Fatalf("a single-part file is part 1 of 1, got %d of %d", p.Number, p.Total)
	}
}

// The four bytes that become NUL, LF, CR and '=' after adding 42 are always
// escaped as '=' followed by the character plus 64.
func TestDecodeEscapes(t *testing.T) {
	raw := []byte{0xd6, 0xe0, 0xe3, 0x13}
	article := "=ybegin part=1 total=1 line=128 size=4 name=esc.bin\r\n" +
		"=ypart begin=1 end=4\r\n" +
		"=@=J=M=}\r\n" +
		fmt.Sprintf("=yend size=4 part=1 pcrc32=%08x\r\n", crc32.ChecksumIEEE(raw))
	p, err := Decode([]byte(article))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(p.Data, raw) {
		t.Fatalf("decoded % x, want % x", p.Data, raw)
	}
}

func TestDecodeMultipartOffsets(t *testing.T) {
	article := "=ybegin part=2 total=3 line=128 size=27 name=a file = b.bin\r\n" +
		"=ypart begin=10 end=18\r\n" +
		"[\\]^_`abc\r\n" +
		"=yend size=9 part=2 pcrc32=cbf43926\r\n"
	p, err := Decode([]byte(article))
	if err != nil {
		t.Fatal(err)
	}
	if p.Begin != 9 || p.End() != 18 || p.Number != 2 || p.Total != 3 || p.FileSize != 27 {
		t.Fatalf("got begin %d end %d part %d/%d size %d", p.Begin, p.End(), p.Number, p.Total, p.FileSize)
	}
	if p.Name != "a file = b.bin" {
		t.Fatalf("a name keeps its spaces and equals signs, got %q", p.Name)
	}
}

func TestDecodeSkipsTextBeforeHeader(t *testing.T) {
	p, err := Decode([]byte("posted with care\r\n\r\n" + checkArticle))
	if err != nil || string(p.Data) != "123456789" {
		t.Fatalf("got %q, %v", p.Data, err)
	}
}

func TestDecodeReportsDamage(t *testing.T) {
	damaged := strings.Replace(checkArticle, "abc", "abd", 1)
	if _, err := Decode([]byte(damaged)); !errors.Is(err, ErrCRC) {
		t.Fatalf("a changed byte must fail the checksum, got %v", err)
	}
	short := strings.Replace(checkArticle, "abc", "ab", 1)
	if _, err := Decode([]byte(short)); !errors.Is(err, ErrCRC) {
		t.Fatalf("a lost byte must fail the size check, got %v", err)
	}
}

func TestDecodeRefusesWhatIsNotYEnc(t *testing.T) {
	cases := map[string]string{
		"plain text":  "hello\r\nworld\r\n",
		"no trailer":  "=ybegin line=128 size=9 name=x\r\n[\\]^_`abc\r\n",
		"no size":     "=ybegin line=128 name=x\r\n=yend size=0\r\n",
		"no ypart":    "=ybegin part=1 total=2 line=128 size=9 name=x\r\n[\\]^_`abc\r\n=yend size=9\r\n",
		"wild ypart":  "=ybegin part=1 total=2 line=128 size=9 name=x\r\n=ypart begin=5 end=40\r\n=yend size=0\r\n",
		"bad pcrc32":  "=ybegin line=128 size=9 name=x\r\n[\\]^_`abc\r\n=yend size=9 crc32=zz\r\n",
		"empty input": "",
	}
	for name, article := range cases {
		if _, err := Decode([]byte(article)); err == nil {
			t.Errorf("%s: decoded without an error", name)
		}
	}
}

func TestDecodeRefusesASizeTheBodyCannotHold(t *testing.T) {
	cases := map[string]string{
		"single part": "=ybegin line=128 size=99999999999999999 name=x.bin\r\nabc\r\n=yend size=3\r\n",
		"ypart range": "=ybegin part=2 total=2 line=128 size=99999999999999999 name=x.bin\r\n" +
			"=ypart begin=4001 end=99999999999999999\r\nabc\r\n=yend size=3 part=2\r\n",
	}
	for name, article := range cases {
		if _, err := Decode([]byte(article)); !errors.Is(err, ErrCRC) {
			t.Errorf("%s: got %v, want a size mismatch", name, err)
		}
	}
}

func TestEncodeRoundTripsEveryByte(t *testing.T) {
	data := make([]byte, 1000)
	for i := range data {
		data[i] = byte(i * 7)
	}
	parts := []Part{
		{Name: "all.bin", FileSize: 1000, Number: 1, Total: 2, Begin: 0, Data: data[:600]},
		{Name: "all.bin", FileSize: 1000, Number: 2, Total: 2, Begin: 600, Data: data[600:]},
	}
	got := make([]byte, 1000)
	for _, p := range parts {
		var b bytes.Buffer
		if err := Encode(&b, p, 64); err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(b.String(), "\r\n") {
			if strings.HasPrefix(line, ".") {
				t.Fatalf("a line starts with a dot: %q", line)
			}
			if len(line) > 0 && (line[len(line)-1] == ' ' || line[len(line)-1] == '\t') {
				t.Fatalf("a line ends in white space: %q", line)
			}
		}
		d, err := Decode(b.Bytes())
		if err != nil {
			t.Fatalf("part %d: %v", p.Number, err)
		}
		copy(got[d.Begin:], d.Data)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("the parts put together differ from the file")
	}
}

func TestEncodeSinglePart(t *testing.T) {
	var b bytes.Buffer
	if err := Encode(&b, Part{Name: "check.bin", FileSize: 9, Number: 1, Total: 1, Data: []byte("123456789")}, 128); err != nil {
		t.Fatal(err)
	}
	if b.String() != checkArticle {
		t.Fatalf("got\n%q\nwant\n%q", b.String(), checkArticle)
	}
}

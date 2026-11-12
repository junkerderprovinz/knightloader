package httpx_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/httpx"
)

func TestProgressWriterCountsWhatPassesThrough(t *testing.T) {
	var buf bytes.Buffer
	var seen [][2]int64
	w := httpx.ProgressWriter(&buf, 10, func(done, total int64) { seen = append(seen, [2]int64{done, total}) })
	if _, err := io.Copy(w, io.MultiReader(strings.NewReader("abcd"), strings.NewReader("efghij"))); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "abcdefghij" {
		t.Fatalf("wrote %q", buf.String())
	}
	if last := seen[len(seen)-1]; last != [2]int64{10, 10} {
		t.Fatalf("last report %v, all %v", last, seen)
	}
}

func TestProgressWriterWithoutAReportIsTheWriterItself(t *testing.T) {
	var buf bytes.Buffer
	if w := httpx.ProgressWriter(&buf, -1, nil); w != io.Writer(&buf) {
		t.Fatalf("got %T", w)
	}
}

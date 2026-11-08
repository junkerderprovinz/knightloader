package hoster

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPeekNamesAndSizesAFileFromItsFirstByte(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 4096)
	var ranges []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ranges = append(ranges, r.Header.Get("Range"))
		if r.URL.Path == "/share" {
			http.Redirect(w, r, "/files/report.pdf", http.StatusFound)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="report.pdf"; filename*=UTF-8''Quartalsbericht%20M%C3%A4rz.pdf`)
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
	}))
	defer srv.Close()

	a, err := Peek(context.Background(), srv.Client(), srv.URL+"/share", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !a.File() || a.Name != "Quartalsbericht März.pdf" || a.Size != 4096 || a.URL.Path != "/files/report.pdf" {
		t.Errorf("Peek = %+v, want the file at the end of the redirect named from filename*", a)
	}
	if len(ranges) != 2 || ranges[1] != "bytes=0-0" {
		t.Errorf("asked with ranges %q, want only the first byte", ranges)
	}
}

func TestPeekTakesTheNameFromThePathWithoutADisposition(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-gzip")
		w.Write([]byte("whole body, no ranges"))
	}))
	defer srv.Close()

	a, err := Peek(context.Background(), srv.Client(), srv.URL+"/data/pretrained%20models.tar.gz?ga=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !a.File() || a.Name != "pretrained models.tar.gz" || a.Size != 21 {
		t.Errorf("Peek = %+v, want the path's file name and the full length", a)
	}
}

func TestPeekKeepsTheStartOfAPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<html><title>Virus scan warning</title></html>"))
	}))
	defer srv.Close()

	a, err := Peek(context.Background(), srv.Client(), srv.URL+"/download?id=1", http.Header{"Cookie": {"a=b"}})
	if err != nil {
		t.Fatal(err)
	}
	if a.File() || !strings.Contains(string(a.Page), "Virus scan warning") || a.Name != "" {
		t.Errorf("Peek = %+v, want the page and no file", a)
	}
}

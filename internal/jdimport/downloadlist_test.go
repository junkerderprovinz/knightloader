package jdimport

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// A list inside an upload is copied before it is read, and the copies share
// the budget with what the lists unpack to.
func TestCopiedDownloadListsUseUpTheBudget(t *testing.T) {
	var list bytes.Buffer
	zw := zip.NewWriter(&list)
	w, err := zw.Create("0")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte(`{"name":"pkg"}`))
	w, err = zw.CreateHeader(&zip.FileHeader{Name: "padding", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	w.Write(make([]byte, 3<<20))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	var upload bytes.Buffer
	uw := zip.NewWriter(&upload)
	w, err = uw.Create("cfg/downloadList.zip")
	if err != nil {
		t.Fatal(err)
	}
	w.Write(list.Bytes())
	if err := uw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(upload.Bytes()), int64(upload.Len()))
	if err != nil {
		t.Fatal(err)
	}
	fsys := ZipFS(zr)

	budget := int64(4 << 20)
	pkgs, err := readList(fsys, "cfg/downloadList.zip", &budget)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 || pkgs[0].Name != "pkg" {
		t.Errorf("packages = %+v, want pkg", pkgs)
	}
	if budget > 1<<20 {
		t.Errorf("budget left = %d, want the 3 MiB copy charged", budget)
	}
	if _, err := readList(fsys, "cfg/downloadList.zip", &budget); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Errorf("second copy err = %v, want it refused by the budget", err)
	}
}

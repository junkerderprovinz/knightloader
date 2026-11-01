package jre

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// adoptium stands in for Adoptium's API and its download, offering the archive
// as version with checksum sum. It returns the API's address.
func adoptium(t *testing.T, version, name string, body []byte, sum string) string {
	t.Helper()
	osName, arch, err := platform()
	if err != nil {
		t.Skip(err)
	}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/v3/assets/latest/21/hotspot", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("image_type") != "jre" || q.Get("os") != osName || q.Get("architecture") != arch || q.Get("vendor") != "eclipse" {
			http.Error(w, "unexpected question "+r.URL.RawQuery, http.StatusBadRequest)
			return
		}
		pkg := map[string]any{"name": name, "link": srv.URL + "/dl/" + name, "checksum": sum}
		json.NewEncoder(w).Encode([]any{map[string]any{
			"binary":       map[string]any{"image_type": "jre", "os": osName, "architecture": arch, "package": pkg},
			"release_name": "jdk-" + version,
		}})
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) { w.Write(body) })

	prev := smokeTest
	t.Cleanup(func() { smokeTest = prev })
	smokeTest = func(context.Context, string) error { return nil }
	return srv.URL
}

// inPlace writes a runtime of version into the data folder as Ensure would.
func inPlace(t *testing.T, data, version, java string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(Dir(data), "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Java(data), []byte(java), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeRecord(Dir(data), Record{Vendor: Vendor, Version: version}); err != nil {
		t.Fatal(err)
	}
}

func TestCompareOrdersRuntimeVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"21.0.13+9", "21.0.13+9", 0},
		{"21.0.14+7", "21.0.13+9", 1},
		{"21.0.13+11", "21.0.13+9", 1},
		{"21.0.9+10", "21.0.13+1", -1},
		{"21.0.4.1+1", "21.0.4+7", 1},
		{"22+36", "21.0.13+9", 1},
		{"jdk-21.0.13+9", "21.0.13+9-LTS", 0},
	} {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := Compare(c.b, c.a); got != -c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.b, c.a, got, -c.want)
		}
	}
}

func TestLatestAsksForThisPlatformsJRE(t *testing.T) {
	name, body := fakeRuntime(t)
	api := adoptium(t, "21.0.14+7", name, body, digest(body))
	rel, err := Latest(context.Background(), api)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "21.0.14+7" || rel.Name != name || rel.SHA256 != digest(body) || !strings.HasPrefix(rel.URL, api) {
		t.Errorf("Latest = %+v", rel)
	}
}

func TestStageKeepsAVerifiedRuntimeForTheNextStart(t *testing.T) {
	name, body := fakeRuntime(t)
	api := adoptium(t, "21.0.14+7", name, body, digest(body))
	data := t.TempDir()
	inPlace(t, data, Version, "old java")
	rel, err := Latest(context.Background(), api)
	if err != nil {
		t.Fatal(err)
	}
	if err := Stage(context.Background(), data, rel); err != nil {
		t.Fatal(err)
	}
	pending, err := Pending(data)
	if err != nil || pending == nil || pending.Version != "21.0.14+7" || pending.SHA256 != digest(body) {
		t.Fatalf("Pending = %+v, %v", pending, err)
	}
	if got := mustRead(t, Java(data)); got != "old java" {
		t.Errorf("java in use is %q, want it left alone until JD moves over", got)
	}
	if left, _ := filepath.Glob(filepath.Join(data, stagePrefix+"*")); len(left) != 0 {
		t.Errorf("left %v behind", left)
	}
}

func TestStageThrowsAwayADownloadThatFailsAdoptiumsChecksum(t *testing.T) {
	name, body := fakeRuntime(t)
	api := adoptium(t, "21.0.14+7", name, body, digest([]byte("another runtime")))
	data := t.TempDir()
	inPlace(t, data, Version, "old java")
	rel, err := Latest(context.Background(), api)
	if err != nil {
		t.Fatal(err)
	}
	if err := Stage(context.Background(), data, rel); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("Stage = %v, want a checksum refusal", err)
	}
	if rec, _ := Pending(data); rec != nil {
		t.Errorf("a runtime that failed its checksum waits as %+v", rec)
	}
	if left, _ := filepath.Glob(filepath.Join(data, stagePrefix+"*")); len(left) != 0 {
		t.Errorf("left %v behind", left)
	}
}

func stageNewer(t *testing.T, data string) {
	t.Helper()
	name, body := fakeRuntime(t)
	api := adoptium(t, "21.0.14+7", name, body, digest(body))
	rel, err := Latest(context.Background(), api)
	if err != nil {
		t.Fatal(err)
	}
	if err := Stage(context.Background(), data, rel); err != nil {
		t.Fatal(err)
	}
}

func TestPromoteThenCommitDropsTheOldRuntime(t *testing.T) {
	data := t.TempDir()
	inPlace(t, data, Version, "old java")
	stageNewer(t, data)
	sw, err := Promote(data)
	if err != nil || sw == nil {
		t.Fatalf("Promote = %v, %v", sw, err)
	}
	if sw.From.Version != Version || sw.To.Version != "21.0.14+7" {
		t.Errorf("switch from %s to %s", sw.From.Version, sw.To.Version)
	}
	if got := mustRead(t, Java(data)); got != "java" {
		t.Errorf("java in place is %q, want the new one", got)
	}
	if err := sw.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(data, prevDir)); !os.IsNotExist(err) {
		t.Errorf("the old runtime is still kept: %v", err)
	}
	if rec, _ := Load(data); rec == nil || rec.Version != "21.0.14+7" {
		t.Errorf("record in place = %+v", rec)
	}
	if sw, err := Promote(data); sw != nil || err != nil {
		t.Errorf("a second Promote = %+v, %v; want nothing left to do", sw, err)
	}
}

func TestRollbackPutsTheOldRuntimeBack(t *testing.T) {
	data := t.TempDir()
	inPlace(t, data, Version, "old java")
	stageNewer(t, data)
	sw, err := Promote(data)
	if err != nil || sw == nil {
		t.Fatalf("Promote = %v, %v", sw, err)
	}
	if err := sw.Rollback(); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, Java(data)); got != "old java" {
		t.Errorf("java in place is %q, want the old one back", got)
	}
	if rec, _ := Pending(data); rec != nil {
		t.Errorf("the runtime that failed still waits as %+v", rec)
	}
}

func TestPromoteRestoresARuntimeACrashLeftAside(t *testing.T) {
	data := t.TempDir()
	inPlace(t, data, Version, "old java")
	if err := os.Rename(Dir(data), filepath.Join(data, prevDir)); err != nil {
		t.Fatal(err)
	}
	if sw, err := Promote(data); sw != nil || err != nil {
		t.Fatalf("Promote = %+v, %v; want nothing to switch", sw, err)
	}
	if got := mustRead(t, Java(data)); got != "old java" {
		t.Errorf("java in place is %q, want the one left aside", got)
	}
}

func TestPendingDropsARuntimeNoNewerThanTheOneInUse(t *testing.T) {
	data := t.TempDir()
	inPlace(t, data, "21.0.15+3", "java from a newer pin")
	stageNewer(t, data)
	if rec, err := Pending(data); rec != nil || err != nil {
		t.Errorf("Pending = %+v, %v; want an older download dropped", rec, err)
	}
	if _, err := os.Stat(filepath.Join(data, nextDir)); !os.IsNotExist(err) {
		t.Errorf("the older download is still kept: %v", err)
	}
}

func TestEnsureKeepsANewerRuntimeTheWeeklyUpdateFetched(t *testing.T) {
	name, body := fakeRuntime(t)
	hits, _ := serve(t, name, body, digest(body))
	data := t.TempDir()
	inPlace(t, data, "21.0.14+7", "newer java")
	java, err := Ensure(context.Background(), data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, java); got != "newer java" || hits.Load() != 0 {
		t.Errorf("java is %q after %d downloads, want the newer one kept", got, hits.Load())
	}
}

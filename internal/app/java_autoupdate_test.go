package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/core"
	"github.com/junkerderprovinz/knightloader/internal/jre"
)

// putRuntime writes a runtime of version, whose java reads java, into dir as
// the jre package lays one out.
func putRuntime(t *testing.T, dir, version, java string) {
	t.Helper()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, filepath.Base(jre.Java(""))), []byte(java), 0o755); err != nil {
		t.Fatal(err)
	}
	rec, _ := json.Marshal(jre.Record{Vendor: jre.Vendor, Version: version})
	if err := os.WriteFile(filepath.Join(dir, "knightloader-runtime.json"), rec, 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeAdoptium stands in for Adoptium's API offering version, and for the
// fetch and the JD restart, which it counts. refuse makes JD fail to start on
// the new runtime.
type fakeAdoptium struct {
	asked, fetched, switched atomic.Int32
	refuse                   bool
}

func (f *fakeAdoptium) wire(t *testing.T, a *App, version string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.asked.Add(1)
		q := r.URL.Query()
		fmt.Fprintf(w, `[{"binary":{"image_type":"jre","os":%q,"architecture":%q,"package":{"name":"runtime.zip","link":"http://%s/runtime.zip","checksum":"%s"}},"release_name":"jdk-%s"}]`,
			q.Get("os"), q.Get("architecture"), r.Host, strings.Repeat("ab", 32), version)
	}))
	t.Cleanup(srv.Close)
	a.latestJava = func(ctx context.Context) (*jre.Release, error) { return jre.Latest(ctx, srv.URL) }
	a.stageJava = func(_ context.Context, rel *jre.Release) error {
		f.fetched.Add(1)
		putRuntime(t, filepath.Join(a.DataDir, "jre-next"), rel.Version, "new java")
		return nil
	}
	a.SwitchJava = func(context.Context) error {
		f.switched.Add(1)
		sw, err := jre.Promote(a.DataDir)
		if err != nil || sw == nil {
			return fmt.Errorf("nothing to switch to: %v", err)
		}
		if f.refuse {
			if err := sw.Rollback(); err != nil {
				return err
			}
			return fmt.Errorf("%w: JD did not answer", ErrJavaRefused)
		}
		return sw.Commit()
	}
}

// javaUpdateApp is an App with the component switch set and the pinned
// runtime in place.
func javaUpdateApp(t *testing.T, on bool) *App {
	t.Helper()
	a := jdAutoUpdateApp(t, on)
	putRuntime(t, jre.Dir(a.DataDir), jre.Version, "old java")
	return a
}

func javaInUse(t *testing.T, a *App) string {
	t.Helper()
	b, err := os.ReadFile(jre.Java(a.DataDir))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTheJavaUpdateMovesJDOverOnceNoJDDownloadRuns(t *testing.T) {
	a := javaUpdateApp(t, true)
	f := &fakeAdoptium{}
	f.wire(t, a, "21.0.99+1")
	putTask(t, a, core.Task{ID: "jd", Resolver: "jd", Status: core.StatusRunning, Enabled: true})

	if r := a.updateJavaComponent(a.ctx); r.State != ComponentWaits {
		t.Error("the move was not held back while a JD download runs")
	}
	if f.fetched.Load() != 1 || f.switched.Load() != 0 {
		t.Fatalf("fetched %d times and switched %d times, want the fetch only", f.fetched.Load(), f.switched.Load())
	}
	if got := a.JDStatus().RuntimeUpdate; got != "21.0.99+1" {
		t.Errorf("RuntimeUpdate = %q while the move waits", got)
	}

	setStatus(a, "jd", core.StatusDone)
	if r := a.updateJavaComponent(a.ctx); r.State == ComponentWaits {
		t.Error("the move was held back with no JD download running")
	}
	if f.asked.Load() != 1 || f.fetched.Load() != 1 || f.switched.Load() != 1 {
		t.Fatalf("asked %d, fetched %d, switched %d times; want the waiting runtime moved in without asking again", f.asked.Load(), f.fetched.Load(), f.switched.Load())
	}
	if got := javaInUse(t, a); got != "new java" {
		t.Errorf("java in use is %q, want the new one", got)
	}
	if rec, _ := jre.Load(a.DataDir); rec == nil || rec.Version != "21.0.99+1" {
		t.Errorf("record in use = %+v", rec)
	}
	if got := a.JDStatus().RuntimeUpdate; got != "" {
		t.Errorf("RuntimeUpdate = %q after the move", got)
	}
}

func TestTheComponentSwitchOffLeavesAdoptiumUnasked(t *testing.T) {
	a := javaUpdateApp(t, false)
	f := &fakeAdoptium{}
	f.wire(t, a, "21.0.99+1")

	a.autoUpdateJava()

	if f.asked.Load() != 0 || f.fetched.Load() != 0 || f.switched.Load() != 0 {
		t.Fatalf("switched off: asked %d, fetched %d, switched %d times", f.asked.Load(), f.fetched.Load(), f.switched.Load())
	}
}

func TestTheJavaUpdateIgnoresAnOlderOrEqualRuntime(t *testing.T) {
	for _, v := range []string{jre.Version, "21.0.1+12"} {
		a := javaUpdateApp(t, true)
		f := &fakeAdoptium{}
		f.wire(t, a, v)

		a.updateJavaComponent(a.ctx)

		if f.asked.Load() != 1 || f.fetched.Load() != 0 || f.switched.Load() != 0 {
			t.Errorf("offered %s: asked %d, fetched %d, switched %d times; want the question only", v, f.asked.Load(), f.fetched.Load(), f.switched.Load())
		}
		if got := a.JDStatus().RuntimeUpdate; got != "" {
			t.Errorf("offered %s: RuntimeUpdate = %q", v, got)
		}
	}
}

func TestARuntimeJDDoesNotStartOnIsDroppedAndNotFetchedAgain(t *testing.T) {
	a := javaUpdateApp(t, true)
	f := &fakeAdoptium{refuse: true}
	f.wire(t, a, "21.0.99+1")

	if r := a.updateJavaComponent(a.ctx); r.State == ComponentWaits {
		t.Error("a refused runtime asked to come back early")
	}
	if got := javaInUse(t, a); got != "old java" {
		t.Errorf("java in use is %q, want the old one back", got)
	}
	if rec, _ := jre.Pending(a.DataDir); rec != nil {
		t.Errorf("the refused runtime still waits as %+v", rec)
	}

	a.updateJavaComponent(a.ctx)
	if f.fetched.Load() != 1 || f.switched.Load() != 1 {
		t.Errorf("fetched %d and switched %d times, want the refused runtime left alone the second time", f.fetched.Load(), f.switched.Load())
	}
}

func TestASwitchThatCouldNotStartIsTriedAgainSoon(t *testing.T) {
	a := javaUpdateApp(t, true)
	f := &fakeAdoptium{}
	f.wire(t, a, "21.0.99+1")
	a.SwitchJava = func(context.Context) error { return errors.New("JDownloader is already being set up") }

	if r := a.updateJavaComponent(a.ctx); r.State != ComponentWaits {
		t.Error("a switch that could not start yet is not tried again within the hour")
	}
	if rec, _ := jre.Pending(a.DataDir); rec == nil {
		t.Error("the fetched runtime no longer waits")
	}
}

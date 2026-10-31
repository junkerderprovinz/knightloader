package main

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/provision"
)

func testJDSetup(t *testing.T) (*jdSetup, context.CancelFunc) {
	t.Helper()
	t.Setenv("KL_JD", "")
	dir := t.TempDir()
	a, err := app.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	prev := ensureJava
	t.Cleanup(func() { ensureJava = prev })
	return &jdSetup{a: a, pv: provision.New(filepath.Join(dir, "jd")), dataDir: dir, ctx: ctx}, cancel
}

func TestJDSetupSaysWhyTheRuntimeDidNotArriveAndTriesAgain(t *testing.T) {
	s, _ := testJDSetup(t)
	ensureJava = func(context.Context, string, func(int64, int64)) (string, error) {
		return "", errors.New("downloading the Java runtime: no network")
	}
	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	s.wait()
	st := s.a.JDStatus()
	if st.Setup != app.JDSetupFailed || st.Detail != "downloading the Java runtime: no network" {
		t.Fatalf("status after a failed download = %+v", st)
	}

	block := make(chan struct{})
	ensureJava = func(ctx context.Context, _ string, progress func(int64, int64)) (string, error) {
		progress(10, 40)
		<-block
		return "", errors.New("the test is over")
	}
	if err := s.start(); err != nil {
		t.Fatalf("retry: %v", err)
	}
	t.Cleanup(func() { close(block); s.wait() })
	if err := s.start(); !errors.Is(err, errJDSettingUp) {
		t.Errorf("a second retry during the setup = %v, want errJDSettingUp", err)
	}
	for st = s.a.JDStatus(); st.SetupDone == 0; st = s.a.JDStatus() {
	}
	if st.Setup != app.JDSetupStarting || st.SetupDone != 10 || st.SetupTotal != 40 {
		t.Errorf("status during the retry = %+v, want starting at 10 of 40", st)
	}
}

func TestJDSetupRunsJDOnTheFetchedRuntime(t *testing.T) {
	s, cancel := testJDSetup(t)
	if err := os.MkdirAll(s.pv.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	jar, err := os.Create(filepath.Join(s.pv.Dir, "JDownloader.jar"))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(jar)
	zw.Create("META-INF/MANIFEST.MF")
	zw.Close()
	jar.Close()
	fetched := filepath.Join(s.dataDir, "jre", "bin", "java")
	// The program ends while JD is being set up, after the runtime arrived.
	ensureJava = func(context.Context, string, func(int64, int64)) (string, error) {
		cancel()
		return fetched, nil
	}

	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	s.wait()
	if s.pv.Java != fetched {
		t.Errorf("JD was handed %q, want the fetched runtime %q", s.pv.Java, fetched)
	}
}

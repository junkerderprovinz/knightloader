package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/jre"
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

// putRuntime writes a runtime of version, whose java reads java, into dir as
// the jre package lays one out.
func putRuntime(t *testing.T, dir, version, java string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", filepath.Base(jre.Java(""))), []byte(java), 0o755); err != nil {
		t.Fatal(err)
	}
	rec, _ := json.Marshal(jre.Record{Vendor: jre.Vendor, Version: version})
	if err := os.WriteFile(filepath.Join(dir, "knightloader-runtime.json"), rec, 0o644); err != nil {
		t.Fatal(err)
	}
}

// javaSwitchSetup is a setup with the pinned runtime in place and a newer one
// waiting, whose JD starts unless the java in place reads refused.
func javaSwitchSetup(t *testing.T, refused string) (*jdSetup, *[]string) {
	t.Helper()
	s, _ := testJDSetup(t)
	putRuntime(t, jre.Dir(s.dataDir), jre.Version, "old java")
	putRuntime(t, filepath.Join(s.dataDir, "jre-next"), "21.0.99+1", "new java")
	prev := runJD
	t.Cleanup(func() { runJD = prev })
	var ranOn []string
	runJD = func(context.Context, *provision.Provisioner) (string, error) {
		b, _ := os.ReadFile(jre.Java(s.dataDir))
		ranOn = append(ranOn, string(b))
		if string(b) == refused {
			return "", errors.New("JD did not answer")
		}
		return "http://127.0.0.1:3128", nil
	}
	return s, &ranOn
}

func TestJDMovesOntoTheJavaRuntimeTheWeeklyUpdateFetched(t *testing.T) {
	s, ranOn := javaSwitchSetup(t, "")
	if err := s.switchJava(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*ranOn, []string{"new java"}) {
		t.Errorf("JD ran on %q, want the new runtime", *ranOn)
	}
	if rec, _ := jre.Load(s.dataDir); rec == nil || rec.Version != "21.0.99+1" {
		t.Errorf("record in place = %+v", rec)
	}
	if _, err := os.Stat(filepath.Join(s.dataDir, "jre-prev")); !os.IsNotExist(err) {
		t.Errorf("the old runtime is still kept: %v", err)
	}
}

func TestJDGoesBackToTheOldJavaWhenItDoesNotStartOnTheNewOne(t *testing.T) {
	s, ranOn := javaSwitchSetup(t, "new java")
	err := s.switchJava(context.Background())
	if !errors.Is(err, app.ErrJavaRefused) {
		t.Fatalf("switchJava = %v, want app.ErrJavaRefused", err)
	}
	if !slices.Equal(*ranOn, []string{"new java", "old java"}) {
		t.Errorf("JD ran on %q, want the new runtime, then the old one again", *ranOn)
	}
	if rec, _ := jre.Load(s.dataDir); rec == nil || rec.Version != jre.Version {
		t.Errorf("record in place = %+v, want the old runtime's", rec)
	}
	if rec, _ := jre.Pending(s.dataDir); rec != nil {
		t.Errorf("the runtime JD did not start on still waits as %+v", rec)
	}
	if os.Getenv("KL_JD") == "" {
		t.Error("the JD running on the old runtime again was not taken into use")
	}
}

func TestASetupStartsJDOnAJavaRuntimeThatWaits(t *testing.T) {
	s, ranOn := javaSwitchSetup(t, "")
	ensureJava = func(_ context.Context, dir string, _ func(int64, int64)) (string, error) { return jre.Java(dir), nil }
	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	s.wait()
	if !slices.Equal(*ranOn, []string{"new java"}) {
		t.Errorf("JD ran on %q, want the runtime that waited", *ranOn)
	}
}

package update

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

// The test binary doubles as a running program: started with this variable
// set, it only waits to be killed.
const idleEnv = "UPDATE_TEST_IDLE"

func TestMain(m *testing.M) {
	if os.Getenv(idleEnv) != "" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// startCopy runs a copy of the test binary from dir. It returns the copy's
// path and a function that stops it.
func startCopy(t *testing.T, dir string) (string, func()) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	prog := filepath.Join(dir, "prog.exe")
	src, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.Create(prog)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	dst.Close()

	cmd := exec.Command(prog)
	cmd.Env = append(os.Environ(), idleEnv+"=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stop := func() {
		cmd.Process.Kill()
		cmd.Wait()
	}
	t.Cleanup(stop)
	return prog, stop
}

func writeDownload(t *testing.T, prog, body string) string {
	t.Helper()
	f, err := os.CreateTemp(filepath.Dir(prog), downloadPrefix(prog)+"*")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(body)
	f.Close()
	return f.Name()
}

func TestReplaceFileMovesTheRunningProgramAside(t *testing.T) {
	dir := t.TempDir()
	prog, _ := startCopy(t, dir)

	if err := os.Remove(prog); err == nil {
		t.Fatal("deleted a running program, so this test proves nothing")
	}
	if err := replaceFile(prog, writeDownload(t, prog, "second")); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, prog); got != "second" {
		t.Errorf("program holds %q", got)
	}
	if _, err := os.Stat(prog + ".old"); err != nil {
		t.Errorf("the running program is not at .old: %v", err)
	}

	// A second update in the same run finds the running program at .old and
	// replaces the staged file directly.
	if err := replaceFile(prog, writeDownload(t, prog, "third")); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, prog); got != "third" {
		t.Errorf("after the second update the program holds %q", got)
	}

	u := &Updater{Path: prog, Logf: t.Logf}
	u.Cleanup()
	if _, err := os.Stat(prog + ".old"); err != nil {
		t.Error("cleanup removed the .old of a program that still runs")
	}
}

func TestCleanupRemovesTheOldProgramOnceItHasExited(t *testing.T) {
	dir := t.TempDir()
	prog, stop := startCopy(t, dir)
	if err := replaceFile(prog, writeDownload(t, prog, "new")); err != nil {
		t.Fatal(err)
	}
	stop()

	(&Updater{Path: prog, Logf: t.Logf}).Cleanup()
	if got := dirEntries(t, dir); len(got) != 1 || got[0] != "prog.exe" {
		t.Errorf("after cleanup the folder holds %v", got)
	}
	if got := readFile(t, prog); got != "new" {
		t.Errorf("program holds %q", got)
	}
}

func TestRecordVersionUpdatesOnlyTheInstalledCopy(t *testing.T) {
	name := "update-test-" + strings.ReplaceAll(t.Name(), "/", "-") + time.Now().Format("150405.000")
	k, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallRoot+name, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		k.Close()
		registry.DeleteKey(registry.CURRENT_USER, uninstallRoot+name)
	})
	exe := `C:\Users\someone\AppData\Local\Programs\Prog\Prog.exe`
	k.SetStringValue("DisplayIcon", exe)
	k.SetStringValue("DisplayVersion", "1.3.0")

	if err := recordVersion(name, `D:\portable\Prog.exe`, "1.4.0"); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := k.GetStringValue("DisplayVersion"); v != "1.3.0" {
		t.Errorf("a portable copy changed the entry to %q", v)
	}
	if err := recordVersion(name, strings.ToLower(exe), "1.4.0"); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := k.GetStringValue("DisplayVersion"); v != "1.4.0" {
		t.Errorf("DisplayVersion is %q, want 1.4.0", v)
	}
	if err := recordVersion(name+"-missing", exe, "1.4.0"); err != nil {
		t.Errorf("a missing entry: %v", err)
	}
}

package provision

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteAPIConfig(t *testing.T) {
	dir := t.TempDir()
	p := New(dir)
	p.Port = 3128
	if err := p.WriteAPIConfig(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "cfg", "org.jdownloader.api.RemoteAPIConfig.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["deprecatedapienabled"] != true {
		t.Errorf("deprecatedapienabled = %v, want true", cfg["deprecatedapienabled"])
	}
	if cfg["deprecatedapiport"].(float64) != 3128 {
		t.Errorf("deprecatedapiport = %v, want 3128", cfg["deprecatedapiport"])
	}
	if cfg["deprecatedapilocalhostonly"] != true {
		t.Errorf("deprecatedapilocalhostonly = %v, want true", cfg["deprecatedapilocalhostonly"])
	}
}

func TestFindJavaFromJavaHome(t *testing.T) {
	// Build a fake JAVA_HOME with a bin/java(.exe) file and confirm it's found.
	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "java"
	if runtime.GOOS == "windows" {
		name = "java.exe"
	}
	javaPath := filepath.Join(bin, name)
	if err := os.WriteFile(javaPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JAVA_HOME", home)

	got, err := FindJava()
	if err != nil {
		t.Fatal(err)
	}
	if got != javaPath {
		t.Fatalf("FindJava = %q, want %q", got, javaPath)
	}
}

func TestProvisionedFalseOnEmptyDir(t *testing.T) {
	p := New(t.TempDir())
	if p.Provisioned() {
		t.Error("Provisioned() = true on an empty dir")
	}
	if p.URL() != "http://127.0.0.1:3128" {
		t.Errorf("URL() = %q", p.URL())
	}
}

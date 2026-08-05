// Package provision sets up a private, headless JDownloader for the desktop
// build so the user gets full hoster coverage out of the box without ever
// seeing JD's own UI. It downloads JDownloader.jar on first run, enables the
// local Deprecated API KnightLoader talks to, and launches it in the
// background. Nothing here is bundled in the repo — JD is fetched from its
// official source at runtime.
package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// jarURL is JDownloader's official self-updating launcher jar.
const jarURL = "http://installer.jdownloader.org/JDownloader.jar"

// Provisioner owns a private JD home directory and the local API port.
type Provisioner struct {
	Dir  string // JD home: the jar and its cfg/ live here
	Port int    // Deprecated API port (127.0.0.1 only)
}

// New returns a provisioner rooted at dir (default port 3128).
func New(dir string) *Provisioner {
	return &Provisioner{Dir: dir, Port: 3128}
}

// URL is the KL_JD base for the provisioned instance.
func (p *Provisioner) URL() string { return fmt.Sprintf("http://127.0.0.1:%d", p.Port) }

func (p *Provisioner) jarPath() string { return filepath.Join(p.Dir, "JDownloader.jar") }

// Provisioned reports whether the jar is already in place.
func (p *Provisioner) Provisioned() bool {
	fi, err := os.Stat(p.jarPath())
	return err == nil && fi.Size() > 0
}

// EnsureJar downloads JDownloader.jar into Dir if it is not there yet.
func (p *Provisioner) EnsureJar(ctx context.Context) error {
	if p.Provisioned() {
		return nil
	}
	if err := os.MkdirAll(p.Dir, 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jarURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("provision: download JD: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("provision: download JD: HTTP %d", resp.StatusCode)
	}
	tmp := p.jarPath() + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, p.jarPath())
}

// WriteAPIConfig enables the Deprecated API (plain-HTTP local JSON) that
// KnightLoader uses, writing JD's config file so it is picked up on boot.
func (p *Provisioner) WriteAPIConfig() error {
	cfgDir := filepath.Join(p.Dir, "cfg")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		return err
	}
	cfg := map[string]any{
		"deprecatedapienabled":       true,
		"deprecatedapiport":          p.Port,
		"deprecatedapilocalhostonly": true, // desktop: same machine only
		"jdanywhereapienabled":       false,
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(cfgDir, "org.jdownloader.api.RemoteAPIConfig.json")
	return os.WriteFile(path, b, 0o644)
}

// FindJava returns a runnable java, preferring JAVA_HOME then PATH. The desktop
// installer bundles a JRE; this also lets a system Java be used.
func FindJava() (string, error) {
	bin := "java"
	if runtime.GOOS == "windows" {
		bin = "java.exe"
	}
	if home := os.Getenv("JAVA_HOME"); home != "" {
		cand := filepath.Join(home, "bin", bin)
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			return cand, nil
		}
	}
	if path, err := exec.LookPath(bin); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("provision: no Java runtime found (set JAVA_HOME or bundle a JRE)")
}

// Start launches headless JD in the background using the given java binary.
func (p *Provisioner) Start(java string) (*exec.Cmd, error) {
	cmd := exec.Command(java, "-jar", p.jarPath())
	cmd.Dir = p.Dir
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("provision: start JD: %w", err)
	}
	return cmd, nil
}

// WaitReachable polls the Deprecated API until it answers or the context ends.
// JD self-updates on first run, so allow a generous deadline.
func (p *Provisioner) WaitReachable(ctx context.Context) error {
	ping := p.URL() + "/device/ping"
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ping, nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Ensure runs the full first-run sequence: download the jar (once), write the
// API config, launch JD, and wait for it to answer. Returns the running command
// and the KL_JD URL. The caller sets KL_JD (or the app's jd backend) to URL().
func (p *Provisioner) Ensure(ctx context.Context) (*exec.Cmd, string, error) {
	if err := p.EnsureJar(ctx); err != nil {
		return nil, "", err
	}
	if err := p.WriteAPIConfig(); err != nil {
		return nil, "", err
	}
	java, err := FindJava()
	if err != nil {
		return nil, "", err
	}
	cmd, err := p.Start(java)
	if err != nil {
		return nil, "", err
	}
	if err := p.WaitReachable(ctx); err != nil {
		return cmd, "", err
	}
	return cmd, p.URL(), nil
}

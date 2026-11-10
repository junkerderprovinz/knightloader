package flatpak

import (
	"os"
	"path/filepath"
	"strings"
)

// NVIDIARenders reports whether WebKit renders on NVIDIA's proprietary driver,
// whose DMA-BUF import fails with Wayland's error 71. It finds the driver
// under /sys/bus, since Flatpak does not mount /sys/module where Wails looks,
// and picks the rendering card the way Wails does.
func NVIDIARenders(sys string, getenv func(string) string) bool {
	if _, err := os.Stat(filepath.Join(sys, "bus", "pci", "drivers", "nvidia")); err != nil {
		return false
	}
	if getenv("__NV_PRIME_RENDER_OFFLOAD") == "1" {
		return true
	}
	// Under X11 the server's configuration decides, which is not readable here.
	if getenv("XDG_SESSION_TYPE") != "wayland" && getenv("WAYLAND_DISPLAY") == "" {
		return true
	}
	drm := filepath.Join(sys, "class", "drm")
	driverOf := func(card string) string {
		link, err := os.Readlink(filepath.Join(drm, card, "device", "driver"))
		if err != nil {
			return ""
		}
		return filepath.Base(link)
	}
	var screens []string
	statuses, _ := filepath.Glob(filepath.Join(drm, "card*-*", "status"))
	for _, status := range statuses {
		if b, err := os.ReadFile(status); err == nil && strings.TrimSpace(string(b)) == "connected" {
			card, _, _ := strings.Cut(filepath.Base(filepath.Dir(status)), "-")
			screens = append(screens, card)
		}
	}
	for _, card := range screens {
		if b, err := os.ReadFile(filepath.Join(drm, card, "device", "boot_vga")); err == nil && strings.TrimSpace(string(b)) == "1" {
			return driverOf(card) == "nvidia"
		}
	}
	for _, card := range screens {
		if driver := driverOf(card); driver != "" && driver != "nvidia" {
			return false
		}
	}
	return true
}

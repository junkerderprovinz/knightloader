package flatpak

import (
	"os"
	"path/filepath"
	"testing"
)

type card struct {
	name, driver string
	bootVGA      bool
	screen       string
}

// fakeSys lays out the part of /sys that Flatpak mounts: the PCI drivers and
// the DRM cards, each linked to its driver and with its connectors.
func fakeSys(t *testing.T, drivers []string, cards ...card) string {
	t.Helper()
	sys := t.TempDir()
	for _, d := range drivers {
		if err := os.MkdirAll(filepath.Join(sys, "bus", "pci", "drivers", d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range cards {
		dev := filepath.Join(sys, "class", "drm", c.name, "device")
		if err := os.MkdirAll(dev, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join("..", "..", "..", "..", "bus", "pci", "drivers", c.driver), filepath.Join(dev, "driver")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		vga := "0"
		if c.bootVGA {
			vga = "1"
		}
		if err := os.WriteFile(filepath.Join(dev, "boot_vga"), []byte(vga+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		connector := filepath.Join(sys, "class", "drm", c.name+"-DP-1")
		if err := os.MkdirAll(connector, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(connector, "status"), []byte(c.screen+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return sys
}

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

var onWayland = env(map[string]string{"WAYLAND_DISPLAY": "wayland-0", "XDG_SESSION_TYPE": "wayland"})

func TestAnNVIDIACardDrivingTheScreenRenders(t *testing.T) {
	sys := fakeSys(t, []string{"nvidia"}, card{"card1", "nvidia", true, "connected"})
	if !NVIDIARenders(sys, onWayland) {
		t.Error("an NVIDIA card driving the only screen was not taken to render")
	}
}

func TestAnIntelBootScreenRendersBesideAnNVIDIACard(t *testing.T) {
	sys := fakeSys(t, []string{"i915", "nvidia"},
		card{"card0", "i915", true, "connected"},
		card{"card1", "nvidia", false, "disconnected"})
	if NVIDIARenders(sys, onWayland) {
		t.Error("an NVIDIA card next to the Intel card driving the screen was taken to render")
	}
}

func TestWithoutTheNVIDIADriverNothingRendersOnIt(t *testing.T) {
	sys := fakeSys(t, []string{"amdgpu"}, card{"card0", "amdgpu", true, "connected"})
	if NVIDIARenders(sys, onWayland) {
		t.Error("an AMD card was taken for NVIDIA")
	}
}

func TestAnX11SessionWithTheNVIDIADriverRendersOnIt(t *testing.T) {
	sys := fakeSys(t, []string{"i915", "nvidia"},
		card{"card0", "i915", true, "connected"},
		card{"card1", "nvidia", false, "disconnected"})
	if !NVIDIARenders(sys, env(map[string]string{"XDG_SESSION_TYPE": "x11"})) {
		t.Error("an X11 session with the NVIDIA driver loaded was not taken to render on it")
	}
}

func TestAnOffloadedAppRendersOnNVIDIA(t *testing.T) {
	sys := fakeSys(t, []string{"i915", "nvidia"},
		card{"card0", "i915", true, "connected"},
		card{"card1", "nvidia", false, "disconnected"})
	offload := env(map[string]string{"WAYLAND_DISPLAY": "wayland-0", "__NV_PRIME_RENDER_OFFLOAD": "1"})
	if !NVIDIARenders(sys, offload) {
		t.Error("an app offloaded to the NVIDIA card was not taken to render on it")
	}
}

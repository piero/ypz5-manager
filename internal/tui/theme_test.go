package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/piero/ypz5-manager/fat32"
)

// TestMain disables colors so tests can match plain text; tests that
// check colors enable them explicitly.
func TestMain(m *testing.M) {
	colorEnabled = false

	os.Exit(m.Run())
}

func withColors(t *testing.T) {
	t.Helper()

	colorEnabled = true

	t.Cleanup(func() { colorEnabled = false })
}

// TestPalettesShowWhichSideIsActive verifies that device screens use the
// YP-Z5 palette and local screens the Mac palette.
func TestPalettesShowWhichSideIsActive(t *testing.T) {
	withColors(t)

	m := model{
		path:    "/Music",
		dest:    "/Music",
		entries: []fat32.DirectoryEntry{{Name: "Album", IsDir: true}},
	}

	deviceBar := devicePalette.bar.render(devicePalette.name)
	localBar := localPalette.bar.render(localPalette.name)

	device := m.browserView()

	if !strings.Contains(device, deviceBar) || strings.Contains(device, localBar) {
		t.Fatalf("device browser does not use the YP-Z5 palette:\n%q", device)
	}

	m.local = &localBrowser{
		path:    "/Users/me",
		entries: []localEntry{{name: "Album", isDir: true}},
	}

	local := m.localView()

	if !strings.Contains(local, localBar) || strings.Contains(local, deviceBar) {
		t.Fatalf("local browser does not use the Mac palette:\n%q", local)
	}

	// The destination on the local screen is a device path, so it is
	// labelled in the device color.
	if !strings.Contains(local, devicePalette.accent.render("Copy to (YP-Z5):")) {
		t.Fatalf("destination not in the YP-Z5 color:\n%q", local)
	}

	m.picker = &destPicker{path: "/Music"}

	if picker := m.pickerView(); !strings.Contains(picker, deviceBar) {
		t.Fatalf("destination picker does not use the YP-Z5 palette:\n%q", picker)
	}
}

func TestNoColor(t *testing.T) {
	m := model{path: "/", entries: []fat32.DirectoryEntry{{Name: "Music", IsDir: true}}}

	if view := m.browserView(); strings.Contains(view, "\x1b[") {
		t.Fatalf("escape codes with colors disabled:\n%q", view)
	}
}

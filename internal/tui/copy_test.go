package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/piero/ypz5/internal/testimage"
	"github.com/piero/ypz5/internal/transfer"
)

func TestCopyViews(t *testing.T) {
	m := model{
		path:         "/",
		dest:         "/Music",
		local:        &localBrowser{path: "/Users/me"},
		localConfirm: true,
		copyPlan: transfer.Plan{
			Files:   3,
			Bytes:   6 * 1024 * 1024,
			Skipped: []string{"/Users/me/Album/.DS_Store (macOS metadata)"},
		},
	}

	confirm := m.localView()

	for _, want := range []string{"Copy 3 files (6.0 MiB) to /Music?", "1 item will be skipped"} {
		if !strings.Contains(confirm, want) {
			t.Fatalf("confirmation view missing %q:\n%s", want, confirm)
		}
	}

	m.copy = &copyJob{
		plan:    m.copyPlan,
		dest:    "/Music",
		done:    true,
		copied:  2,
		written: 4 * 1024 * 1024,
		skipped: m.copyPlan.Skipped,
		errors:  []string{"/Music/Album/bad:name.mp3: invalid character"},
	}

	summary := m.copyView()

	for _, want := range []string{"Copy finished with errors.", "Copied:  2 files (4.0 MiB)", "Skipped (1):", "Errors (1):"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary view missing %q:\n%s", want, summary)
		}
	}
}

func runCmds(t *testing.T, next tea.Model, cmd tea.Cmd) tea.Model {
	t.Helper()

	for cmd != nil {
		next, cmd = next.Update(cmd())
	}

	return next
}

func TestCopyToChosenDestination(t *testing.T) {
	fs := testimage.Open(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	testimage.WriteTree(t, home, map[string]string{
		"Album/01 - Track.mp3": "track",
		"Album/.DS_Store":      "junk",
	})

	entries, err := fs.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}

	var next tea.Model = model{fs: fs, path: "/", entries: entries, writeEnabled: true}

	press := func(code rune) {
		t.Helper()

		var cmd tea.Cmd
		next, cmd = next.Update(tea.KeyPressMsg{Code: code, Text: string(code)})
		next = runCmds(t, next, cmd)
	}

	pressKey := func(code rune) {
		t.Helper()

		var cmd tea.Cmd
		next, cmd = next.Update(tea.KeyPressMsg{Code: code})
		next = runCmds(t, next, cmd)
	}

	press('n')

	if got := next.(model).dest; got != "/Music" {
		t.Fatalf("default destination = %q, want /Music", got)
	}

	// Open the picker, go up to the root and into Recordings.
	press('t')

	if got := next.(model).picker.path; got != "/Music" {
		t.Fatalf("picker starts at %q, want /Music", got)
	}

	pressKey(tea.KeyBackspace)

	picker := next.(model).picker
	if picker.path != "/" {
		t.Fatalf("picker path after backspace = %q, want /", picker.path)
	}

	found := false

	for i, entry := range picker.entries {
		if !entry.IsDir {
			t.Fatalf("picker lists file %q", entry.Name)
		}

		if entry.Name == "Recordings" {
			for range i {
				pressKey(tea.KeyDown)
			}

			found = true

			break
		}
	}

	if !found {
		t.Fatal("Recordings not listed in picker")
	}

	pressKey(tea.KeyEnter)
	press('s')

	m := next.(model)
	if m.picker != nil || m.dest != "/Recordings" {
		t.Fatalf("after choosing: picker open = %v, dest = %q", m.picker != nil, m.dest)
	}

	if view := m.localView(); !strings.Contains(view, "Copy to (YP-Z5): /Recordings") {
		t.Fatalf("local view does not show the destination:\n%s", view)
	}

	// Select the album (the only local entry), copy and confirm.
	pressKey(tea.KeySpace)
	press('c')
	press('y')

	m = next.(model)
	if m.copy == nil || !m.copy.done {
		t.Fatal("copy did not finish")
	}

	if len(m.copy.errors) > 0 {
		t.Fatalf("copy errors: %v", m.copy.errors)
	}

	got, err := fs.ReadFile("/Recordings/Album/01 - Track.mp3")
	if err != nil {
		t.Fatalf("copied file not in chosen destination: %v", err)
	}

	if string(got) != "track" {
		t.Fatalf("copied contents = %q", got)
	}

	if _, err := fs.Lookup("/Music/Album"); err == nil {
		t.Fatal("album was also copied into /Music")
	}
}

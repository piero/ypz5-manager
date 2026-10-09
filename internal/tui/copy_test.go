package tui

import (
	"path"
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

// runCmds executes tea commands synchronously until none is left, the way
// the program would run them, and feeds each message back to the model.
func runCmds(t *testing.T, next tea.Model, cmd tea.Cmd) tea.Model {
	t.Helper()

	for cmd != nil {
		next, cmd = next.Update(cmd())
	}

	return next
}

// TestCopyToChosenDestination drives the TUI: the destination defaults to
// /Music (or / without one), is changed with the picker, and the copy
// lands there regardless of the folder open in the device browser. It
// creates its own target folder, so it works with any image.
func TestCopyToChosenDestination(t *testing.T) {
	fs := testimage.Open(t)

	const target = "/YPZ5 Picker/Target"

	for _, dir := range []string{"/YPZ5 Picker", target} {
		if err := fs.Mkdir(dir); err != nil {
			t.Fatal(err)
		}
	}

	wantDefault := "/"

	if music, err := fs.Find("/Music"); err == nil {
		wantDefault = "/" + music.Name
	}

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

	// openFolder moves the picker's cursor to name and opens it.
	openFolder := func(name string) {
		t.Helper()

		for i, entry := range next.(model).picker.entries {
			if !entry.IsDir {
				t.Fatalf("picker lists file %q", entry.Name)
			}

			if entry.Name == name {
				for range i {
					pressKey(tea.KeyDown)
				}

				pressKey(tea.KeyEnter)

				return
			}
		}

		t.Fatalf("%q not listed in picker at %s", name, next.(model).picker.path)
	}

	press('n')

	if got := next.(model).dest; got != wantDefault {
		t.Fatalf("default destination = %q, want %q", got, wantDefault)
	}

	press('t')

	if got := next.(model).picker.path; got != wantDefault {
		t.Fatalf("picker starts at %q, want %q", got, wantDefault)
	}

	// Go up to the root, then down into the target.
	for range 10 {
		if next.(model).picker.path == "/" {
			break
		}

		pressKey(tea.KeyBackspace)
	}

	if got := next.(model).picker.path; got != "/" {
		t.Fatalf("picker path after going up = %q, want /", got)
	}

	openFolder("YPZ5 Picker")
	openFolder("Target")
	press('s')

	m := next.(model)
	if m.picker != nil || m.dest != target {
		t.Fatalf("after choosing: picker open = %v, dest = %q", m.picker != nil, m.dest)
	}

	if view := m.localView(); !strings.Contains(view, "Copy to (YP-Z5): "+target) {
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

	got, err := fs.ReadFile(target + "/Album/01 - Track.mp3")
	if err != nil {
		t.Fatalf("copied file not in chosen destination: %v", err)
	}

	if string(got) != "track" {
		t.Fatalf("copied contents = %q", got)
	}

	if _, err := fs.Lookup(path.Join(wantDefault, "Album")); err == nil {
		t.Fatal("album was also copied into the default destination")
	}
}

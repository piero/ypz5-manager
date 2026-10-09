package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/piero/ypz5/fat32"
)

// writeTree creates files (path → contents) under root.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for name, contents := range files {
		path := filepath.Join(root, name)

		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPlanCopySkipsMacOSMetadata(t *testing.T) {
	root := t.TempDir()

	writeTree(t, root, map[string]string{
		"Album/01 - Intro.mp3":   "intro",
		"Album/02 - Song.mp3":    "song!",
		"Album/.DS_Store":        "junk",
		"Album/._01 - Intro.mp3": "junk",
		"Single.mp3":             "single",
	})

	plan, err := planCopy(
		[]string{filepath.Join(root, "Album"), filepath.Join(root, "Single.mp3")},
		"/Music",
		4096,
	)
	if err != nil {
		t.Fatal(err)
	}

	var dests []string

	for _, op := range plan.ops {
		dests = append(dests, op.dest)
	}

	want := []string{
		"/Music/Album",
		"/Music/Album/01 - Intro.mp3",
		"/Music/Album/02 - Song.mp3",
		"/Music/Single.mp3",
	}

	if strings.Join(dests, "\n") != strings.Join(want, "\n") {
		t.Fatalf("ops:\n%s\nwant:\n%s", strings.Join(dests, "\n"), strings.Join(want, "\n"))
	}

	if !plan.ops[0].isDir {
		t.Fatal("first op should create the directory")
	}

	if plan.files != 3 || plan.bytes != int64(len("intro")+len("song!")+len("single")) {
		t.Fatalf("files = %d, bytes = %d", plan.files, plan.bytes)
	}

	if len(plan.skipped) != 2 {
		t.Fatalf("skipped = %v, want .DS_Store and ._ file", plan.skipped)
	}

	// 1 for the destination, 2 for the new directory, 1 per file.
	if plan.clusters != 6 {
		t.Fatalf("clusters = %d, want 6", plan.clusters)
	}
}

func TestPlanCopyAtRoot(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"a.mp3": "a"})

	plan, err := planCopy([]string{filepath.Join(root, "a.mp3")}, "/", 4096)
	if err != nil {
		t.Fatal(err)
	}

	if len(plan.ops) != 1 || plan.ops[0].dest != "/a.mp3" {
		t.Fatalf("ops = %+v", plan.ops)
	}
}

// openTestImage opens a writable clone of the YP-Z5 image through the
// same storage layer the TUI uses, or skips without YP_Z5_IMAGE.
func openTestImage(t *testing.T) *fat32.Filesystem {
	t.Helper()

	source := os.Getenv("YP_Z5_IMAGE")
	if source == "" {
		t.Skip("requires YP_Z5_IMAGE")
	}

	clone := filepath.Join(t.TempDir(), "image.img")

	if output, err := exec.Command("cp", "-c", source, clone).CombinedOutput(); err != nil {
		t.Fatalf("clone image: %v: %s", err, output)
	}

	if err := os.Chmod(clone, 0o600); err != nil {
		t.Fatal(err)
	}

	storage, err := openStorage(clone, true, false)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { storage.Close() })

	fs := fat32.NewReadWrite(storage, storage)

	if err := fs.Open(); err != nil {
		t.Fatal(err)
	}

	return fs
}

func TestCopyToImage(t *testing.T) {
	fs := openTestImage(t)

	root := t.TempDir()
	big := strings.Repeat("x", 3*1024*1024+17)

	writeTree(t, root, map[string]string{
		"New Album/01 - First.mp3":  big,
		"New Album/02 - Second.mp3": "second",
		"New Album/.DS_Store":       "junk",
	})

	freeBefore, _ := fs.FreeClusters()

	plan, err := planCopy([]string{filepath.Join(root, "New Album")}, "/Music", fs.BytesPerCluster())
	if err != nil {
		t.Fatal(err)
	}

	if err := checkSpace(fs, plan); err != nil {
		t.Fatal(err)
	}

	for _, op := range plan.ops {
		skipped, err := runCopyOp(fs, op)
		if err != nil {
			t.Fatalf("%s: %v", op.dest, err)
		}

		if skipped != "" {
			t.Fatalf("unexpectedly skipped %s", skipped)
		}
	}

	got, err := fs.ReadFile("/Music/New Album/01 - First.mp3")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte(big)) {
		t.Fatal("copied file contents differ")
	}

	if _, err := fs.Lookup("/Music/New Album/.DS_Store"); err == nil {
		t.Fatal(".DS_Store was copied")
	}

	freeAfter, _ := fs.FreeClusters()
	if used := int64(freeBefore - freeAfter); used > plan.clusters {
		t.Fatalf("copy used %d clusters, plan estimated %d", used, plan.clusters)
	}

	// Copying again skips existing files and merges into the directory.
	for _, op := range plan.ops {
		skipped, err := runCopyOp(fs, op)
		if err != nil {
			t.Fatalf("second copy of %s: %v", op.dest, err)
		}

		if !op.isDir && skipped == "" {
			t.Fatalf("second copy of %s was not skipped", op.dest)
		}
	}
}

func TestCheckSpaceRejectsOversizedCopy(t *testing.T) {
	fs := openTestImage(t)

	free, _ := fs.FreeClusters()

	plan := copyPlan{clusters: int64(free) + 1}

	if err := checkSpace(fs, plan); err == nil {
		t.Fatal("checkSpace accepted a copy larger than the free space")
	}
}

func TestCopyViews(t *testing.T) {
	m := model{
		path:         "/",
		dest:         "/Music",
		local:        &localBrowser{path: "/Users/me"},
		localConfirm: true,
		copyPlan: copyPlan{
			files:   3,
			bytes:   6 * 1024 * 1024,
			skipped: []string{"/Users/me/Album/.DS_Store (macOS metadata)"},
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
		skipped: m.copyPlan.skipped,
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
// /Music, is changed with the picker, and the copy lands there regardless
// of the folder open in the device browser.
func TestCopyToChosenDestination(t *testing.T) {
	fs := openTestImage(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTree(t, home, map[string]string{
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

	if view := m.localView(); !strings.Contains(view, "Destination: /Recordings") {
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

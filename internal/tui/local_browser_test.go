package tui

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLocalBrowserListsDirectoriesBeforeFiles verifies local entries are sorted with directories first.
func TestLocalBrowserListsDirectoriesBeforeFiles(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(
		filepath.Join(root, "z.txt"),
		[]byte("test"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.Mkdir(
		filepath.Join(root, "a-directory"),
		0755,
	); err != nil {
		t.Fatal(err)
	}

	browser := &localBrowser{path: root}

	if err := browser.reload(); err != nil {
		t.Fatal(err)
	}

	if len(browser.entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(browser.entries))
	}

	if !browser.entries[0].isDir {
		t.Fatalf("first entry is not a directory")
	}

	if browser.entries[0].name != "a-directory" {
		t.Fatalf(
			"first entry = %q, want %q",
			browser.entries[0].name,
			"a-directory",
		)
	}

	if browser.entries[1].name != "z.txt" {
		t.Fatalf(
			"second entry = %q, want %q",
			browser.entries[1].name,
			"z.txt",
		)
	}
}

// TestLocalBrowserNavigationChangesDirectory verifies entering and leaving a directory.
func TestLocalBrowserNavigationChangesDirectory(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")

	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}

	browser := &localBrowser{path: root}

	if err := browser.reload(); err != nil {
		t.Fatal(err)
	}

	if len(browser.entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(browser.entries))
	}

	if err := browser.enter(); err != nil {
		t.Fatal(err)
	}

	if browser.path != child {
		t.Fatalf(
			"current path = %q, want %q",
			browser.path,
			child,
		)
	}

	if err := browser.parent(); err != nil {
		t.Fatal(err)
	}

	if browser.path != root {
		t.Fatalf(
			"current path = %q, want %q",
			browser.path,
			root,
		)
	}
}

// TestLocalBrowserSelectionToggles verifies selecting and deselecting an entry.
func TestLocalBrowserSelectionToggles(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(
		filepath.Join(root, "test.txt"),
		[]byte("test"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	browser := &localBrowser{path: root}

	if err := browser.reload(); err != nil {
		t.Fatal(err)
	}

	browser.toggleSelection()

	selected := browser.selectedPaths()

	if len(selected) != 1 {
		t.Fatalf("got %d selected paths, want 1", len(selected))
	}

	if selected[0] != filepath.Join(root, "test.txt") {
		t.Fatalf(
			"selected path = %q, want %q",
			selected[0],
			filepath.Join(root, "test.txt"),
		)
	}

	browser.toggleSelection()

	if selected := browser.selectedPaths(); len(selected) != 0 {
		t.Fatalf("got %d selected paths after deselection, want 0", len(selected))
	}
}

// TestLocalBrowserKeepsSelectionsAcrossDirectories verifies selections survive navigation.
func TestLocalBrowserKeepsSelectionsAcrossDirectories(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")

	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}

	rootFile := filepath.Join(root, "root.txt")

	if err := os.WriteFile(rootFile, []byte("root"), 0644); err != nil {
		t.Fatal(err)
	}

	childFile := filepath.Join(child, "child.txt")

	if err := os.WriteFile(childFile, []byte("child"), 0644); err != nil {
		t.Fatal(err)
	}

	browser := &localBrowser{
		path:       root,
		selections: make(map[string]bool),
	}

	if err := browser.reload(); err != nil {
		t.Fatal(err)
	}

	// Select the root-level file.
	for i, entry := range browser.entries {
		if entry.path == rootFile {
			browser.cursor = i
			break
		}
	}

	browser.toggleSelection()

	// Navigate into the child directory.
	for i, entry := range browser.entries {
		if entry.path == child {
			browser.cursor = i
			break
		}
	}

	if err := browser.enter(); err != nil {
		t.Fatal(err)
	}

	// Select the file inside the child directory.
	for i, entry := range browser.entries {
		if entry.path == childFile {
			browser.cursor = i
			break
		}
	}

	browser.toggleSelection()

	// Return to the parent directory.
	if err := browser.parent(); err != nil {
		t.Fatal(err)
	}

	selected := browser.selectedPaths()

	if len(selected) != 2 {
		t.Fatalf("got %d selected paths, want 2", len(selected))
	}

	if !browser.selections[rootFile] {
		t.Fatalf("root file is no longer selected")
	}

	if !browser.selections[childFile] {
		t.Fatalf("child file is no longer selected")
	}
}

// TestLocalBrowserDeselectsPersistentSelection verifies deselection removes the persistent selection.
func TestLocalBrowserDeselectsPersistentSelection(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "test.txt")

	if err := os.WriteFile(file, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	browser := &localBrowser{
		path:       root,
		selections: make(map[string]bool),
	}

	if err := browser.reload(); err != nil {
		t.Fatal(err)
	}

	browser.toggleSelection()

	if len(browser.selectedPaths()) != 1 {
		t.Fatalf("expected one selection")
	}

	browser.toggleSelection()

	if len(browser.selectedPaths()) != 0 {
		t.Fatalf("expected no selections after deselection")
	}
}

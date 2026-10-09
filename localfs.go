package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type localEntry struct {
	name     string
	path     string
	isDir    bool
	size     int64
	selected bool
}

type localBrowser struct {
	path       string
	entries    []localEntry
	cursor     int
	offset     int
	selections map[string]bool
}

func newLocalBrowser() (*localBrowser, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home directory: %w", err)
	}

	browser := &localBrowser{
		path:       home,
		selections: make(map[string]bool),
	}

	if err := browser.reload(); err != nil {
		return nil, err
	}

	return browser, nil
}

func (b *localBrowser) reload() error {
	dirEntries, err := os.ReadDir(b.path)
	if err != nil {
		return fmt.Errorf("read local directory %q: %w", b.path, err)
	}

	entries := make([]localEntry, 0, len(dirEntries))

	for _, entry := range dirEntries {
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf(
				"stat local entry %q: %w",
				filepath.Join(b.path, entry.Name()),
				err,
			)
		}

		path := filepath.Join(b.path, entry.Name())

		entries = append(entries, localEntry{
			name:     entry.Name(),
			path:     path,
			isDir:    entry.IsDir(),
			size:     info.Size(),
			selected: b.selections[path],
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].isDir != entries[j].isDir {
			return entries[i].isDir
		}

		return entries[i].name < entries[j].name
	})

	b.entries = entries

	if b.cursor >= len(b.entries) {
		b.cursor = len(b.entries) - 1
	}

	if b.cursor < 0 {
		b.cursor = 0
	}

	return nil
}

func (b *localBrowser) current() *localEntry {
	if b.cursor < 0 || b.cursor >= len(b.entries) {
		return nil
	}

	return &b.entries[b.cursor]
}

func (b *localBrowser) moveUp() {
	if b.cursor > 0 {
		b.cursor--
	}
}

func (b *localBrowser) moveDown() {
	if b.cursor < len(b.entries)-1 {
		b.cursor++
	}
}

func (b *localBrowser) toggleSelection() {
	entry := b.current()
	if entry == nil {
		return
	}

	if b.selections == nil {
		b.selections = make(map[string]bool)
	}

	entry.selected = !entry.selected

	if entry.selected {
		b.selections[entry.path] = true
	} else {
		delete(b.selections, entry.path)
	}
}

func (b *localBrowser) enter() error {
	entry := b.current()
	if entry == nil || !entry.isDir {
		return nil
	}

	b.path = entry.path
	b.cursor = 0

	return b.reload()
}

func (b *localBrowser) parent() error {
	parent := filepath.Dir(b.path)

	if parent == b.path {
		return nil
	}

	b.path = parent
	b.cursor = 0

	return b.reload()
}

func (b *localBrowser) selectedPaths() []string {
	paths := make([]string, 0, len(b.selections))

	for path := range b.selections {
		paths = append(paths, path)
	}

	sort.Strings(paths)

	return paths
}

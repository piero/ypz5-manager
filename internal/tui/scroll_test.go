package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/piero/ypz5-manager/fat32"
)

func TestScrollOffset(t *testing.T) {
	tests := []struct {
		cursor, offset, n, rows int
		want                    int
	}{
		{cursor: 0, offset: 0, n: 5, rows: 10, want: 0},   // fits
		{cursor: 50, offset: 0, n: 100, rows: 0, want: 0}, // height unknown
		{cursor: 9, offset: 0, n: 100, rows: 10, want: 0},
		{cursor: 10, offset: 0, n: 100, rows: 10, want: 1},
		{cursor: 50, offset: 45, n: 100, rows: 10, want: 45}, // already visible
		{cursor: 3, offset: 45, n: 100, rows: 10, want: 3},
		{cursor: 99, offset: 0, n: 100, rows: 10, want: 90},
		{cursor: 5, offset: 95, n: 100, rows: 10, want: 5},
	}

	for _, tt := range tests {
		if got := scrollOffset(tt.cursor, tt.offset, tt.n, tt.rows); got != tt.want {
			t.Errorf("scrollOffset(%d, %d, %d, %d) = %d, want %d",
				tt.cursor, tt.offset, tt.n, tt.rows, got, tt.want)
		}
	}
}

func TestTruncateName(t *testing.T) {
	if got := truncateName("short.mp3", 40); got != "short.mp3" {
		t.Fatalf("got %q", got)
	}

	if got := truncateName("città-è-lunghissima.mp3", 10); got != "città-è-l…" {
		t.Fatalf("got %q", got)
	}
}

// TestLocalBrowserScrollsWithCursor reproduces the reported bug: with more
// local entries than fit on screen, moving down must scroll the list and
// keep the cursor visible.
func TestLocalBrowserScrollsWithCursor(t *testing.T) {
	const height = 20

	browser := &localBrowser{path: "/Users/me"}

	for i := range 100 {
		browser.entries = append(browser.entries, localEntry{
			name: fmt.Sprintf("file-%03d.mp3", i),
			path: fmt.Sprintf("/Users/me/file-%03d.mp3", i),
		})
	}

	var next tea.Model = model{path: "/Music", local: browser}

	press := func(code rune) {
		next, _ = next.Update(tea.KeyPressMsg{Code: code})
	}

	next, _ = next.Update(tea.WindowSizeMsg{Width: 100, Height: height})

	check := func(wantCursor int) {
		t.Helper()

		m := next.(model)

		if m.local.cursor != wantCursor {
			t.Fatalf("cursor = %d, want %d", m.local.cursor, wantCursor)
		}

		view := m.localView()

		if lines := strings.Count(view, "\n") + 1; lines > height {
			t.Fatalf("view has %d lines, terminal has %d:\n%s", lines, height, view)
		}

		cursorLine := fmt.Sprintf("> [ ] FILE file-%03d.mp3", wantCursor)

		if !strings.Contains(view, cursorLine) {
			t.Fatalf("cursor line %q not visible:\n%s", cursorLine, view)
		}
	}

	check(0)

	for range 30 {
		press(tea.KeyDown)
	}
	check(30)

	press(tea.KeyPgDown)
	check(30 + pageSize(next.(model).localRows()))

	press(tea.KeyEnd)
	check(99)

	if view := next.(model).localView(); !strings.Contains(view, "of 100]") {
		t.Fatalf("missing position line:\n%s", view)
	}

	press(tea.KeyHome)
	check(0)
}

// TestDeviceBrowserScrollsWithCursor checks the same for the device list.
func TestDeviceBrowserScrollsWithCursor(t *testing.T) {
	const height = 15

	m := model{path: "/Music"}

	for i := range 60 {
		m.entries = append(m.entries, fat32DirEntry(fmt.Sprintf("track-%02d.mp3", i)))
	}

	var next tea.Model = m

	next, _ = next.Update(tea.WindowSizeMsg{Width: 100, Height: height})

	for range 59 {
		next, _ = next.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}

	view := next.(model).browserView()

	if lines := strings.Count(view, "\n") + 1; lines > height {
		t.Fatalf("view has %d lines, terminal has %d:\n%s", lines, height, view)
	}

	if !strings.Contains(view, "> FILE track-59.mp3") {
		t.Fatalf("cursor not visible:\n%s", view)
	}
}

func fat32DirEntry(name string) fat32.DirectoryEntry {
	return fat32.DirectoryEntry{Name: name, Size: 1}
}

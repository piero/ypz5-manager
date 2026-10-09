package tui

import (
	"errors"
	"fmt"
	"path"

	tea "charm.land/bubbletea/v2"

	"github.com/piero/ypz5/fat32"
	"github.com/piero/ypz5/internal/transfer"
)

// updateBrowser handles keys in the YP-Z5 file browser.
//
// Navigation:
//
//	Directory
//	│
//	├── Enter on directory ──> Directory
//	│                           m.file = nil
//	│
//	└── Enter on file ────────> File view
//	                            m.file != nil
//	                                │
//	                           Backspace/h
//	                                │
//	                                ▼
//	                            Directory
//	                            m.file = nil
func (m model) updateBrowser(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// Errors are shown until the next key press.
		m.err = nil

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "n":
			if !m.writeEnabled {
				return m, nil
			}

			browser, err := newLocalBrowser()
			if err != nil {
				m.err = err
				return m, nil
			}

			m.local = browser
			m.localError = nil
			m.file = nil
			m.dest = defaultDest(m.fs)

		case "d":
			if !m.writeEnabled || len(m.entries) == 0 {
				return m, nil
			}

			entry := m.entries[m.cursor]

			if entry.Protected() {
				m.err = fmt.Errorf(
					"%q is read-only or a system entry and cannot be deleted",
					entry.Name,
				)
				return m, nil
			}

			m.deletePath = path.Join(m.path, entry.Name)
			m.deleteIsDir = entry.IsDir
			m.confirmDelete = true

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.entries)-1 {
				m.cursor++
			}

		case "pgup":
			m.cursor = moveCursor(m.cursor, -pageSize(m.browserRows()), len(m.entries))

		case "pgdown":
			m.cursor = moveCursor(m.cursor, pageSize(m.browserRows()), len(m.entries))

		case "home":
			m.cursor = 0

		case "end":
			m.cursor = moveCursor(len(m.entries), 0, len(m.entries))

		case "enter":
			if len(m.entries) == 0 {
				return m, nil
			}

			entry := m.entries[m.cursor]

			if !entry.IsDir {
				m.file = &entry
				return m, nil
			}

			path := path.Join(m.path, entry.Name)

			entries, err := loadDirectory(m.fs, path)
			if err != nil {
				m.err = err
				return m, nil
			}

			m.path = path
			m.entries = entries
			m.cursor = 0
			m.file = nil

		case "backspace", "h":
			if m.file != nil {
				m.file = nil
				return m, nil
			}

			if m.path == "/" {
				return m, nil
			}

			path := path.Dir(m.path)

			entries, err := loadDirectory(m.fs, path)
			if err != nil {
				m.err = err
				return m, nil
			}

			m.path = path
			m.entries = entries
			m.cursor = 0
		}
	}

	return m, nil
}

func (m model) browserHeader() string {
	p := devicePalette

	var s string

	s += p.titleBar("File manager") + "  " + modeTag(m.writeEnabled) + "\n\n"
	s += p.label("Source", m.source) + "\n"
	s += p.label("Path", m.path) + "\n\n"

	return s
}

func (m model) browserFooter() string {
	p := devicePalette

	var s string

	if m.err != nil {
		s += "\n" + errorLine(m.err) + "\n"
	}

	s += "\n" + p.help(
		"↑/↓", "navigate",
		"PgUp/PgDn", "page",
		"Enter", "open",
		"Backspace/h", "back",
	)

	actions := []string{}

	if m.writeEnabled {
		actions = append(actions, "n", "add from Mac", "d", "delete")
	}

	actions = append(actions, "q", "quit")

	s += "\n" + p.help(actions...) + "\n"

	return s
}

// browserRows is how many device entries fit on screen.
func (m model) browserRows() int {
	return listRows(m.height, m.browserHeader(), m.browserFooter())
}

func (m model) browserView() string {
	p := devicePalette
	s := m.browserHeader()

	start, end := visibleRange(m.offset, len(m.entries), m.browserRows())

	for i := start; i < end; i++ {
		entry := m.entries[i]

		kind := "FILE"
		size := transfer.FormatSize(int64(entry.Size))

		if entry.IsDir {
			kind = "DIR "
			size = ""
		}

		suffix := fmt.Sprintf(" %10s", size)

		if entry.Protected() {
			suffix += "  (protected)"
		}

		s += p.row(
			i == m.cursor,
			"",
			kind,
			fmt.Sprintf("%-*s", nameWidth, truncateName(entry.Name, nameWidth)),
			suffix,
		) + "\n"
	}

	s += dimStyle.render(positionLine(start, end, len(m.entries))) + "\n"
	s += m.browserFooter()

	return s
}

func loadDirectory(fs *fat32.Filesystem, path string) ([]fat32.DirectoryEntry, error) {
	return fs.ReadDir(path)
}

func (m *model) deleteEntry(path string) error {
	if !m.writeEnabled {
		return fmt.Errorf("filesystem is read-only")
	}

	removeErr := m.fs.RemoveAll(path)

	// Reload even after a failure: part of a directory tree may already
	// have been removed.
	entries, err := loadDirectory(m.fs, m.path)
	if err != nil {
		return errors.Join(removeErr, err)
	}

	m.entries = entries

	if m.cursor >= len(m.entries) {
		m.cursor = len(m.entries) - 1
	}

	if m.cursor < 0 {
		m.cursor = 0
	}

	return removeErr
}

func (m model) fileView() string {
	p := devicePalette
	entry := m.file

	var s string

	s += p.titleBar("File") + "  " + modeTag(m.writeEnabled) + "\n\n"
	s += p.label("Source       ", m.source) + "\n"
	s += p.label("Path         ", m.path) + "\n\n"
	s += p.label("Name         ", entry.Name) + "\n"
	s += p.label("8.3 name     ", entry.ShortName) + "\n"
	s += p.label("Size         ", fmt.Sprintf("%d bytes (%s)", entry.Size, transfer.FormatSize(int64(entry.Size)))) + "\n"

	if !entry.Modified.IsZero() {
		s += p.label("Modified     ", entry.Modified.Format("2006-01-02 15:04")) + "\n"
	}

	s += p.label("Start cluster", fmt.Sprint(entry.StartCluster)) + "\n"
	s += "\n" + p.help("Backspace/h", "back", "q", "quit") + "\n"

	return s
}

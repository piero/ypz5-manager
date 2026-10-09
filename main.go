package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/piero/ypz5/fat32"
)

type model struct {
	fs            *fat32.Filesystem
	source        string
	isDevice      bool
	writeEnabled  bool
	confirmDevice bool

	deletePath    string
	deleteIsDir   bool
	confirmDelete bool

	local        *localBrowser
	localError   error
	localConfirm bool
	copyPlan     copyPlan

	copy *copyJob

	path    string
	entries []fat32.DirectoryEntry
	cursor  int
	err     error
	file    *fat32.DirectoryEntry
}

func (m model) Init() tea.Cmd {
	return nil
}

/*
NAVIGATION:

Directory

	│
	├── Enter on directory ──> Directory
	│                           m.file = nil
	│
	└── Enter on file ────────> File view
	                            m.file != nil
	                                │
	                           Backspace/h
	                                │
	                                ▼
	                            Directory
	                            m.file = nil
*/
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.copy != nil {
		return m.updateCopy(msg)
	}

	if m.confirmDevice {
		return m.updateDeviceConfirmation(msg)
	}

	if m.confirmDelete {
		return m.updateDeleteConfirmation(msg)
	}

	if m.localConfirm {
		return m.updateLocalConfirmation(msg)
	}

	if m.local != nil {
		return m.updateLocalBrowser(msg)
	}

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

			m.deletePath = joinPath(m.path, entry.Name)
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

		case "enter":
			if len(m.entries) == 0 {
				return m, nil
			}

			entry := m.entries[m.cursor]

			if !entry.IsDir {
				m.file = &entry
				return m, nil
			}

			path := joinPath(m.path, entry.Name)

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

			path := parentPath(m.path)

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

func (m model) View() tea.View {
	if m.confirmDevice {
		return tea.NewView(m.deviceConfirmationView())
	}

	if m.confirmDelete {
		return tea.NewView(m.deleteConfirmationView())
	}

	if m.copy != nil {
		return tea.NewView(m.copyView())
	}

	if m.local != nil {
		return tea.NewView(m.localView())
	}

	if m.file != nil {
		return m.fileView()
	}

	var s string

	s += "YPZ5 Manager\n\n"
	s += fmt.Sprintf("Source: %s\n", m.source)

	if m.writeEnabled {
		s += "Mode:   READ/WRITE\n"
	} else {
		s += "Mode:   READ-ONLY\n"
	}

	s += fmt.Sprintf("\nPath: %s\n\n", m.path)

	for i, entry := range m.entries {
		cursor := "  "

		if i == m.cursor {
			cursor = "> "
		}

		kind := "FILE"

		if entry.IsDir {
			kind = "DIR "
		}

		s += fmt.Sprintf(
			"%s%-4s %-40s %d",
			cursor,
			kind,
			entry.Name,
			entry.Size,
		)

		if entry.Protected() {
			s += "  (protected)"
		}

		s += "\n"
	}

	if m.err != nil {
		s += fmt.Sprintf("\nError: %v\n", m.err)
	}

	s += "\n↑/↓: navigate   Enter: open"

	if m.writeEnabled {
		s += "   n: add   d: delete"
	}

	s += "   q: quit\n"

	return tea.NewView(s)
}

func loadDirectory(fs *fat32.Filesystem, path string) ([]fat32.DirectoryEntry, error) {
	return fs.ReadDir(path)
}

func parentPath(path string) string {
	if path == "/" {
		return "/"
	}

	index := strings.LastIndex(path, "/")

	if index <= 0 {
		return "/"
	}

	return path[:index]
}

func (m model) fileView() tea.View {
	entry := m.file

	var s string

	s += "YPZ5 Manager\n\n"
	s += fmt.Sprintf("Storage: %s\n", m.source)
	mode := "read-only"
	if m.writeEnabled {
		mode = "READ/WRITE"
	}
	s += fmt.Sprintf("Mode:    %s\n", mode)
	s += fmt.Sprintf("Path: %s\n\n", m.path)
	s += fmt.Sprintf("Name:          %s\n", entry.Name)
	s += "Type:          File\n"
	s += fmt.Sprintf("Size:          %d bytes\n", entry.Size)
	s += fmt.Sprintf("Start cluster: %d\n", entry.StartCluster)
	s += "\nBackspace/h: back   q: quit\n"

	return tea.NewView(s)
}

func fsDelete(m *model, path string) error {
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

// updateDeviceConfirmation handles the confirmation required before writing to a device.
func (m model) updateDeviceConfirmation(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "y", "enter":
			m.confirmDevice = false
			m.writeEnabled = true
			return m, nil

		case "n", "esc", "q", "ctrl+c":
			return m, tea.Quit
		}
	}

	return m, nil
}

// updateDeleteConfirmation handles confirmation before deleting a file or directory.
func (m model) updateDeleteConfirmation(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "y":
			m.confirmDelete = false

			if err := fsDelete(&m, m.deletePath); err != nil {
				m.err = err
			}

			m.deletePath = ""
			m.deleteIsDir = false

			return m, nil

		case "n", "esc":
			m.confirmDelete = false
			m.deletePath = ""
			m.deleteIsDir = false
		}
	}

	return m, nil
}

// deviceConfirmationView displays the safety warning shown before enabling device writes.
func (m model) deviceConfirmationView() string {
	var s string

	s += "YPZ5 Manager\n\n"
	s += "WARNING\n\n"
	s += "You are about to open a physical device for writing.\n\n"
	s += fmt.Sprintf("Device: %s\n\n", m.source)
	s += "Writing to the device can corrupt its filesystem or\n"
	s += "destroy data if the device is disconnected during a write.\n\n"
	s += "Continue and enable READ/WRITE mode?\n\n"
	s += "y/Enter: continue   n/Esc/q: quit\n"

	return s
}

// deleteConfirmationView displays the appropriate deletion warning.
func (m model) deleteConfirmationView() string {
	var s string

	s += "YPZ5 Manager\n\n"
	s += "WARNING\n\n"
	s += fmt.Sprintf("Delete: %s\n\n", m.deletePath)

	if m.deleteIsDir {
		s += "This will recursively delete the directory and\n"
		s += "ALL of its contents.\n\n"
	} else {
		s += "This will permanently delete the file.\n\n"
	}

	s += "This operation cannot be undone.\n\n"
	s += "y: delete   n/Esc: cancel\n"

	return s
}

// updateLocalBrowser handles navigation and selection in the local filesystem browser.
func (m model) updateLocalBrowser(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.local = nil
			m.localError = nil

		case "q", "ctrl+c":
			return m, tea.Quit

		case "up", "k":
			m.local.moveUp()
			m.localError = nil

		case "down", "j":
			m.local.moveDown()
			m.localError = nil

		case "space":
			m.local.toggleSelection()
			m.localError = nil

		case "enter":
			if err := m.local.enter(); err != nil {
				m.localError = err
			} else {
				m.localError = nil
			}

		case "backspace", "h":
			if err := m.local.parent(); err != nil {
				m.localError = err
			} else {
				m.localError = nil
			}

		case "c":
			if len(m.local.selectedPaths()) == 0 {
				return m, nil
			}

			plan, err := planCopy(
				m.local.selectedPaths(),
				m.path,
				m.fs.BytesPerCluster(),
			)
			if err != nil {
				m.localError = err
				return m, nil
			}

			if err := checkSpace(m.fs, plan); err != nil {
				m.localError = err
				return m, nil
			}

			m.copyPlan = plan
			m.localConfirm = true
		}
	}

	return m, nil
}

func (m model) updateLocalConfirmation(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "y", "enter":
			m.localConfirm = false
			m.local = nil
			m.copy = &copyJob{plan: m.copyPlan}
			m.copyPlan = copyPlan{}

			return m, m.nextCopyStep()

		case "n", "esc":
			m.localConfirm = false
		}
	}

	return m, nil
}

func (m model) localView() string {
	if m.localError != nil {
		return fmt.Sprintf(
			"Add files\n\nError: %v\n\nEsc: back",
			m.localError,
		)
	}

	var s string

	s += "Add files to YP-Z5\n\n"
	s += fmt.Sprintf("Local path: %s\n", m.local.path)
	s += fmt.Sprintf("Destination: %s\n\n", m.path)

	// s += fmt.Sprintf(
	// 	"DEBUG cursor=%d selected=%d currentSelected=%v\n\n",
	// 	m.local.cursor,
	// 	len(m.local.selectedPaths()),
	// 	func() bool {
	// 		entry := m.local.current()
	// 		return entry != nil && entry.selected
	// 	}(),
	// )

	for i, entry := range m.local.entries {
		cursor := "  "

		if i == m.local.cursor {
			cursor = "> "
		}

		selected := " "

		// if m.local.selections[entry.path] {
		// 	selected = "*"
		// }
		if entry.selected {
			selected = "*"
		}

		kind := "FILE"

		if entry.isDir {
			kind = "DIR "
		}

		s += fmt.Sprintf(
			"%s[%s] %-4s %-40s",
			cursor,
			selected,
			kind,
			entry.name,
		)

		if !entry.isDir {
			s += fmt.Sprintf(" %d", entry.size)
		}

		s += "\n"
	}

	s += fmt.Sprintf(
		"\nSelected: %d",
		len(m.local.selectedPaths()),
	)

	s += "\n\n↑/↓: navigate   Space: select   Enter: open"
	s += "\nBackspace/h: parent   c: copy   Esc: cancel\n"

	if m.localConfirm {
		s += "\n"
		s += fmt.Sprintf(
			"Copy %d files (%s) to %s?\n",
			m.copyPlan.files,
			formatSize(m.copyPlan.bytes),
			m.path,
		)

		switch n := len(m.copyPlan.skipped); n {
		case 0:
		case 1:
			s += "1 item will be skipped.\n"
		default:
			s += fmt.Sprintf("%d items will be skipped.\n", n)
		}
		s += "y/Enter: confirm   n/Esc: cancel\n"
	}

	return s
}

func joinPath(parent, name string) string {
	if parent == "/" {
		return "/" + name
	}

	return parent + "/" + name
}

func main() {
	imagePath := flag.String(
		"image",
		"",
		"path to a FAT32 image",
	)

	devicePath := flag.String(
		"device",
		"",
		"path to a FAT32 device or partition",
	)

	write := flag.Bool(
		"write",
		false,
		"open the storage for writing",
	)

	flag.Parse()

	if *imagePath == "" && *devicePath == "" {
		fmt.Fprintln(
			os.Stderr,
			"error: specify either --image or --device",
		)
		flag.Usage()
		os.Exit(2)
	}

	if *imagePath != "" && *devicePath != "" {
		fmt.Fprintln(
			os.Stderr,
			"error: --image and --device are mutually exclusive",
		)
		os.Exit(2)
	}

	path := *imagePath
	if path == "" {
		path = *devicePath
	}

	storage, err := openStorage(path, *write, *devicePath != "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}
	defer storage.Close()

	var fs *fat32.Filesystem

	if *write {
		fs = fat32.NewReadWrite(storage, storage)
	} else {
		fs = fat32.New(storage)
	}

	if err := fs.Open(); err != nil {
		fmt.Fprintln(os.Stderr, "filesystem:", err)
		os.Exit(1)
	}

	entries, err := fs.ReadDir("/")
	if err != nil {
		fmt.Fprintln(os.Stderr, "readdir:", err)
		os.Exit(1)
	}

	m := model{
		fs:            fs,
		source:        storage.Source(),
		isDevice:      storage.IsDevice(),
		writeEnabled:  !storage.IsDevice() && *write,
		confirmDevice: storage.IsDevice() && *write,
		path:          "/",
		entries:       entries,
	}

	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

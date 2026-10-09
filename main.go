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

	// dest is the device folder files are copied into; picker is open
	// while the user chooses another one.
	dest        string
	picker      *destPicker
	pickerError error

	copy *copyJob

	path    string
	entries []fat32.DirectoryEntry
	cursor  int
	offset  int
	err     error
	file    *fat32.DirectoryEntry

	// Terminal size, from tea.WindowSizeMsg; 0 until known.
	width  int
	height int
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
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = size.Width
		m.height = size.Height
	}

	next, cmd := m.update(msg)

	updated := next.(model)
	updated.syncScroll()

	return updated, cmd
}

// syncScroll keeps the cursor of each list on screen after the cursor,
// the list or the terminal size changed.
func (m *model) syncScroll() {
	m.offset = scrollOffset(m.cursor, m.offset, len(m.entries), m.browserRows())

	if m.picker != nil {
		m.picker.offset = scrollOffset(
			m.picker.cursor,
			m.picker.offset,
			len(m.picker.entries),
			m.pickerRows(),
		)
	}

	if m.local != nil {
		m.local.offset = scrollOffset(
			m.local.cursor,
			m.local.offset,
			len(m.local.entries),
			m.localRows(),
		)
	}
}

// pageSize is how far PgUp/PgDn move in a list showing rows entries.
func pageSize(rows int) int {
	if rows <= 1 {
		return 10
	}

	return rows - 1
}

func (m model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

	if m.picker != nil {
		return m.updateDestPicker(msg)
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
	if m.file != nil && m.copy == nil && m.local == nil &&
		!m.confirmDevice && !m.confirmDelete {
		return screen(m.fileView())
	}

	return screen(m.content())
}

// screen shows content full-screen, so long lists can be scrolled
// instead of running past the bottom of the terminal.
func screen(content string) tea.View {
	v := tea.NewView(content)
	v.AltScreen = true

	return v
}

func (m model) content() string {
	if m.confirmDevice {
		return m.deviceConfirmationView()
	}

	if m.confirmDelete {
		return m.deleteConfirmationView()
	}

	if m.copy != nil {
		return m.copyView()
	}

	if m.picker != nil {
		return m.pickerView()
	}

	if m.local != nil {
		return m.localView()
	}

	return m.browserView()
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
		size := formatSize(int64(entry.Size))

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

func (m model) fileView() string {
	p := devicePalette
	entry := m.file

	var s string

	s += p.titleBar("File") + "  " + modeTag(m.writeEnabled) + "\n\n"
	s += p.label("Source       ", m.source) + "\n"
	s += p.label("Path         ", m.path) + "\n\n"
	s += p.label("Name         ", entry.Name) + "\n"
	s += p.label("8.3 name     ", entry.ShortName) + "\n"
	s += p.label("Size         ", fmt.Sprintf("%d bytes (%s)", entry.Size, formatSize(int64(entry.Size)))) + "\n"

	if !entry.Modified.IsZero() {
		s += p.label("Modified     ", entry.Modified.Format("2006-01-02 15:04")) + "\n"
	}

	s += p.label("Start cluster", fmt.Sprint(entry.StartCluster)) + "\n"
	s += "\n" + p.help("Backspace/h", "back", "q", "quit") + "\n"

	return s
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
	p := devicePalette

	var s string

	s += dangerBar.render(" WARNING ") + " " + p.accent.render("Open device for writing") + "\n\n"
	s += p.label("Device", m.source) + "\n\n"
	s += "Writing to the device can corrupt its filesystem or\n"
	s += "destroy data if the device is disconnected during a write.\n\n"
	s += warningStyle.render("Continue and enable READ/WRITE mode?") + "\n\n"
	s += p.help("y/Enter", "continue", "n/Esc/q", "quit") + "\n"

	return s
}

// deleteConfirmationView displays the appropriate deletion warning.
func (m model) deleteConfirmationView() string {
	p := devicePalette

	var s string

	s += dangerBar.render(" DELETE ") + " " + p.accent.render("from the YP-Z5") + "\n\n"
	s += p.label("Delete", m.deletePath) + "\n\n"

	if m.deleteIsDir {
		s += warningStyle.render("This will recursively delete the directory and") + "\n"
		s += warningStyle.render("ALL of its contents.") + "\n\n"
	} else {
		s += "This will permanently delete the file.\n\n"
	}

	s += errorStyle.render("This operation cannot be undone.") + "\n\n"
	s += p.help("y", "delete", "n/Esc", "cancel") + "\n"

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

		case "pgup":
			m.local.cursor = moveCursor(m.local.cursor, -pageSize(m.localRows()), len(m.local.entries))

		case "pgdown":
			m.local.cursor = moveCursor(m.local.cursor, pageSize(m.localRows()), len(m.local.entries))

		case "home":
			m.local.cursor = 0

		case "end":
			m.local.cursor = moveCursor(len(m.local.entries), 0, len(m.local.entries))

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

		case "t":
			picker, err := newDestPicker(m.fs, m.dest)
			if err != nil {
				m.localError = err
				return m, nil
			}

			m.picker = picker

		case "c":
			if len(m.local.selectedPaths()) == 0 {
				return m, nil
			}

			plan, err := planCopy(
				m.local.selectedPaths(),
				m.dest,
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
			m.copy = &copyJob{plan: m.copyPlan, dest: m.dest}
			m.copyPlan = copyPlan{}

			return m, m.nextCopyStep()

		case "n", "esc":
			m.localConfirm = false
		}
	}

	return m, nil
}

func (m model) localHeader() string {
	p := localPalette

	var s string

	s += p.titleBar("Add files from this Mac") + "\n\n"
	s += p.label("Local folder   ", m.local.path) + "\n"
	s += devicePalette.label("Copy to (YP-Z5)", m.dest) + "  " + dimStyle.render("(t: change)") + "\n\n"

	return s
}

func (m model) localFooter() string {
	p := localPalette

	var s string

	s += "\n" + p.label("Selected", fmt.Sprint(len(m.local.selectedPaths())))

	s += "\n\n" + p.help(
		"↑/↓", "navigate",
		"PgUp/PgDn", "page",
		"Space", "select",
		"Enter", "open",
	)
	s += "\n" + p.help(
		"Backspace/h", "parent",
		"t", "change destination",
		"c", "copy",
		"Esc", "cancel",
	) + "\n"

	if m.localConfirm {
		s += "\n"
		s += warningStyle.renderf(
			"Copy %d files (%s) to %s?",
			m.copyPlan.files,
			formatSize(m.copyPlan.bytes),
			m.dest,
		) + "\n"

		switch n := len(m.copyPlan.skipped); n {
		case 0:
		case 1:
			s += dimStyle.render("1 item will be skipped.") + "\n"
		default:
			s += dimStyle.renderf("%d items will be skipped.", n) + "\n"
		}

		s += p.help("y/Enter", "confirm", "n/Esc", "cancel") + "\n"
	}

	return s
}

// localRows is how many local entries fit on screen.
func (m model) localRows() int {
	return listRows(m.height, m.localHeader(), m.localFooter())
}

func (m model) localView() string {
	p := localPalette

	if m.localError != nil {
		return p.titleBar("Add files from this Mac") + "\n\n" +
			errorLine(m.localError) + "\n\n" +
			p.help("Esc", "back")
	}

	s := m.localHeader()

	start, end := visibleRange(m.local.offset, len(m.local.entries), m.localRows())

	for i := start; i < end; i++ {
		entry := m.local.entries[i]
		isCursor := i == m.local.cursor

		mark := " "
		if entry.selected {
			mark = "*"

			if !isCursor {
				mark = p.marked.render(mark)
			}
		}

		kind := "FILE"
		size := formatSize(entry.size)

		if entry.isDir {
			kind = "DIR "
			size = ""
		}

		s += p.row(
			isCursor,
			"["+mark+"] ",
			kind,
			fmt.Sprintf("%-*s", nameWidth, truncateName(entry.name, nameWidth)),
			fmt.Sprintf(" %10s", size),
		) + "\n"
	}

	s += dimStyle.render(positionLine(start, end, len(m.local.entries))) + "\n"
	s += m.localFooter()

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

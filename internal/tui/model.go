// Package tui is the terminal user interface: a browser for the YP-Z5's
// FAT32 filesystem, a browser for the Mac's local files to copy from,
// and the screens in between.
package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/piero/ypz5-manager/fat32"
	"github.com/piero/ypz5-manager/internal/transfer"
)

// model is the Bubble Tea model of the whole TUI. One screen is active
// at a time; update dispatches messages to it and View draws it.
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
	copyPlan     transfer.Plan

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

// Run shows the TUI for fs until the user quits. source names the image
// or device for display. Writing a device needs an extra confirmation,
// so write only takes effect for a device after the user confirms.
func Run(fs *fat32.Filesystem, source string, isDevice, write bool) error {
	entries, err := fs.ReadDir("/")
	if err != nil {
		return fmt.Errorf("read root directory: %w", err)
	}

	m := model{
		fs:            fs,
		source:        source,
		isDevice:      isDevice,
		writeEnabled:  !isDevice && write,
		confirmDevice: isDevice && write,
		path:          "/",
		entries:       entries,
	}

	_, err = tea.NewProgram(m).Run()

	return err
}

func (m model) Init() tea.Cmd {
	return nil
}

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

	return m.updateBrowser(msg)
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

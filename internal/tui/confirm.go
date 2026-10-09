package tui

import (
	tea "charm.land/bubbletea/v2"
)

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

			if err := m.deleteEntry(m.deletePath); err != nil {
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

package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/piero/ypz5/internal/transfer"
)

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

			plan, err := transfer.PlanCopy(
				m.local.selectedPaths(),
				m.dest,
				m.fs.BytesPerCluster(),
			)
			if err != nil {
				m.localError = err
				return m, nil
			}

			if err := transfer.CheckSpace(m.fs, plan); err != nil {
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
			m.copyPlan = transfer.Plan{}

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
			m.copyPlan.Files,
			transfer.FormatSize(m.copyPlan.Bytes),
			m.dest,
		) + "\n"

		switch n := len(m.copyPlan.Skipped); n {
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
		size := transfer.FormatSize(entry.size)

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

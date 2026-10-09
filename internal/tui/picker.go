package tui

import (
	"path"

	tea "charm.land/bubbletea/v2"

	"github.com/piero/ypz5/fat32"
)

// defaultDestination is where files are copied unless the user picks
// another folder: the player builds its library from /Music.
const defaultDestination = "/Music"

// destPicker lets the user choose the device folder to copy into. It
// lists folders only.
type destPicker struct {
	path    string
	entries []fat32.DirectoryEntry
	cursor  int
	offset  int
}

// defaultDest returns /Music, spelled as on the device, or / if the
// device has no Music folder.
func defaultDest(fs *fat32.Filesystem) string {
	entry, err := fs.Find(defaultDestination)
	if err != nil {
		return "/"
	}

	return path.Join("/", entry.Name)
}

func newDestPicker(fs *fat32.Filesystem, path string) (*destPicker, error) {
	picker := &destPicker{}

	if err := picker.load(fs, path); err != nil {
		return nil, err
	}

	return picker, nil
}

// load shows the folders inside path.
func (p *destPicker) load(fs *fat32.Filesystem, path string) error {
	entries, err := fs.ReadDir(path)
	if err != nil {
		return err
	}

	dirs := entries[:0]

	for _, entry := range entries {
		if entry.IsDir {
			dirs = append(dirs, entry)
		}
	}

	p.path = path
	p.entries = dirs
	p.cursor = 0
	p.offset = 0

	return nil
}

func (m model) updateDestPicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}

	p := m.picker
	m.pickerError = nil

	switch key.String() {
	case "esc":
		m.picker = nil

	case "s":
		m.dest = p.path
		m.picker = nil

	case "up", "k":
		p.cursor = moveCursor(p.cursor, -1, len(p.entries))

	case "down", "j":
		p.cursor = moveCursor(p.cursor, 1, len(p.entries))

	case "pgup":
		p.cursor = moveCursor(p.cursor, -pageSize(m.pickerRows()), len(p.entries))

	case "pgdown":
		p.cursor = moveCursor(p.cursor, pageSize(m.pickerRows()), len(p.entries))

	case "home":
		p.cursor = 0

	case "end":
		p.cursor = moveCursor(len(p.entries), 0, len(p.entries))

	case "enter":
		if len(p.entries) == 0 {
			return m, nil
		}

		path := path.Join(p.path, p.entries[p.cursor].Name)

		if err := p.load(m.fs, path); err != nil {
			m.pickerError = err
		}

	case "backspace", "h":
		if p.path == "/" {
			return m, nil
		}

		if err := p.load(m.fs, path.Dir(p.path)); err != nil {
			m.pickerError = err
		}
	}

	return m, nil
}

func (m model) pickerHeader() string {
	p := devicePalette

	var s string

	s += p.titleBar("Choose where to copy") + "\n\n"
	s += p.label("Folder", m.picker.path) + "\n\n"

	return s
}

func (m model) pickerFooter() string {
	p := devicePalette

	var s string

	if m.pickerError != nil {
		s += "\n" + errorLine(m.pickerError) + "\n"
	}

	s += "\n" + p.help(
		"↑/↓", "navigate",
		"PgUp/PgDn", "page",
		"Enter", "open",
		"Backspace/h", "back",
	)
	s += "\n" + p.help("s", "copy into this folder", "Esc", "cancel") + "\n"

	return s
}

func (m model) pickerRows() int {
	return listRows(m.height, m.pickerHeader(), m.pickerFooter())
}

func (m model) pickerView() string {
	p := devicePalette
	picker := m.picker
	s := m.pickerHeader()

	if len(picker.entries) == 0 {
		s += dimStyle.render("  (no folders)") + "\n"
	}

	start, end := visibleRange(picker.offset, len(picker.entries), m.pickerRows())

	for i := start; i < end; i++ {
		s += p.row(
			i == picker.cursor,
			"",
			"DIR ",
			truncateName(picker.entries[i].Name, nameWidth),
			"",
		) + "\n"
	}

	s += dimStyle.render(positionLine(start, end, len(picker.entries))) + "\n"
	s += m.pickerFooter()

	return s
}

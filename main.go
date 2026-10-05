package main

import (
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
	path          string
	entries       []fat32.DirectoryEntry
	cursor        int
	err           error
	file          *fat32.DirectoryEntry

	deletePath    string
	deleteIsDir   bool
	confirmDelete bool
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

	if m.confirmDelete {
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case "y":
				if err := fsDelete(&m, m.deletePath); err != nil {
					m.err = err
				}

				m.confirmDelete = false
				m.deletePath = ""
				m.deleteIsDir = false

			case "n", "esc":
				m.confirmDelete = false
				m.deletePath = ""
				m.deleteIsDir = false
			}
		}

		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.entries)-1 {
				m.cursor++
			}

		// View directory or file
		case "enter":
			if len(m.entries) == 0 {
				return m, nil
			}

			entry := m.entries[m.cursor]

			if !entry.IsDir {
				m.file = &entry
				return m, nil
			}

			path := m.path
			if path != "/" {
				path += "/"
			}
			path += entry.Name

			entries, err := loadDirectory(m.fs, path)
			if err != nil {
				m.err = err
				return m, nil
			}

			m.path = path
			m.entries = entries
			m.cursor = 0
			m.file = nil

		// Navigate back
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

		// Delete file (not directory)
		case "d":
			if !m.writeEnabled {
				return m, nil
			}

			if len(m.entries) == 0 {
				return m, nil
			}

			entry := m.entries[m.cursor]

			deletePath := m.path
			if deletePath != "/" {
				deletePath += "/"
			}
			deletePath += entry.Name

			m.deletePath = deletePath
			m.deleteIsDir = entry.IsDir
			m.confirmDelete = true

			return m, nil
		}
	}

	return m, nil
}

func (m model) View() tea.View {
	if m.confirmDevice {
		return tea.NewView(
			fmt.Sprintf(
				"YPZ5 Manager\n\n"+
					"WARNING: this is a real device.\n\n"+
					"Storage: %s\n\n"+
					"Write access has been requested.\n"+
					"Any filesystem changes will modify the device directly.\n\n"+
					"Continue with write access? [y/N]\n",
				m.source,
			),
		)
	}

	if m.confirmDelete {
		if m.deleteIsDir {
			return tea.NewView(
				fmt.Sprintf(
					"YPZ5 Manager\n\n"+
						"Delete directory?\n\n"+
						"  %s\n\n"+
						"WARNING: this will recursively delete\n"+
						"the directory and all of its contents.\n\n"+
						"This operation cannot be undone.\n\n"+
						"Delete recursively? [y/N]\n",
					m.deletePath,
				),
			)
		}

		return tea.NewView(
			fmt.Sprintf(
				"YPZ5 Manager\n\n"+
					"Delete file?\n\n"+
					"  %s\n\n"+
					"This operation cannot be undone.\n\n"+
					"Delete? [y/N]\n",
				m.deletePath,
			),
		)
	}

	if m.err != nil {
		return tea.NewView(fmt.Sprintf("Error: %v\n", m.err))
	}

	if m.file != nil {
		return m.fileView()
	}

	var s string

	s += "YPZ5 Manager\n\n"
	s += fmt.Sprintf("Storage: %s\n", m.source)

	mode := "read-only"
	if m.writeEnabled {
		mode = "READ/WRITE"
	}

	s += fmt.Sprintf("Mode:    %s\n", mode)
	s += fmt.Sprintf("Path:    %s\n\n", m.path)

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
			"%s%-4s %-40s %d\n",
			cursor,
			kind,
			entry.Name,
			entry.Size,
		)
	}

	s += "\n↑/↓: navigate   Enter: open   Backspace/h: parent   d: delete   q: quit\n"

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

	if err := m.fs.RemoveAll(path); err != nil {
		return err
	}

	entries, err := loadDirectory(m.fs, m.path)
	if err != nil {
		return err
	}

	m.entries = entries

	if m.cursor >= len(m.entries) {
		m.cursor = len(m.entries) - 1
	}

	if m.cursor < 0 {
		m.cursor = 0
	}

	return nil
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

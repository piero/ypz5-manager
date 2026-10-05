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
	fs      *fat32.Filesystem
	path    string
	entries []fat32.DirectoryEntry
	cursor  int
	err     error
	file    *fat32.DirectoryEntry
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
	if m.err != nil {
		return tea.NewView(fmt.Sprintf("Error: %v\n", m.err))
	}

	if m.file != nil {
		return m.fileView()
	}

	var s string

	s += fmt.Sprintf("YPZ5 Manager\n\n")
	s += fmt.Sprintf("Path: %s\n\n", m.path)

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

	s += "\n↑/↓: navigate   Enter: open   Backspace/h: parent   q: quit\n"

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
	s += fmt.Sprintf("Path: %s\n\n", m.path)
	s += fmt.Sprintf("Name:          %s\n", entry.Name)
	s += fmt.Sprint("Type:          File\n")
	s += fmt.Sprintf("Size:          %d bytes\n", entry.Size)
	s += fmt.Sprintf("Start cluster: %d\n", entry.StartCluster)
	s += "\nBackspace/h: back   q: quit\n"

	return tea.NewView(s)
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

	storage, err := openStorage(path, *write)
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
		fs:      fs,
		path:    "/",
		entries: entries,
	}

	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/piero/ypz5/fat32"
)

// Application state
type model struct{}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m model) View() tea.View {
	return tea.NewView(
		"YPZ5 Manager\n\n" +
			"q: quit\n",
	)
}

func main() {
	// if _, err := tea.NewProgram(model{}).Run(); err != nil {
	// 	fmt.Fprintln(os.Stderr, "Error:", err)
	// 	os.Exit(1)
	// }
	f, err := os.Open("/dev/rdisk4s1")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()

	fs := fat32.New(f)

	if err := fs.Open(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// data, err := fs.ReadRootCluster()
	// if err != nil {
	// 	fmt.Fprintln(os.Stderr, err)
	// 	os.Exit(1)
	// }
	//
	// 	fmt.Printf("root cluster: %d bytes\n", len(data))
	// 	fmt.Printf("%x\n", data[:64])
	//
	// 	if err := os.WriteFile("root-cluster.bin", data, 0644); err != nil {
	// 		fmt.Fprintln(os.Stderr, err)
	// 		os.Exit(1)
	// 	}

	entries, err := fs.ReadRootDirectory()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	for _, entry := range entries {
		kind := "FILE"
		if entry.IsDir {
			kind = "DIR "
		}

		fmt.Printf(
			"%-4s %-30s cluster=%-8d size=%d\n",
			kind,
			entry.Name,
			entry.StartCluster,
			entry.Size,
		)
	}
}

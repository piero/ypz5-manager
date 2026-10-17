// Command ypz5-manager is a file manager for the FAT32 filesystem of the Samsung
// YP-Z5 MP3 player, working on the device or on a disk image.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/piero/ypz5-manager/fat32"
	"github.com/piero/ypz5-manager/internal/storage"
	"github.com/piero/ypz5-manager/internal/tui"
)

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

	st, err := storage.Open(path, *write, *devicePath != "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}
	defer st.Close()

	var fs *fat32.Filesystem

	if *write {
		fs = fat32.NewReadWrite(st, st)
	} else {
		fs = fat32.New(st)
	}

	if err := fs.Open(); err != nil {
		fmt.Fprintln(os.Stderr, "filesystem:", err)
		os.Exit(1)
	}

	if err := tui.Run(fs, st.Source(), st.IsDevice(), *write); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

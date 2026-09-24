package main

import (
	"fmt"
	"os"

	"github.com/piero/ypz5/fat32"
)

func main() {
	fmt.Println("opening device...")

	f, err := os.Open("/dev/rdisk4s1")
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}
	defer f.Close()

	fmt.Println("creating filesystem...")

	fs := fat32.New(f)

	fmt.Println("opening filesystem...")

	if err := fs.Open(); err != nil {
		fmt.Fprintln(os.Stderr, "fs.Open:", err)
		os.Exit(1)
	}

	fmt.Println("finding /Music...")

	dir, err := fs.Find("/Music")
	if err != nil {
		fmt.Fprintln(os.Stderr, "fs.Find:", err)
		os.Exit(1)
	}

	fmt.Printf(
		"found %q at cluster %d\n",
		dir.Name,
		dir.StartCluster,
	)

	fmt.Println("reading the /Music directory...")

	entries, err := fs.ReadDir("/Music")
	if err != nil {
		fmt.Fprintln(os.Stderr, "fs.ReadDir:", err)
		os.Exit(1)
	}

	fmt.Printf("found %d entries\n", len(entries))

	for _, entry := range entries {
		kind := "FILE"
		if entry.IsDir {
			kind = "DIR "
		}

		fmt.Printf(
			"%-4s %-50s cluster=%-8d size=%d\n",
			kind,
			entry.Name,
			entry.StartCluster,
			entry.Size,
		)
	}

	fmt.Println("\nNew test")
	data, err := fs.ReadFile("/Music/Cartoni animati/Pollon.mp3")
	if err != nil {
		fmt.Fprintln(os.Stderr, "fs.ReadFile:", err)
		os.Exit(1)
	}

	fmt.Printf("read %d bytes\n", len(data))
	fmt.Printf("first 32 bytes: %q\n", data[:min(32, len(data))])
}

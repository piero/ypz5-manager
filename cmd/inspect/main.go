package main

import (
	"fmt"
	"os"

	"github.com/piero/ypz5/fat32"
)

func main() {
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

	entries, err := fs.ReadDir(fs.RootCluster())
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

	fmt.Println("Looking into Cluster 369...")
	
	// Read directory at cluster 369
	entries, err = fs.ReadDir(369)
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

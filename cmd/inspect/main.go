package main

import (
	"fmt"
	"os"

	"github.com/piero/ypz5/fat32"
)

func main() {
	deviceName := "/dev/rdisk4s1"
	fmt.Println("opening device:", deviceName)

	f, err := os.Open(deviceName)
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

	fmt.Printf("bytes/sector:     %d\n", fs.BootSector().BytesPerSector)
	fmt.Printf("sectors/cluster:  %d\n", fs.BootSector().SectorsPerCluster)
	fmt.Printf("total sectors:    %d\n", fs.BootSector().TotalSectors)
	fmt.Printf("cluster count:    %d\n", fs.BootSector().ClusterCount())
	fmt.Printf("max cluster:      %d\n", fs.BootSector().MaxCluster())

	freeCluster, err := fs.FindFreeCluster(2)
	if err != nil {
		fmt.Fprintln(os.Stderr, "find free cluster:", err)
		os.Exit(1)
	}

	fmt.Printf("first free cluster: %d\n", freeCluster)

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
			"%-4s %-50s cluster=%-8d size=%-8d dirCluster=%-8d dirOffset=%d\n",
			kind,
			entry.Name,
			entry.StartCluster,
			entry.Size,
			entry.EntryCluster,
			entry.EntryOffset,
		)
	}

	filePath := "/Music/Cartoni animati/Pollon.mp3"
	fmt.Println("\nTesting", filePath)
	data, err := fs.ReadFile(filePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fs.ReadFile:", err)
		os.Exit(1)
	}

	fmt.Printf("read %d bytes\n", len(data))
	fmt.Printf("first 32 bytes: %q\n", data[:min(32, len(data))])

	entry, err := fs.Lookup(filePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lookup:", err)
		os.Exit(1)
	}

	fmt.Printf(
		"directory entry: cluster=%d offset=%d LFN entries=%d\n",
		entry.EntryCluster,
		entry.EntryOffset,
		entry.LFNEntries,
	)

	chain, err := fs.ClusterChain(entry.StartCluster)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cluster chain:", err)
		os.Exit(1)
	}

	expectedClusters := (uint64(entry.Size) +
		uint64(fs.BootSector().BytesPerSector)*
			uint64(fs.BootSector().SectorsPerCluster) - 1) /
		(uint64(fs.BootSector().BytesPerSector) *
			uint64(fs.BootSector().SectorsPerCluster))

	fmt.Printf("file size:          %d bytes\n", entry.Size)
	fmt.Printf("cluster size:       %d bytes\n",
		fs.BootSector().BytesPerSector*
			uint16(fs.BootSector().SectorsPerCluster))
	fmt.Printf("cluster chain size: %d\n", len(chain))
	fmt.Printf("expected clusters:  %d\n", expectedClusters)
	fmt.Printf("first cluster:      %d\n", chain[0])
	fmt.Printf("last cluster:       %d\n", chain[len(chain)-1])

	fmt.Println("\nInspecting directory entries around Pollon.mp3")

	if err := fs.DebugDirectory("/Music/Cartoni animati"); err != nil {
		fmt.Fprintln(os.Stderr, "debug directory:", err)
		os.Exit(1)
	}
}

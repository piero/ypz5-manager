// Command inspect prints the FAT32 geometry of an image or device and a
// detailed listing of one directory. It never writes.
//
//	go run ./cmd/inspect [-path /Music] images/yp-z5a-full-20261009.img
//	sudo go run ./cmd/inspect -path /Music /dev/rdisk4
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/piero/ypz5/fat32"
)

func main() {
	dir := flag.String("path", "/", "directory to list")

	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: inspect [-path DIR] IMAGE_OR_DEVICE")
		flag.PrintDefaults()
	}

	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	if err := inspect(flag.Arg(0), *dir); err != nil {
		fmt.Fprintln(os.Stderr, "inspect:", err)
		os.Exit(1)
	}
}

func inspect(source, dir string) error {
	f, err := os.Open(source)
	if err != nil {
		return err
	}
	defer f.Close()

	offset, err := fat32.PartitionOffset(f)
	if err != nil {
		return err
	}

	// The section's size is unknown for devices; the filesystem never
	// reads past the volume anyway.
	fs := fat32.New(io.NewSectionReader(f, offset, 1<<62))

	if err := fs.Open(); err != nil {
		return err
	}

	boot := fs.BootSector()

	fmt.Printf("source:            %s\n", source)
	fmt.Printf("partition offset:  %d\n", offset)
	fmt.Printf("bytes/sector:      %d\n", boot.BytesPerSector)
	fmt.Printf("sectors/cluster:   %d\n", boot.SectorsPerCluster)
	fmt.Printf("cluster size:      %d\n", fs.BytesPerCluster())
	fmt.Printf("FATs:              %d\n", boot.NumberOfFATs)
	fmt.Printf("total sectors:     %d\n", boot.TotalSectors)
	fmt.Printf("clusters:          %d\n", boot.ClusterCount())

	if free, ok := fs.FreeClusters(); ok {
		fmt.Printf(
			"free (FSInfo):     %d clusters, %d bytes\n",
			free,
			int64(free)*fs.BytesPerCluster(),
		)
	} else {
		fmt.Println("free (FSInfo):     unknown")
	}

	entries, err := fs.ReadDir(dir)
	if err != nil {
		return err
	}

	fmt.Printf("\n%s: %d entries\n\n", dir, len(entries))
	fmt.Printf("%-6s %-16s %-12s %10s %8s  %s\n", "ATTR", "MODIFIED", "8.3", "SIZE", "CLUSTER", "NAME")

	for _, entry := range entries {
		modified := "-"
		if !entry.Modified.IsZero() {
			modified = entry.Modified.Format("2006-01-02 15:04")
		}

		fmt.Printf(
			"%-6s %-16s %-12s %10d %8d  %s\n",
			attributes(entry.Attr),
			modified,
			entry.ShortName,
			entry.Size,
			entry.StartCluster,
			entry.Name,
		)
	}

	return nil
}

// attributes renders attribute bits as "DRHSA" flags.
func attributes(attr uint8) string {
	flags := []struct {
		bit  uint8
		char byte
	}{
		{fat32.AttrDirectory, 'D'},
		{fat32.AttrReadOnly, 'R'},
		{fat32.AttrHidden, 'H'},
		{fat32.AttrSystem, 'S'},
		{fat32.AttrArchive, 'A'},
	}

	s := make([]byte, len(flags))

	for i, flag := range flags {
		s[i] = '-'

		if attr&flag.bit != 0 {
			s[i] = flag.char
		}
	}

	return string(s)
}

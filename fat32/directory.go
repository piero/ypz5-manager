package fat32

import (
	"encoding/binary"
	"fmt"
	"strings"
)

type DirectoryEntry struct {
	Name         string
	ShortName    string
	IsDir        bool
	Size         uint32
	StartCluster uint32
}

func parseShortName(data []byte) string {
	name := strings.TrimRight(string(data[0:8]), " ")
	ext := strings.TrimRight(string(data[8:11]), " ")

	if ext == "" {
		return name
	}

	return name + "." + ext
}

func parseDirectoryEntry(data []byte) (DirectoryEntry, error) {
	if len(data) != 32 {
		return DirectoryEntry{}, fmt.Errorf(
			"directory entry has %d bytes, want 32",
			len(data),
		)
	}

	attr := data[0x0B]

	if attr == 0x0F {
		return DirectoryEntry{}, fmt.Errorf(
			"directory entry is an LFN entry",
		)
	}

	startClusterHigh := binary.LittleEndian.Uint16(data[0x14:0x16])
	startClusterLow := binary.LittleEndian.Uint16(data[0x1A:0x1C])

	startCluster :=
		uint32(startClusterHigh)<<16 |
			uint32(startClusterLow)

	return DirectoryEntry{
		ShortName:    parseShortName(data),
		IsDir:        attr&0x10 != 0,
		Size:         binary.LittleEndian.Uint32(data[0x1C:0x20]),
		StartCluster: startCluster,
	}, nil
}

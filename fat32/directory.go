package fat32

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf16"
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

type lfnEntry struct {
	Sequence uint8
	IsLast   bool
	Name     string
}

func parseLFNEntry(data []byte) (lfnEntry, error) {
	if len(data) != 32 {
		return lfnEntry{}, fmt.Errorf(
			"LFN entry has %d bytes, want 32",
			len(data),
		)
	}

	if data[0x0B] != 0x0F {
		return lfnEntry{}, fmt.Errorf(
			"not an LFN entry: attributes = %#02x",
			data[0x0B],
		)
	}

	sequence := data[0x00]

	var chars []uint16

	// Characters 1–5.
	for offset := 0x01; offset < 0x0B; offset += 2 {
		chars = append(chars, binary.LittleEndian.Uint16(data[offset:offset+2]))
	}

	// Characters 6–11.
	for offset := 0x0E; offset < 0x1A; offset += 2 {
		chars = append(chars, binary.LittleEndian.Uint16(data[offset:offset+2]))
	}

	// Characters 12–13.
	for offset := 0x1C; offset < 0x20; offset += 2 {
		chars = append(chars, binary.LittleEndian.Uint16(data[offset:offset+2]))
	}

	// Stop at the UTF-16 terminator.
	for i, char := range chars {
		if char == 0x0000 {
			chars = chars[:i]
			break
		}
	}

	return lfnEntry{
		Sequence: sequence & 0x3F,
		IsLast:   sequence&0x40 != 0,
		Name:     string(utf16.Decode(chars)),
	}, nil
}

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

func combineLFN(entries []lfnEntry) string {
	var name string

	for i := len(entries) - 1; i >= 0; i-- {
		name += entries[i].Name
	}

	return name
}

func (fs *Filesystem) readDirectory(startCluster uint32) ([]DirectoryEntry, error) {
	clusters, err := fs.readClusterChain(startCluster)
	if err != nil {
		return nil, err
	}

	var entries []DirectoryEntry
	var lfn []lfnEntry

	for _, cluster := range clusters {
		data, err := fs.readCluster(cluster)
		if err != nil {
			return nil, err
		}

		for offset := 0; offset < len(data); offset += 32 {
			entry := data[offset : offset+32]

			// 0x00 means there are no more directory entries.
			if entry[0] == 0x00 {
				return entries, nil
			}

			// 0xE5 means this entry has been deleted.
			if entry[0] == 0xE5 {
				lfn = nil
				continue
			}

			// Long filename entry.
			if entry[0x0B] == 0x0F {
				name, err := parseLFNEntry(entry)
				if err != nil {
					return nil, err
				}

				lfn = append(lfn, name)
				continue
			}

			// Normal directory entry.
			dirEntry, err := parseDirectoryEntry(entry)
			if err != nil {
				return nil, err
			}

			if len(lfn) > 0 {
				dirEntry.Name = combineLFN(lfn)
			} else {
				dirEntry.Name = dirEntry.ShortName
			}

			entries = append(entries, dirEntry)

			lfn = nil
		}
	}

	return entries, nil
}

func (fs *Filesystem) ReadRootDirectory() ([]DirectoryEntry, error) {
	return fs.readDirectory(fs.boot.RootCluster)
}

func (fs *Filesystem) ReadDir(path string) ([]DirectoryEntry, error) {
	dir, err := fs.Find(path)
	if err != nil {
		return nil, err
	}

	entries, err := fs.readDirectory(dir.StartCluster)
	if err != nil {
		return nil, err
	}

	filtered := entries[:0]

	for _, entry := range entries {
		if entry.Name == "." || entry.Name == ".." {
			continue
		}

		filtered = append(filtered, entry)
	}

	return filtered, nil
}

func (fs *Filesystem) Find(path string) (DirectoryEntry, error) {
	if path == "/" {
		return DirectoryEntry{
			Name:         "/",
			IsDir:        true,
			StartCluster: fs.RootCluster(),
		}, nil
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")

	currentCluster := fs.RootCluster()

	for _, part := range parts {
		entries, err := fs.readDirectory(currentCluster)
		if err != nil {
			return DirectoryEntry{}, err
		}

		found := false

		for _, entry := range entries {
			if entry.Name == part {
				if !entry.IsDir {
					return DirectoryEntry{}, fmt.Errorf(
						"%q is not a directory",
						part,
					)
				}

				currentCluster = entry.StartCluster
				found = true
				break
			}
		}

		if !found {
			return DirectoryEntry{}, fmt.Errorf(
				"directory not found: %s",
				path,
			)
		}
	}

	return DirectoryEntry{
		Name:         parts[len(parts)-1],
		IsDir:        true,
		StartCluster: currentCluster,
	}, nil
}

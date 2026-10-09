package fat32

import (
	"encoding/binary"
	"fmt"
	"time"
)

// Directory entry attribute bits.
const (
	AttrReadOnly  = 0x01
	AttrHidden    = 0x02
	AttrSystem    = 0x04
	AttrVolumeID  = 0x08
	AttrDirectory = 0x10
	AttrArchive   = 0x20
)

// what the application sees
type DirectoryEntry struct {
	Name         string
	ShortName    string
	IsDir        bool
	Attr         uint8
	Modified     time.Time
	Size         uint32
	StartCluster uint32

	EntryCluster uint32
	EntryOffset  uint32
	LFNEntries   uint8
}

// Protected reports whether the entry is read-only or a system entry.
// Such entries are never deleted or replaced.
func (e DirectoryEntry) Protected() bool {
	return e.Attr&(AttrReadOnly|AttrSystem) != 0
}

func isProtected(entry DirectoryEntry) bool {
	return entry.Protected()
}

type directoryEntryLocation struct {
	Cluster uint32
	Offset  uint32
}

// how an entry is physically represented on FAT32
type directoryRecord struct {
	Entry DirectoryEntry
	Short directoryEntryLocation
	LFN   []directoryEntryLocation
}

type directoryEntrySlot struct {
	Cluster uint32
	Offset  uint32
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
		ShortName: parseShortName(data),
		IsDir:     attr&AttrDirectory != 0,
		Attr:      attr,
		Modified: decodeFATDateTime(
			binary.LittleEndian.Uint16(data[entryWriteDate:]),
			binary.LittleEndian.Uint16(data[entryWriteTime:]),
		),
		Size:         binary.LittleEndian.Uint32(data[0x1C:0x20]),
		StartCluster: startCluster,
	}, nil
}

func encodeDirectoryEntry(
	shortName [11]byte,
	attr byte,
	startCluster uint32,
	size uint32,
) [32]byte {
	var data [32]byte

	copy(data[0:11], shortName[:])

	data[0x0B] = attr

	binary.LittleEndian.PutUint16(
		data[0x14:0x16],
		uint16(startCluster>>16),
	)

	binary.LittleEndian.PutUint16(
		data[0x1A:0x1C],
		uint16(startCluster),
	)

	binary.LittleEndian.PutUint32(
		data[0x1C:0x20],
		size,
	)

	return data
}

func encodeSpecialDirectoryEntry(
	name string,
	startCluster uint32,
) ([32]byte, error) {
	var shortName [11]byte

	for i := range shortName {
		shortName[i] = ' '
	}

	switch name {
	case ".":
		shortName[0] = '.'

	case "..":
		shortName[0] = '.'
		shortName[1] = '.'

	default:
		return [32]byte{}, fmt.Errorf(
			"invalid special directory entry: %q",
			name,
		)
	}

	return encodeDirectoryEntry(
		shortName,
		0x10,
		startCluster,
		0,
	), nil
}

type encodedDirectoryRecord struct {
	LFN   [][32]byte
	Short [32]byte
}

func encodeDirectoryRecord(
	name string,
	startCluster uint32,
	size uint32,
) (encodedDirectoryRecord, error) {
	var shortName [11]byte

	if isShortName(name) {
		var err error

		shortName, err = encodeShortName(name)
		if err != nil {
			return encodedDirectoryRecord{}, err
		}
	} else {
		var err error

		shortName, err = generateShortName(name, 1)
		if err != nil {
			return encodedDirectoryRecord{}, err
		}
	}

	return encodeDirectoryRecordWithShortName(
		name,
		shortName,
		startCluster,
		size,
		false,
	)
}

func encodeDirectoryRecordWithShortName(
	name string,
	shortName [11]byte,
	startCluster uint32,
	size uint32,
	isDir bool,
) (encodedDirectoryRecord, error) {
	var result encodedDirectoryRecord

	attr := byte(0x20) // archive

	if isDir {
		attr = 0x10 // directory
	}

	shortEntry := encodeDirectoryEntry(
		shortName,
		attr,
		startCluster,
		size,
	)

	if isShortName(name) {
		shortEntry[0x0C] = shortNameCaseFlags(name)
	}

	result.Short = shortEntry

	if !isShortName(name) {
		checksum := shortNameChecksum(shortEntry[:])

		lfnEntries, err := encodeLFNEntries(name, checksum)
		if err != nil {
			return result, err
		}

		result.LFN = lfnEntries
	}

	return result, nil
}

package fat32

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	mbrSectorSize = 512

	mbrPartitionTableOffset = 0x1BE
	mbrPartitionEntrySize   = 16
	mbrPartitionCount       = 4
	mbrSignatureOffset      = 0x1FE

	fat32PartitionTypeLBA   = 0x0C
	fat32PartitionTypeCHS   = 0x0B
	fat32PartitionTypeLBA64 = 0x1C
)

// PartitionOffset returns the byte offset of the FAT32 filesystem in r:
// 0 when r is the filesystem itself (an image of a partition), or the
// start of the first FAT32 partition when r is a whole disk with an MBR.
func PartitionOffset(r io.ReaderAt) (int64, error) {
	// First try the storage itself as a FAT32 filesystem.
	bootSector := make([]byte, mbrSectorSize)

	if _, err := r.ReadAt(bootSector, 0); err != nil {
		return 0, fmt.Errorf("read boot sector: %w", err)
	}

	if looksLikeFAT32BootSector(bootSector) {
		return 0, nil
	}

	// Otherwise look for an MBR partition containing FAT32.
	mbr := bootSector

	if binary.LittleEndian.Uint16(mbr[mbrSignatureOffset:]) != 0xAA55 {
		return 0, fmt.Errorf(
			"storage is neither a FAT32 filesystem nor an MBR disk",
		)
	}

	for i := 0; i < mbrPartitionCount; i++ {
		offset := mbrPartitionTableOffset + i*mbrPartitionEntrySize
		entry := mbr[offset : offset+mbrPartitionEntrySize]

		partitionType := entry[4]

		if !isFAT32PartitionType(partitionType) {
			continue
		}

		startSector := binary.LittleEndian.Uint32(entry[8:12])

		if startSector == 0 {
			continue
		}

		return int64(startSector) * mbrSectorSize, nil
	}

	return 0, fmt.Errorf("no FAT32 partition found")
}

func looksLikeFAT32BootSector(data []byte) bool {
	if len(data) < mbrSectorSize {
		return false
	}

	// FAT32 stores the filesystem signature at offset 0x52.
	return string(data[0x52:0x5A]) == "FAT32   "
}

func isFAT32PartitionType(partitionType byte) bool {
	switch partitionType {
	case fat32PartitionTypeCHS,
		fat32PartitionTypeLBA,
		fat32PartitionTypeLBA64:
		return true
	default:
		return false
	}
}

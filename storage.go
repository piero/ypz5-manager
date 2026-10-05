package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

const (
	sectorSize = 512

	mbrPartitionTableOffset = 0x1BE
	mbrPartitionEntrySize   = 16
	mbrPartitionCount       = 4
	mbrSignatureOffset      = 0x1FE

	fat32PartitionTypeLBA   = 0x0C
	fat32PartitionTypeCHS   = 0x0B
	fat32PartitionTypeLBA64 = 0x1C
)

type storage struct {
	file   *os.File
	offset int64
	source string
}

func openStorage(path string, writable bool) (*storage, error) {
	flags := os.O_RDONLY
	if writable {
		flags = os.O_RDWR
	}

	f, err := os.OpenFile(path, flags, 0)
	if err != nil {
		return nil, err
	}

	offset, err := detectFilesystemOffset(f)
	if err != nil {
		f.Close()
		return nil, err
	}

	return &storage{
		file:   f,
		offset: offset,
		source: path,
	}, nil
}

func (s *storage) Source() string {
	return s.source
}

func (s *storage) Close() error {
	return s.file.Close()
}

func (s *storage) ReadAt(p []byte, off int64) (int, error) {
	return s.file.ReadAt(p, s.offset+off)
}

func (s *storage) WriteAt(p []byte, off int64) (int, error) {
	return s.file.WriteAt(p, s.offset+off)
}

var (
	_ io.ReaderAt = (*storage)(nil)
	_ io.WriterAt = (*storage)(nil)
)

func detectFilesystemOffset(r io.ReaderAt) (int64, error) {
	// First try the storage itself as a FAT32 filesystem.
	bootSector := make([]byte, sectorSize)

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

		return int64(startSector) * sectorSize, nil
	}

	return 0, fmt.Errorf("no FAT32 partition found")
}

func looksLikeFAT32BootSector(data []byte) bool {
	if len(data) < sectorSize {
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

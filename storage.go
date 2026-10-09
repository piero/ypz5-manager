package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
	file     *os.File
	offset   int64
	source   string
	isDevice bool
}

func openStorage(path string, writable bool, isDevice bool) (*storage, error) {
	if writable && isDevice {
		if err := checkNotMounted(path); err != nil {
			return nil, err
		}
	}

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
		file:     f,
		offset:   offset,
		source:   path,
		isDevice: isDevice,
	}, nil
}

func (s *storage) Source() string {
	return s.source
}

func (s *storage) IsDevice() bool {
	return s.isDevice
}

func (s *storage) Close() error {
	return s.file.Close()
}

// Sync flushes all writes to the medium. The fat32 package calls it at
// the end of every operation that modifies the filesystem.
func (s *storage) Sync() error {
	return fullSync(s.file)
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

// listMountedDevices is a variable so tests can fake the mount table.
var listMountedDevices = mountedDevices

var wholeDiskPattern = regexp.MustCompile(`^r?(disk[0-9]+)`)

// checkNotMounted refuses to write to a device while any partition of
// the same disk is mounted: the OS would keep its own cached view of the
// filesystem and the two writers would corrupt it.
func checkNotMounted(devicePath string) error {
	match := wholeDiskPattern.FindStringSubmatch(filepath.Base(devicePath))
	if match == nil {
		return nil
	}

	disk := match[1]

	mounted, err := listMountedDevices()
	if err != nil {
		return fmt.Errorf("list mounted filesystems: %w", err)
	}

	for _, device := range mounted {
		if isSameDisk(filepath.Base(device), disk) {
			return fmt.Errorf(
				"%s is mounted; run `diskutil unmountDisk /dev/%s` first",
				device,
				disk,
			)
		}
	}

	return nil
}

// isSameDisk reports whether name ("disk4", "disk4s1") is disk or one of
// its partitions.
func isSameDisk(name, disk string) bool {
	if name == disk {
		return true
	}

	rest, ok := strings.CutPrefix(name, disk+"s")

	return ok && rest != ""
}

// Package storage opens disk images and devices holding a FAT32
// filesystem. It finds the filesystem inside a partitioned disk, flushes
// writes to the medium, and refuses to write to a mounted device.
package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/piero/ypz5/fat32"
)

// Storage is a disk image or device holding a FAT32 filesystem. Reads
// and writes are relative to the start of the filesystem, which may be
// inside a partition of a whole-disk image or device.
type Storage struct {
	file     *os.File
	offset   int64
	source   string
	isDevice bool
}

// Open opens an image file or device. A writable device is refused
// while any partition of its disk is mounted.
func Open(path string, writable bool, isDevice bool) (*Storage, error) {
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

	offset, err := fat32.PartitionOffset(f)
	if err != nil {
		f.Close()
		return nil, err
	}

	return &Storage{
		file:     f,
		offset:   offset,
		source:   path,
		isDevice: isDevice,
	}, nil
}

func (s *Storage) Source() string {
	return s.source
}

func (s *Storage) IsDevice() bool {
	return s.isDevice
}

func (s *Storage) Close() error {
	return s.file.Close()
}

// Sync flushes all writes to the medium. The fat32 package calls it at
// the end of every operation that modifies the filesystem.
func (s *Storage) Sync() error {
	return fullSync(s.file)
}

func (s *Storage) ReadAt(p []byte, off int64) (int, error) {
	return s.file.ReadAt(p, s.offset+off)
}

func (s *Storage) WriteAt(p []byte, off int64) (int, error) {
	return s.file.WriteAt(p, s.offset+off)
}

var (
	_ io.ReaderAt = (*Storage)(nil)
	_ io.WriterAt = (*Storage)(nil)
)

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

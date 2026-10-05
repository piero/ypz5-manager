package main

import (
	"encoding/binary"
	"os"
	"testing"
)

// TestOpenStorageReadsAndWritesAtTheDetectedPartitionOffset verifies automatic MBR partition detection.
func TestOpenStorageReadsAndWritesAtTheDetectedPartitionOffset(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "storage-*.img")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	partitionOffset := int64(16384)

	mbr := make([]byte, 512)
	mbr[0x1FE] = 0x55
	mbr[0x1FF] = 0xAA

	entryOffset := 0x1BE
	mbr[entryOffset+4] = fat32PartitionTypeLBA
	binary.LittleEndian.PutUint32(
		mbr[entryOffset+8:entryOffset+12],
		uint32(partitionOffset/sectorSize),
	)

	if _, err := file.Write(mbr); err != nil {
		t.Fatal(err)
	}

	if err := file.Truncate(partitionOffset + 4096); err != nil {
		t.Fatal(err)
	}

	if _, err := file.WriteAt([]byte("original"), partitionOffset); err != nil {
		t.Fatal(err)
	}

	storage, err := openStorage(file.Name(), true, false)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()

	buf := make([]byte, 8)
	if _, err := storage.ReadAt(buf, 0); err != nil {
		t.Fatal(err)
	}

	if string(buf) != "original" {
		t.Fatalf("got %q, want %q", string(buf), "original")
	}

	if _, err := storage.WriteAt([]byte("updated"), 0); err != nil {
		t.Fatal(err)
	}

	raw := make([]byte, 7)
	if _, err := file.ReadAt(raw, partitionOffset); err != nil {
		t.Fatal(err)
	}

	if string(raw) != "updated" {
		t.Fatalf("got %q, want %q", string(raw), "updated")
	}
}

// TestOpenStorageUsesZeroOffsetForFAT32PartitionImages verifies partition images need no partition table.
func TestOpenStorageUsesZeroOffsetForFAT32PartitionImages(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "storage-*.img")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	bootSector := make([]byte, 512)
	copy(bootSector[0x52:0x5A], "FAT32   ")

	if _, err := file.Write(bootSector); err != nil {
		t.Fatal(err)
	}

	storage, err := openStorage(file.Name(), false, false)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()

	buf := make([]byte, 8)
	if _, err := storage.ReadAt(buf, 0x52); err != nil {
		t.Fatal(err)
	}

	if string(buf) != "FAT32   " {
		t.Fatalf("got %q, want %q", string(buf), "FAT32   ")
	}
}

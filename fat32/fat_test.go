package fat32

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestReadFATEntry(t *testing.T) {
	// We calculate the FAT entry location correctly
	const fatOffset = 100

	data := make([]byte, fatOffset+16)

	// Cluster 2 → cluster 3
	binary.LittleEndian.PutUint32(
		data[fatOffset+2*4:],
		3,
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		layout: Layout{
			FATOffset: fatOffset,
		},
	}

	got, err := fs.readFATEntry(2)
	if err != nil {
		t.Fatal(err)
	}

	if got != 3 {
		t.Fatalf("FAT entry = %d, want 3", got)
	}
}

func TestReadFATEntryMasksReservedBits(t *testing.T) {
	// We interpret the FAT32 entry correctly

	const fatOffset = 100

	data := make([]byte, fatOffset+16)

	// Lower 28 bits = cluster 3
	// Upper 4 bits should be ignored
	value := uint32(0xA0000003)

	binary.LittleEndian.PutUint32(
		data[fatOffset+2*4:],
		value,
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		layout: Layout{
			FATOffset: fatOffset,
		},
	}

	got, err := fs.readFATEntry(2)
	if err != nil {
		t.Fatal(err)
	}

	if got != 3 {
		t.Fatalf("FAT entry = %#x, want %#x", got, uint32(3))
	}
}

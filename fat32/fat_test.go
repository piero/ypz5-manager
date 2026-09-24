package fat32

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestReadFATEntry(t *testing.T) {
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

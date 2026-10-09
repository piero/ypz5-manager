package fat32

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestFindEntryFile(t *testing.T) {
	const (
		fatOffset       = 512
		dataOffset      = 1024
		sectorSize      = 512
		bytesPerCluster = 64
	)

	data := make([]byte, dataOffset+bytesPerCluster*2)

	// FAT[2] -> EOF.
	binary.LittleEndian.PutUint32(
		data[fatOffset+2*4:],
		fat32EOF,
	)

	// Root directory, cluster 2.
	//
	// We'll put a single file entry there.
	dirEntry := data[dataOffset : dataOffset+32]

	copy(dirEntry[0:8], []byte("TEST    "))
	copy(dirEntry[8:11], []byte("TXT"))
	dirEntry[0x0B] = 0x00

	// File starts at cluster 3.
	binary.LittleEndian.PutUint16(dirEntry[0x1A:0x1C], 3)

	// File size = 5.
	binary.LittleEndian.PutUint32(dirEntry[0x1C:0x20], 5)

	// FAT[3] -> EOF.
	binary.LittleEndian.PutUint32(
		data[fatOffset+3*4:],
		fat32EOF,
	)

	// Cluster 3 contains the file.
	copy(
		data[dataOffset+bytesPerCluster:],
		[]byte("hello"),
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector: sectorSize,
			SectorsPerFAT:  1,
			RootCluster:    2,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	// Verify that Lookup finds the file.
	found, err := fs.Lookup("/TEST.TXT")
	if err != nil {
		t.Fatal(err)
	}

	if found.Name != "TEST.TXT" {
		t.Fatalf("name = %q, want %q", found.Name, "TEST.TXT")
	}

	if found.IsDir {
		t.Fatal("file reported as directory")
	}

	if found.Size != 5 {
		t.Fatalf("size = %d, want 5", found.Size)
	}

	// Verify that ReadFile can read the file by path.
	got, err := fs.ReadFile("/TEST.TXT")
	if err != nil {
		t.Fatal(err)
	}

	want := []byte("hello")

	if !bytes.Equal(got, want) {
		t.Fatalf("data = %q, want %q", got, want)
	}
}

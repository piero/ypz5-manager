package fat32

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestReadFile(t *testing.T) {
	const (
		fatOffset       = 512
		dataOffset      = 1024
		sectorSize      = 512
		bytesPerCluster = 8
	)

	data := make([]byte, dataOffset+bytesPerCluster*2)

	// FAT[2] -> cluster 3
	binary.LittleEndian.PutUint32(
		data[fatOffset+2*4:],
		3,
	)

	// FAT[3] -> EOF
	binary.LittleEndian.PutUint32(
		data[fatOffset+3*4:],
		fat32EOF,
	)

	// Cluster 2 contains the first part of the file.
	copy(
		data[dataOffset:dataOffset+bytesPerCluster],
		[]byte("hello, w"),
	)
	
	// Cluster 3 contains the rest.
	copy(
		data[dataOffset+bytesPerCluster:],
		[]byte("orld!"),
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector: sectorSize,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	entry := DirectoryEntry{
		Name:         "test.txt",
		IsDir:        false,
		Size:         13,
		StartCluster: 2,
	}

	clusters, err := fs.readClusterChain(entry.StartCluster)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("clusters = %v", clusters)

	got, err := fs.readFile(entry)
	if err != nil {
		t.Fatal(err)
	}

	want := []byte("hello, world!")

	if !bytes.Equal(got, want) {
		t.Fatalf("data = %q, want %q", got, want)
	}
}

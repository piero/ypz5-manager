package fat32

import (
	"bytes"
	"strings"
	"testing"
)

func TestReadCluster(t *testing.T) {
	const (
		dataOffset      = 100
		bytesPerCluster = 16
		cluster         = 3
	)

	data := make([]byte, dataOffset+bytesPerCluster*2)

	want := []byte{
		0x01, 0x02, 0x03, 0x04,
	}

	copy(
		data[dataOffset+bytesPerCluster:],
		want,
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		layout: Layout{
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	got, err := fs.readCluster(cluster)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got[:len(want)], want) {
		t.Fatalf("cluster data = %x, want %x", got[:len(want)], want)
	}
}

func TestWriteCluster(t *testing.T) {
	const (
		sectorSize  = 512
		dataOffset  = 1024
		clusterSize = 8
	)

	data := make([]byte, dataOffset+clusterSize*2)

	fs := NewReadWrite(
		bytes.NewReader(data),
		writerAtBuffer{data: data},
	)

	fs.boot = BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: 1,
		TotalSectors:      10,
	}

	fs.layout = Layout{
		DataOffset:      dataOffset,
		BytesPerCluster: clusterSize,
	}

	want := []byte("testdata")

	if err := fs.writeCluster(2, want); err != nil {
		t.Fatal(err)
	}

	got := data[dataOffset : dataOffset+clusterSize]

	if !bytes.Equal(got, want) {
		t.Fatalf("cluster data = %q, want %q", got, want)
	}
}

func TestWriteClusterWriteFailure(t *testing.T) {
	const (
		sectorSize  = 512
		dataOffset  = 1024
		clusterSize = 8
	)

	data := make([]byte, dataOffset+clusterSize*2)

	writer := &failingWriterAt{
		data:         data,
		failAfter:    0,
		partialWrite: true,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		writer,
	)

	fs.boot = BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: 1,
		TotalSectors:      10,
	}

	fs.layout = Layout{
		DataOffset:      dataOffset,
		BytesPerCluster: clusterSize,
	}

	err := fs.writeCluster(2, []byte("testdata"))
	if err == nil {
		t.Fatal("writeCluster unexpectedly succeeded")
	}

	if !strings.Contains(err.Error(), "short write") &&
		!strings.Contains(err.Error(), "injected partial write failure") {
		t.Fatalf("unexpected error: %v", err)
	}
}

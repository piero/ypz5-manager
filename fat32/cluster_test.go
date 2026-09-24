package fat32

import (
	"bytes"
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
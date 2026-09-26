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

func TestWriteFileData(t *testing.T) {
	const (
		dataOffset  = 1024
		clusterSize = 8
	)

	data := make([]byte, dataOffset+clusterSize*2)

	fs := NewReadWrite(
		bytes.NewReader(data),
		writerAtBuffer{data: data},
	)

	fs.boot = BootSector{
		BytesPerSector:    512,
		SectorsPerCluster: 1,
		TotalSectors:      10,
	}

	fs.layout = Layout{
		DataOffset:      dataOffset,
		BytesPerCluster: clusterSize,
	}

	fileData := []byte("hello, world!")

	chain := []uint32{2, 3}

	if err := fs.writeFileData(chain, fileData); err != nil {
		t.Fatal(err)
	}

	wantCluster2 := []byte("hello, w")
	wantCluster3 := []byte{
		'o', 'r', 'l', 'd', '!',
		0, 0, 0,
	}

	gotCluster2 := data[dataOffset : dataOffset+clusterSize]
	gotCluster3 := data[dataOffset+clusterSize : dataOffset+2*clusterSize]

	if !bytes.Equal(gotCluster2, wantCluster2) {
		t.Fatalf(
			"cluster 2 = %v, want %v",
			gotCluster2,
			wantCluster2,
		)
	}

	if !bytes.Equal(gotCluster3, wantCluster3) {
		t.Fatalf(
			"cluster 3 = %v, want %v",
			gotCluster3,
			wantCluster3,
		)
	}
}

func TestWriteFileDataWriteFailure(t *testing.T) {
	const (
		dataOffset  = 1024
		clusterSize = 8
	)

	data := make([]byte, dataOffset+clusterSize*2)

	writer := &failingWriterAt{
		data:      data,
		failAfter: 1,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		writer,
	)

	fs.boot = BootSector{
		BytesPerSector:    512,
		SectorsPerCluster: 1,
		TotalSectors:      10,
	}

	fs.layout = Layout{
		DataOffset:      dataOffset,
		BytesPerCluster: clusterSize,
	}

	fileData := []byte("hello, world!")

	err := fs.writeFileData([]uint32{2, 3}, fileData)
	if err == nil {
		t.Fatal("writeFileData unexpectedly succeeded")
	}

	// The first cluster was successfully written before the
	// second cluster failed.
	wantCluster2 := []byte("hello, w")

	gotCluster2 := data[dataOffset : dataOffset+clusterSize]

	if !bytes.Equal(gotCluster2, wantCluster2) {
		t.Fatalf(
			"cluster 2 = %v, want %v",
			gotCluster2,
			wantCluster2,
		)
	}

	// The second cluster must remain untouched.
	gotCluster3 := data[dataOffset+clusterSize : dataOffset+2*clusterSize]

	if !bytes.Equal(gotCluster3, make([]byte, clusterSize)) {
		t.Fatalf(
			"cluster 3 = %v, want all zeroes",
			gotCluster3,
		)
	}
}

func TestClustersForSize(t *testing.T) {
	fs := New(bytes.NewReader(nil))

	fs.layout = Layout{
		BytesPerCluster: 4096,
	}

	tests := []struct {
		size int
		want int
	}{
		{size: 0, want: 0},
		{size: 1, want: 1},
		{size: 4095, want: 1},
		{size: 4096, want: 1},
		{size: 4097, want: 2},
		{size: 8192, want: 2},
		{size: 8193, want: 3},
	}

	for _, tt := range tests {
		got := fs.clustersForSize(tt.size)

		if got != tt.want {
			t.Errorf(
				"clustersForSize(%d) = %d, want %d",
				tt.size,
				got,
				tt.want,
			)
		}
	}
}

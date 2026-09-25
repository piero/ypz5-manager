package fat32

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"strings"
	"testing"
)

func TestReadFATEntry(t *testing.T) {
	const (
		sectorSize = 512
		fatOffset  = 512
	)

	data := make([]byte, fatOffset+sectorSize)

	binary.LittleEndian.PutUint32(
		data[fatOffset+2*4:fatOffset+2*4+4],
		3,
	)

	fs := New(bytes.NewReader(data))

	fs.boot = BootSector{
		BytesPerSector: sectorSize,
	}

	fs.layout = Layout{
		FATOffset: fatOffset,
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
	const (
		sectorSize = 512
		fatOffset  = 512
	)

	data := make([]byte, fatOffset+sectorSize)

	value := uint32(0xA0000003)

	binary.LittleEndian.PutUint32(
		data[fatOffset+2*4:fatOffset+2*4+4],
		value,
	)

	fs := New(bytes.NewReader(data))

	fs.boot = BootSector{
		BytesPerSector: sectorSize,
	}

	fs.layout = Layout{
		FATOffset: fatOffset,
	}

	got, err := fs.readFATEntry(2)
	if err != nil {
		t.Fatal(err)
	}

	if got != 3 {
		t.Fatalf("FAT entry = %#x, want %#x", got, uint32(3))
	}
}

func TestReadClusterChain(t *testing.T) {
	const (
		fatOffset  = 512
		sectorSize = 512
	)

	data := make([]byte, fatOffset+sectorSize)

	// 2 → 3 → 4 → 5 → EOF
	binary.LittleEndian.PutUint32(data[fatOffset+2*4:], 3)
	binary.LittleEndian.PutUint32(data[fatOffset+3*4:], 4)
	binary.LittleEndian.PutUint32(data[fatOffset+4*4:], 5)
	binary.LittleEndian.PutUint32(data[fatOffset+5*4:], fat32EOF)

	fs := New(bytes.NewReader(data))

	fs.boot = BootSector{
		BytesPerSector: sectorSize,
	}

	fs.layout = Layout{
		FATOffset: fatOffset,
	}

	got, err := fs.readClusterChain(2)
	if err != nil {
		t.Fatal(err)
	}

	want := []uint32{2, 3, 4, 5}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chain = %v, want %v", got, want)
	}
}

func TestReadClusterChainSingleCluster(t *testing.T) {
	const (
		fatOffset  = 512
		sectorSize = 512
	)

	data := make([]byte, fatOffset+sectorSize)

	binary.LittleEndian.PutUint32(
		data[fatOffset+2*4:],
		fat32EOF,
	)

	fs := New(bytes.NewReader(data))

	fs.boot = BootSector{
		BytesPerSector: sectorSize,
	}

	fs.layout = Layout{
		FATOffset: fatOffset,
	}

	got, err := fs.readClusterChain(2)
	if err != nil {
		t.Fatal(err)
	}

	want := []uint32{2}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chain = %v, want %v", got, want)
	}
}

func TestIsFreeCluster(t *testing.T) {
	const fatOffset = 512

	// Fixture
	//
	// 	offset 0
	// 	│
	// 	│  boot/test area
	// 	│
	// 	├── 512  ← FAT starts
	// 	│
	// 	│   ┌──────────────────────────────┐
	// 	│   │ FAT sector (512 bytes)       │
	// 	│   │                              │
	// 	│   │ FAT[2] = EOF                 │
	// 	│   │ FAT[3] = 0 (free)            │
	// 	│   │                              │
	// 	│   └──────────────────────────────┘
	// 	│
	// 	└── 1024
	data := make([]byte, fatOffset+512)

	// Cluster 2 is allocated.
	binary.LittleEndian.PutUint32(
		data[fatOffset+2*4:],
		fat32EOF,
	)

	// Cluster 3 is free: leave FAT[3] as zero.
	fs := New(bytes.NewReader(data))
	fs.boot = BootSector{
		BytesPerSector: 512,
	}
	fs.layout = Layout{
		FATOffset: fatOffset,
	}

	free, err := fs.IsFreeCluster(2)
	if err != nil {
		t.Fatal(err)
	}

	if free {
		t.Fatal("cluster 2 reported as free")
	}

	free, err = fs.IsFreeCluster(3)
	if err != nil {
		t.Fatal(err)
	}

	if !free {
		t.Fatal("cluster 3 reported as allocated")
	}
}

func TestFindFreeCluster(t *testing.T) {
	const fatOffset = 512

	data := make([]byte, fatOffset+512)

	// Cluster 2 is allocated.
	binary.LittleEndian.PutUint32(
		data[fatOffset+2*4:],
		fat32EOF,
	)

	// Cluster 3 is free.

	fs := New(bytes.NewReader(data))
	fs.boot = BootSector{
		BytesPerSector:    512,
		SectorsPerCluster: 1,
		ReservedSectors:   1,
		NumberOfFATs:      1,
		SectorsPerFAT:     1,
		TotalSectors:      5,
	}
	fs.layout = Layout{
		FATOffset: fatOffset,
	}

	cluster, err := fs.FindFreeCluster(2)
	if err != nil {
		t.Fatal(err)
	}

	if cluster != 3 {
		t.Fatalf("cluster = %d, want 3", cluster)
	}
}

func TestClusterChain(t *testing.T) {
	const (
		fatOffset  = 512
		sectorSize = 512
	)

	data := make([]byte, fatOffset+sectorSize)

	binary.LittleEndian.PutUint32(
		data[fatOffset+2*4:],
		3,
	)

	binary.LittleEndian.PutUint32(
		data[fatOffset+3*4:],
		4,
	)

	binary.LittleEndian.PutUint32(
		data[fatOffset+4*4:],
		fat32EOF,
	)

	fs := New(bytes.NewReader(data))

	fs.boot = BootSector{
		BytesPerSector: sectorSize,
	}

	fs.layout = Layout{
		FATOffset: fatOffset,
	}

	got, err := fs.ClusterChain(2)
	if err != nil {
		t.Fatal(err)
	}

	want := []uint32{2, 3, 4}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chain = %v, want %v", got, want)
	}
}

func TestWriteFATEntry(t *testing.T) {
	data := make([]byte, 10000)

	boot := BootSector{
		BytesPerSector:    512,
		SectorsPerCluster: 8,
		ReservedSectors:   1,
		NumberOfFATs:      2,
		SectorsPerFAT:     2,
		TotalSectors:      100,
		RootCluster:       2,
	}

	layout := boot.Layout()

	binary.LittleEndian.PutUint32(
		data[layout.FATOffset+3*4:],
		0xF0000005,
	)

	secondFAT := layout.FATOffset +
		int64(boot.SectorsPerFAT)*int64(boot.BytesPerSector)

	binary.LittleEndian.PutUint32(
		data[secondFAT+3*4:],
		0xF0000005,
	)

	fs := NewReadWrite(
		bytes.NewReader(data),
		writerAtBuffer{data: data},
	)
	fs.boot = boot
	fs.layout = layout

	if err := fs.writeFATEntry(3, 0x12345678); err != nil {
		t.Fatal(err)
	}

	got, err := fs.readFATEntry(3)
	if err != nil {
		t.Fatal(err)
	}

	if got != 0x02345678 {
		t.Fatalf("got FAT value %#x, want %#x", got, uint32(0x02345678))
	}

	gotSecond := binary.LittleEndian.Uint32(
		data[secondFAT+3*4:],
	)

	if gotSecond != 0xF2345678 {
		t.Fatalf("second FAT = %#x, want %#x", gotSecond, uint32(0xF2345678))
	}
}

func TestFreeClusterChain(t *testing.T) {
	data := make([]byte, 4096)

	boot := BootSector{
		BytesPerSector:    512,
		SectorsPerCluster: 8,
		ReservedSectors:   1,
		NumberOfFATs:      2,
		SectorsPerFAT:     2,
		TotalSectors:      100,
		RootCluster:       2,
	}

	layout := boot.Layout()

	// FAT chain:
	//
	// 2 -> 3 -> 4 -> EOF
	//
	// Cluster 5 is allocated but is not part of the chain.
	fatValues := map[uint32]uint32{
		2: 3,
		3: 4,
		4: fat32EOF,
		5: fat32EOF,
	}

	for cluster, value := range fatValues {
		offset := layout.FATOffset + int64(cluster)*4
		binary.LittleEndian.PutUint32(
			data[offset:offset+4],
			value,
		)

		secondFAT := layout.FATOffset +
			int64(boot.SectorsPerFAT)*int64(boot.BytesPerSector)

		offset = secondFAT + int64(cluster)*4
		binary.LittleEndian.PutUint32(
			data[offset:offset+4],
			value,
		)
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		writerAtBuffer{data: data},
	)
	fs.boot = boot
	fs.layout = layout

	if err := fs.freeClusterChain(2); err != nil {
		t.Fatal(err)
	}

	// Clusters in the chain should now be free.
	for _, cluster := range []uint32{2, 3, 4} {
		free, err := fs.IsFreeCluster(cluster)
		if err != nil {
			t.Fatal(err)
		}

		if !free {
			t.Fatalf("cluster %d is still allocated", cluster)
		}
	}

	// An unrelated allocated cluster must remain allocated.
	free, err := fs.IsFreeCluster(5)
	if err != nil {
		t.Fatal(err)
	}

	if free {
		t.Fatal("cluster 5 was unexpectedly freed")
	}

	// Verify that the second FAT copy was also updated.
	secondFAT := layout.FATOffset +
		int64(boot.SectorsPerFAT)*int64(boot.BytesPerSector)

	for _, cluster := range []uint32{2, 3, 4} {
		offset := secondFAT + int64(cluster)*4

		value := binary.LittleEndian.Uint32(
			data[offset : offset+4],
		)

		if value != 0 {
			t.Fatalf(
				"second FAT cluster %d = %#x, want 0",
				cluster,
				value,
			)
		}
	}
}

func TestWriteFATEntryShortWrite(t *testing.T) {
	const (
		sectorSize  = 512
		fatOffset   = 0
		entryOffset = 300
	)

	data := make([]byte, 2048)

	binary.LittleEndian.PutUint32(
		data[entryOffset:entryOffset+4],
		0xF0000000,
	)

	writer := &failingWriterAt{
		data:         data,
		failAfter:    1,
		partialWrite: true,
		failOnce:     true,
	}

	fs := NewReadWrite(bytes.NewReader(data), writer)

	fs.boot = BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: 1,
		ReservedSectors:   1,
		NumberOfFATs:      2,
		SectorsPerFAT:     1,
		TotalSectors:      100,
	}

	fs.layout = Layout{
		FATOffset: fatOffset,
	}

	secondFAT := int64(fs.boot.SectorsPerFAT) *
		int64(fs.boot.BytesPerSector)

	binary.LittleEndian.PutUint32(
		data[secondFAT+entryOffset:secondFAT+entryOffset+4],
		0xF0000000,
	)

	err := fs.writeFATEntry(75, 0x12345678)
	if err == nil {
		t.Fatal("writeFATEntry unexpectedly succeeded")
	}

	first := binary.LittleEndian.Uint32(
		data[entryOffset : entryOffset+4],
	)

	second := binary.LittleEndian.Uint32(
		data[secondFAT+entryOffset : secondFAT+entryOffset+4],
	)

	if first != 0xF0000000 {
		t.Fatalf(
			"first FAT entry: got %#08x, want %#08x",
			first,
			0xF0000000,
		)
	}

	if second != 0xF0000000 {
		t.Fatalf(
			"second FAT entry: got %#08x, want %#08x",
			second,
			0xF0000000,
		)
	}
}

func TestWriteFATEntryRollbackFailure(t *testing.T) {
	const (
		sectorSize  = 512
		fatOffset   = 0
		entryOffset = 300
	)

	data := make([]byte, 2048)

	binary.LittleEndian.PutUint32(
		data[entryOffset:entryOffset+4],
		0xF0000000,
	)

	secondFAT := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[secondFAT+entryOffset:secondFAT+entryOffset+4],
		0xF0000000,
	)

	writer := &failingWriterAt{
		data:         data,
		failAfter:    1,
		partialWrite: true,
	}

	fs := NewReadWrite(bytes.NewReader(data), writer)

	fs.boot = BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: 1,
		ReservedSectors:   1,
		NumberOfFATs:      2,
		SectorsPerFAT:     1,
		TotalSectors:      100,
	}

	fs.layout = Layout{
		FATOffset: fatOffset,
	}

	err := fs.writeFATEntry(75, 0x12345678)
	if err == nil {
		t.Fatal("writeFATEntry unexpectedly succeeded")
	}

	first := binary.LittleEndian.Uint32(
		data[entryOffset : entryOffset+4],
	)

	second := binary.LittleEndian.Uint32(
		data[secondFAT+entryOffset : secondFAT+entryOffset+4],
	)

	if first != 0xF2345678 {
		t.Fatalf(
			"first FAT entry: got %#08x, want %#08x",
			first,
			0xF2345678,
		)
	}

	if second != 0xF0000000 {
		t.Fatalf(
			"second FAT entry: got %#08x, want %#08x",
			second,
			0xF0000000,
		)
	}

	if !strings.Contains(err.Error(), "rollback failed") {
		t.Fatalf("error = %q, want rollback failure", err)
	}
}

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

func TestAllocateCluster(t *testing.T) {
	const fatOffset = 512

	data := make([]byte, fatOffset+512)

	// Cluster 2 is allocated.
	binary.LittleEndian.PutUint32(
		data[fatOffset+2*4:],
		fat32EOF,
	)

	// Cluster 3 is free.

	fs := New(bytes.NewReader(data))
	fs.writer = writerAtBuffer{data: data}
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

	cluster, err := fs.allocateCluster()
	if err != nil {
		t.Fatal(err)
	}

	if cluster != 3 {
		t.Fatalf("cluster = %d, want 3", cluster)
	}

	value := binary.LittleEndian.Uint32(
		data[fatOffset+3*4:],
	)

	if value&0x0FFFFFFF != fat32EOF {
		t.Fatalf(
			"FAT[3] = 0x%08X, want EOF",
			value,
		)
	}
}

func TestAllocateClusterWriteFailure(t *testing.T) {
	const fatOffset = 512

	data := make([]byte, fatOffset+512)

	// Cluster 2 is allocated.
	binary.LittleEndian.PutUint32(
		data[fatOffset+2*4:],
		fat32EOF,
	)

	writer := &failingWriterAt{
		data:      data,
		failAfter: 0,
	}

	fs := New(bytes.NewReader(data))
	fs.writer = writer
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

	cluster, err := fs.allocateCluster()
	if err == nil {
		t.Fatal("allocateCluster unexpectedly succeeded")
	}

	if cluster != 0 {
		t.Fatalf("cluster = %d, want 0", cluster)
	}

	value := binary.LittleEndian.Uint32(
		data[fatOffset+3*4:],
	)

	if value&0x0FFFFFFF != 0 {
		t.Fatalf(
			"FAT[3] = 0x%08X, want free",
			value,
		)
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

func TestAllocateClusterChainInvalidCount(t *testing.T) {
	fs := New(bytes.NewReader(nil))

	fs.boot = BootSector{
		BytesPerSector: 512,
	}

	_, err := fs.allocateClusterChain(0)
	if err == nil {
		t.Fatal("allocateClusterChain unexpectedly succeeded")
	}
}

func TestAllocateClusterChain(t *testing.T) {
	const (
		fatOffset  = 512
		sectorSize = 512
	)

	// add more space to the fixture, so we can test allocation
	data := make([]byte, fatOffset+sectorSize*4)

	// Cluster 2 is allocated.
	binary.LittleEndian.PutUint32(
		data[fatOffset+2*4:],
		fat32EOF,
	)

	fs := NewReadWrite(
		bytes.NewReader(data),
		writerAtBuffer{data: data},
	)

	fs.boot = BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: 1,
		ReservedSectors:   1,
		NumberOfFATs:      1,
		SectorsPerFAT:     1,
		TotalSectors:      10, // add more sectors to the fixture, so we can test allocation
	}

	fs.layout = Layout{
		FATOffset: fatOffset,
	}

	got, err := fs.allocateClusterChain(3)
	if err != nil {
		t.Fatal(err)
	}

	want := []uint32{3, 4, 5}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chain = %v, want %v", got, want)
	}

	chain, err := fs.ClusterChain(3)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(chain, want) {
		t.Fatalf("FAT chain = %v, want %v", chain, want)
	}
}

func TestAllocateClusterChainLinkFailure(t *testing.T) {
	const (
		fatOffset  = 512
		sectorSize = 512
	)

	data := make([]byte, fatOffset+sectorSize*4)

	// Cluster 2 is allocated.
	binary.LittleEndian.PutUint32(
		data[fatOffset+2*4:],
		fat32EOF,
	)

	// Fail during the first link operation.
	//
	// allocateCluster() writes one FAT sector for each allocated
	// cluster, so three successful allocations consume three writes.
	writer := &failingWriterAt{
		data:      data,
		failAfter: 3,
		failOnce:  true,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		writer,
	)

	fs.boot = BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: 1,
		ReservedSectors:   1,
		NumberOfFATs:      1,
		SectorsPerFAT:     1,
		TotalSectors:      10,
	}

	fs.layout = Layout{
		FATOffset: fatOffset,
	}

	_, err := fs.allocateClusterChain(3)
	if err == nil {
		t.Fatal("allocateClusterChain unexpectedly succeeded")
	}

	// The three clusters were allocated before the link failed.
	//
	// Expected sequence:
	//
	// - allocate 3  ✓
	// - allocate 4  ✓
	// - allocate 5  ✓
	// - link 3 → 4  ✗  ← injected failure
	// - rollback 3  ✓
	// - rollback 4  ✓
	// - rollback 5  ✓
	for _, cluster := range []uint32{3, 4, 5} {
		free, err := fs.IsFreeCluster(cluster)
		if err != nil {
			t.Fatal(err)
		}

		if !free {
			t.Fatalf(
				"cluster %d is allocated after rollback; want free",
				cluster,
			)
		}
	}
}

func TestAllocateClusterChainAllocationFailure(t *testing.T) {
	const (
		fatOffset  = 512
		sectorSize = 512
	)

	data := make([]byte, fatOffset+sectorSize*4)

	// Cluster 2 is allocated.
	binary.LittleEndian.PutUint32(
		data[fatOffset+2*4:],
		fat32EOF,
	)

	writer := &failingWriterAt{
		data:      data,
		failAfter: 2,
		failOnce:  true,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		writer,
	)

	fs.boot = BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: 1,
		ReservedSectors:   1,
		NumberOfFATs:      1,
		SectorsPerFAT:     1,
		TotalSectors:      10,
	}

	fs.layout = Layout{
		FATOffset: fatOffset,
	}

	_, err := fs.allocateClusterChain(3)
	if err == nil {
		t.Fatal("allocateClusterChain unexpectedly succeeded")
	}

	// Clusters 3 and 4 were allocated before the failure and
	// must have been rolled back.
	for _, cluster := range []uint32{3, 4} {
		free, err := fs.IsFreeCluster(cluster)
		if err != nil {
			t.Fatal(err)
		}

		if !free {
			t.Fatalf(
				"cluster %d is allocated after rollback; want free",
				cluster,
			)
		}
	}

	// Cluster 5 was never successfully allocated.
	free, err := fs.IsFreeCluster(5)
	if err != nil {
		t.Fatal(err)
	}

	if !free {
		t.Fatal("cluster 5 is allocated; want free")
	}
}

func TestAllocateClusterChainRollbackFailure(t *testing.T) {
	const (
		fatOffset  = 512
		sectorSize = 512
	)

	data := make([]byte, fatOffset+sectorSize*4)

	binary.LittleEndian.PutUint32(
		data[fatOffset+2*4:],
		fat32EOF,
	)

	// Two allocations succeed.
	// The third allocation fails.
	// Rollback then fails as well.
	writer := &failingWriterAt{
		data:      data,
		failAfter: 2,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		writer,
	)

	fs.boot = BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: 1,
		ReservedSectors:   1,
		NumberOfFATs:      1,
		SectorsPerFAT:     1,
		TotalSectors:      10,
	}

	fs.layout = Layout{
		FATOffset: fatOffset,
	}

	_, err := fs.allocateClusterChain(3)
	if err == nil {
		t.Fatal("allocateClusterChain unexpectedly succeeded")
	}

	if !strings.Contains(err.Error(), "rollback cluster") {
		t.Fatalf(
			"error = %q, want rollback error",
			err,
		)
	}

	// Rollback failed, so at least one allocated cluster remains.
	free, err := fs.IsFreeCluster(3)
	if err != nil {
		t.Fatal(err)
	}

	if free {
		t.Fatal("cluster 3 unexpectedly free after rollback failure")
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

func TestFreeClusterChainWriteFailure(t *testing.T) {
	data := make([]byte, 4096)

	boot := BootSector{
		BytesPerSector:    512,
		SectorsPerCluster: 1,
		ReservedSectors:   1,
		NumberOfFATs:      2,
		SectorsPerFAT:     2,
		TotalSectors:      100,
		RootCluster:       2,
	}

	layout := boot.Layout()

	// Chain: 3 -> 4 -> 5 -> EOF.
	for fat := uint8(0); fat < boot.NumberOfFATs; fat++ {
		fatOffset := layout.FATOffset +
			int64(fat)*int64(boot.SectorsPerFAT)*int64(boot.BytesPerSector)

		binary.LittleEndian.PutUint32(
			data[fatOffset+3*4:fatOffset+3*4+4],
			4,
		)

		binary.LittleEndian.PutUint32(
			data[fatOffset+4*4:fatOffset+4*4+4],
			5,
		)

		binary.LittleEndian.PutUint32(
			data[fatOffset+5*4:fatOffset+5*4+4],
			fat32EOF,
		)
	}

	writer := &failingWriterAt{
		data:      data,
		failAfter: 2,
	}

	fs := NewReadWrite(bytes.NewReader(data), writer)
	fs.boot = boot
	fs.layout = layout

	err := fs.freeClusterChain(3)
	if err == nil {
		t.Fatal("freeClusterChain unexpectedly succeeded")
	}

	t.Logf("freeClusterChain failed as expected: %v", err)

	for fat := uint8(0); fat < boot.NumberOfFATs; fat++ {
		fatOffset := layout.FATOffset +
			int64(fat)*int64(boot.SectorsPerFAT)*int64(boot.BytesPerSector)

		cluster3 := binary.LittleEndian.Uint32(
			data[fatOffset+3*4 : fatOffset+3*4+4],
		)

		cluster4 := binary.LittleEndian.Uint32(
			data[fatOffset+4*4 : fatOffset+4*4+4],
		)

		t.Logf(
			"FAT %d: cluster 3 = %#08x, cluster 4 = %#08x",
			fat+1,
			cluster3,
			cluster4,
		)
	}
}

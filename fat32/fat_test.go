package fat32

import (
	"bytes"
	"encoding/binary"
	"reflect"
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

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector: sectorSize,
		},
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

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector: sectorSize,
		},
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

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector: sectorSize,
		},
		layout: Layout{
			FATOffset: fatOffset,
		},
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

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector: sectorSize,
		},
		layout: Layout{
			FATOffset: fatOffset,
		},
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

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector: 512,
		},
		layout: Layout{
			FATOffset: fatOffset,
		},
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

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector:    512,
			SectorsPerCluster: 1,
			ReservedSectors:   1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      5,
		},
		layout: Layout{
			FATOffset: fatOffset,
		},
	}

	cluster, err := fs.FindFreeCluster()
	if err != nil {
		t.Fatal(err)
	}

	if cluster != 3 {
		t.Fatalf("cluster = %d, want 3", cluster)
	}
}

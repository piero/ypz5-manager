package fat32

import (
	"bytes"
	"testing"
)

func TestParseDirectoryEntry(t *testing.T) {
	data := []byte{
		0x50, 0x49, 0x43, 0x54, 0x55, 0x52, 0x45, 0x53,
		0x20, 0x20, 0x20,
		0x10,
		0x00,
		0xbc,
		0x58, 0x55,
		0x22, 0x36,
		0x22, 0x36,
		0x00, 0x00,
		0x59, 0x55,
		0x22, 0x36,
		0x03, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}

	got, err := parseDirectoryEntry(data)
	if err != nil {
		t.Fatal(err)
	}

	if got.ShortName != "PICTURES" {
		t.Errorf("ShortName = %q, want %q", got.ShortName, "PICTURES")
	}

	if !got.IsDir {
		t.Error("IsDir = false, want true")
	}

	if got.StartCluster != 3 {
		t.Errorf("StartCluster = %d, want 3", got.StartCluster)
	}

	if got.Size != 0 {
		t.Errorf("Size = %d, want 0", got.Size)
	}
}

func TestParseLFNEntry(t *testing.T) {
	data := []byte{
		0x41,
		0x50, 0x00,
		0x69, 0x00,
		0x63, 0x00,
		0x74, 0x00,
		0x75, 0x00,
		0x0F,
		0x00,
		0x59,
		0x72, 0x00,
		0x65, 0x00,
		0x73, 0x00,
		0x00, 0x00,
		0xFF, 0xFF,
		0xFF, 0xFF,
		0x00, 0x00,
		0xFF, 0xFF,
		0xFF, 0xFF,
	}

	got, err := parseLFNEntry(data)
	if err != nil {
		t.Fatal(err)
	}

	if got.Sequence != 1 {
		t.Errorf("Sequence = %d, want 1", got.Sequence)
	}

	if !got.IsLast {
		t.Error("IsLast = false, want true")
	}

	if got.Name != "Pictures" {
		t.Errorf("Name = %q, want %q", got.Name, "Pictures")
	}
}

func TestCombineLFN(t *testing.T) {
	entries := []lfnEntry{
		{
			Sequence: 2,
			Name:     "Beast",
		},
		{
			Sequence: 1,
			IsLast:   true,
			Name:     "The Number Of The ",
		},
	}

	got := combineLFN(entries)

	want := "The Number Of The Beast"

	if got != want {
		t.Errorf("Name = %q, want %q", got, want)
	}
}

func TestFindRoot(t *testing.T) {
	// We'll add the actual filesystem fixture here later.
}

func TestCombineLFNMultipleEntries(t *testing.T) {
	entries := []lfnEntry{
		{
			Sequence: 3,
			Name:     "Beast",
		},
		{
			Sequence: 2,
			Name:     "Number Of The ",
		},
		{
			Sequence: 1,
			IsLast:   true,
			Name:     "The ",
		},
	}

	got := combineLFN(entries)

	want := "The Number Of The Beast"

	if got != want {
		t.Fatalf("Name = %q, want %q", got, want)
	}
}

func TestCombineLFNUsesSequenceOrder(t *testing.T) {
	entries := []lfnEntry{
		{
			Sequence: 2,
			Name:     "Beast",
		},
		{
			Sequence: 1,
			IsLast:   true,
			Name:     "The Number Of The ",
		},
	}

	got := combineLFN(entries)

	want := "The Number Of The Beast"

	if got != want {
		t.Fatalf("Name = %q, want %q", got, want)
	}
}

func TestDeleteDirectoryRecord(t *testing.T) {
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

	// Directory cluster 2 starts at DataOffset.
	//
	//  offset  0: unrelated entry
	//  offset 32: LFN entry
	//  offset 64: short entry
	//  offset 96: unrelated entry
	directoryOffset := layout.DataOffset

	data[directoryOffset+0] = 0x41
	data[directoryOffset+32] = 0x41
	data[directoryOffset+64] = 0x50
	data[directoryOffset+96] = 0x42

	fs := NewReadWrite(
		bytes.NewReader(data),
		writerAtBuffer{data: data},
	)
	fs.boot = boot
	fs.layout = layout

	record := directoryRecord{
		Entry: DirectoryEntry{
			Name:         "Example Long Name.txt",
			IsDir:        false,
			Size:         123,
			StartCluster: 10,
		},
		Short: directoryEntryLocation{
			Cluster: 2,
			Offset:  64,
		},
		LFN: []directoryEntryLocation{
			{
				Cluster: 2,
				Offset:  32,
			},
		},
	}

	if err := fs.deleteDirectoryRecord(record); err != nil {
		t.Fatal(err)
	}

	// The LFN and short entries should be marked deleted.
	if got := data[directoryOffset+32]; got != 0xE5 {
		t.Fatalf("LFN entry first byte = %#x, want %#x", got, byte(0xE5))
	}

	if got := data[directoryOffset+64]; got != 0xE5 {
		t.Fatalf("short entry first byte = %#x, want %#x", got, byte(0xE5))
	}

	// Unrelated entries must remain untouched.
	if got := data[directoryOffset+0]; got != 0x41 {
		t.Fatalf("unrelated entry 0 first byte = %#x, want %#x", got, byte(0x41))
	}

	if got := data[directoryOffset+96]; got != 0x42 {
		t.Fatalf("unrelated entry 96 first byte = %#x, want %#x", got, byte(0x42))
	}
}

func TestMarkDirectoryEntryDeletedPreservesSector(t *testing.T) {
	const (
		dataOffset      = 1024
		cluster         = 2
		entryOffset     = 32
		sectorSize      = 512
		bytesPerCluster = 512
	)

	data := make([]byte, 2048)

	// Fill the directory sector with recognizable data.
	for i := dataOffset; i < dataOffset+sectorSize; i++ {
		data[i] = byte(i & 0xff)
	}

	original := append([]byte(nil), data...)

	fs := NewReadWrite(
		bytes.NewReader(data),
		&writerAtBuffer{data: data},
	)

	fs.boot = BootSector{
		BytesPerSector: sectorSize,
	}

	fs.layout = Layout{
		DataOffset:      dataOffset,
		BytesPerCluster: bytesPerCluster,
	}

	err := fs.markDirectoryEntryDeleted(directoryEntryLocation{
		Cluster: cluster,
		Offset:  entryOffset,
	})
	if err != nil {
		t.Fatal(err)
	}

	// The target byte should now be 0xE5.
	target := data[dataOffset+entryOffset]
	if target != 0xE5 {
		t.Fatalf("target byte = %#x, want %#x", target, byte(0xE5))
	}

	// Everything else in the sector must be unchanged.
	for i := 0; i < sectorSize; i++ {
		if i == entryOffset {
			continue
		}

		if data[dataOffset+i] != original[dataOffset+i] {
			t.Fatalf(
				"byte at sector offset %d changed: got %#x, want %#x",
				i,
				data[dataOffset+i],
				original[dataOffset+i],
			)
		}
	}
}

func TestMarkDirectoryEntryDeletedShortWrite(t *testing.T) {
	const (
		sectorSize      = 512
		dataOffset      = 1024
		bytesPerCluster = 512
	)

	data := make([]byte, 2048)

	// Make the target directory entry look occupied.
	data[dataOffset] = 'T'

	writer := &failingWriterAt{
		data:         data,
		failAfter:    0,
		partialWrite: true,
	}

	fs := NewReadWrite(bytes.NewReader(data), writer)

	fs.boot = BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: 1,
		ReservedSectors:   1,
		NumberOfFATs:      1,
		SectorsPerFAT:     1,
		TotalSectors:      16,
		RootCluster:       2,
	}

	fs.layout = Layout{
		DataOffset:      dataOffset,
		BytesPerCluster: bytesPerCluster,
	}

	err := fs.markDirectoryEntryDeleted(directoryEntryLocation{
		Cluster: 2,
		Offset:  0,
	})
	if err == nil {
		t.Fatal("markDirectoryEntryDeleted unexpectedly succeeded")
	}
}

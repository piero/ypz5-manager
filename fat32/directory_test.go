package fat32

import (
	"bytes"
	"encoding/binary"
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

func TestEncodeShortName(t *testing.T) {
	got, err := encodeShortName("TEST.TXT")
	if err != nil {
		t.Fatal(err)
	}

	want := [11]byte{
		'T', 'E', 'S', 'T', ' ', ' ', ' ', ' ',
		'T', 'X', 'T',
	}

	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	tests := []struct {
		name    string
		wantErr bool
	}{
		{name: "TEST.TXT"},
		{name: "README"},
		{name: "A.B"},
		{name: "TOOLONGNAME.TXT", wantErr: true},
		{name: "TEST.TOOLONG", wantErr: true},
		{name: "BAD NAME.TXT", wantErr: true},
		{name: "BAD/NAME.TXT", wantErr: true},
		{name: ".TXT", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := encodeShortName(tt.name)

			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}

			if !tt.wantErr && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEncodeDirectoryEntry(t *testing.T) {
	shortName, err := encodeShortName("TEST.TXT")
	if err != nil {
		t.Fatal(err)
	}

	data := encodeDirectoryEntry(
		shortName,
		0x20, // archive
		1234,
		5678,
	)

	entry, err := parseDirectoryEntry(data[:])
	if err != nil {
		t.Fatal(err)
	}

	if entry.ShortName != "TEST.TXT" {
		t.Fatalf("short name = %q, want %q", entry.ShortName, "TEST.TXT")
	}

	if entry.IsDir {
		t.Fatal("entry unexpectedly marked as directory")
	}

	if entry.StartCluster != 1234 {
		t.Fatalf("start cluster = %d, want %d", entry.StartCluster, 1234)
	}

	if entry.Size != 5678 {
		t.Fatalf("size = %d, want %d", entry.Size, 5678)
	}
}

func TestEncodeLFNEntry(t *testing.T) {
	data, err := encodeLFNEntry(0x41, "Hello", 0xAB)
	if err != nil {
		t.Fatal(err)
	}

	entry, err := parseLFNEntry(data[:])
	if err != nil {
		t.Fatal(err)
	}

	if entry.Sequence != 1 {
		t.Fatalf("sequence = %d, want %d", entry.Sequence, 1)
	}

	if !entry.IsLast {
		t.Fatal("entry should be marked as last")
	}

	if entry.Checksum != 0xAB {
		t.Fatalf("checksum = %#02x, want %#02x", entry.Checksum, 0xAB)
	}

	if entry.Name != "Hello" {
		t.Fatalf("name = %q, want %q", entry.Name, "Hello")
	}
}

func TestEncodeLFNEntries(t *testing.T) {
	name := "This is a long filename"

	entries, err := encodeLFNEntries(name, 0xAB)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	first, err := parseLFNEntry(entries[0][:])
	if err != nil {
		t.Fatal(err)
	}

	second, err := parseLFNEntry(entries[1][:])
	if err != nil {
		t.Fatal(err)
	}

	if first.Sequence != 2 || !first.IsLast {
		t.Fatalf(
			"first entry: sequence=%d isLast=%v, want sequence=2 isLast=true",
			first.Sequence,
			first.IsLast,
		)
	}

	if second.Sequence != 1 || second.IsLast {
		t.Fatalf(
			"second entry: sequence=%d isLast=%v, want sequence=1 isLast=false",
			second.Sequence,
			second.IsLast,
		)
	}

	if first.Checksum != 0xAB || second.Checksum != 0xAB {
		t.Fatal("unexpected LFN checksum")
	}

	if got := combineLFN([]lfnEntry{first, second}); got != name {
		t.Fatalf("combined name = %q, want %q", got, name)
	}
}

func TestEncodeDirectoryRecord(t *testing.T) {
	record, err := encodeDirectoryRecord(
		"TEST.TXT",
		1234,
		5678,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(record.LFN) != 0 {
		t.Fatalf("LFN entries = %d, want 0", len(record.LFN))
	}

	short, err := parseDirectoryEntry(record.Short[:])
	if err != nil {
		t.Fatal(err)
	}

	if short.ShortName != "TEST.TXT" {
		t.Fatalf("short name = %q, want %q", short.ShortName, "TEST.TXT")
	}

	if short.IsDir {
		t.Fatal("entry unexpectedly marked as directory")
	}

	if short.StartCluster != 1234 {
		t.Fatalf("start cluster = %d, want %d", short.StartCluster, 1234)
	}

	if short.Size != 5678 {
		t.Fatalf("size = %d, want %d", short.Size, 5678)
	}
}

func TestIsShortName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"TEST.TXT", true},
		{"test.txt", true},
		{"README", true},
		{"hello.txt", true},
		{"This is a long filename.txt", false},
		{"TOOLONGNAME.TXT", false},
		{"TEST.TOOLONG", false},
		{"BAD NAME.TXT", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isShortName(tt.name); got != tt.want {
				t.Fatalf("isShortName(%q) = %v, want %v",
					tt.name, got, tt.want)
			}
		})
	}
}

func TestGenerateShortName(t *testing.T) {
	tests := []struct {
		name string
		n    int
		want string
	}{
		{
			name: "This is a long filename.txt",
			n:    1,
			want: "THISIS~1.TXT",
		},
		{
			name: "Cartoni animati",
			n:    1,
			want: "CARTON~1",
		},
		{
			name: "Some file.mp3",
			n:    2,
			want: "SOMEFI~2.MP3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := generateShortName(tt.name, tt.n)
			if err != nil {
				t.Fatal(err)
			}

			if gotName := parseShortName(got[:]); gotName != tt.want {
				t.Fatalf(
					"short name = %q, want %q",
					gotName,
					tt.want,
				)
			}
		})
	}
}

func TestEncodeDirectoryRecordLongName(t *testing.T) {
	name := "This is a long filename.txt"

	record, err := encodeDirectoryRecord(
		name,
		1234,
		5678,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(record.LFN) != 3 {
		t.Fatalf("LFN entries = %d, want 3", len(record.LFN))
	}

	short, err := parseDirectoryEntry(record.Short[:])
	if err != nil {
		t.Fatal(err)
	}

	if short.ShortName != "THISIS~1.TXT" {
		t.Fatalf(
			"short name = %q, want %q",
			short.ShortName,
			"THISIS~1.TXT",
		)
	}

	if short.StartCluster != 1234 {
		t.Fatalf("start cluster = %d, want %d", short.StartCluster, 1234)
	}

	if short.Size != 5678 {
		t.Fatalf("size = %d, want %d", short.Size, 5678)
	}

	lfn := make([]lfnEntry, 0, len(record.LFN))

	for _, data := range record.LFN {
		entry, err := parseLFNEntry(data[:])
		if err != nil {
			t.Fatal(err)
		}

		lfn = append(lfn, entry)
	}

	if got := combineLFN(lfn); got != name {
		t.Fatalf("LFN name = %q, want %q", got, name)
	}

	checksum := shortNameChecksum(record.Short[:])

	for _, entry := range lfn {
		if entry.Checksum != checksum {
			t.Fatalf(
				"LFN checksum = %#02x, want %#02x",
				entry.Checksum,
				checksum,
			)
		}
	}
}

func TestFindFreeDirectorySlots(t *testing.T) {
	const (
		sectorSize       = 512
		bytesPerCluster  = 512
		fatOffset        = 0
		dataOffset       = 512
		directoryCluster = uint32(2)
	)

	data := make([]byte, dataOffset+bytesPerCluster)

	// FAT entry for cluster 2: end of chain.
	binary.LittleEndian.PutUint32(
		data[fatOffset+directoryCluster*4:],
		fat32EOF,
	)

	// Directory entries:
	//
	// slot 0: live
	// slot 1: deleted/free
	// slot 2: deleted/free
	// slot 3: live
	// slot 4: end-of-directory/free
	data[dataOffset] = 'A'
	data[dataOffset+32] = 0xE5
	data[dataOffset+64] = 0xE5
	data[dataOffset+96] = 'B'
	data[dataOffset+128] = 0x00

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      2,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	slots, err := fs.findFreeDirectorySlots(directoryCluster, 2)
	if err != nil {
		t.Fatal(err)
	}

	if len(slots) != 2 {
		t.Fatalf("got %d slots, want 2", len(slots))
	}

	if slots[0].Cluster != directoryCluster ||
		slots[0].Offset != 32 {
		t.Fatalf(
			"first slot = %+v, want cluster=%d offset=32",
			slots[0],
			directoryCluster,
		)
	}

	if slots[1].Cluster != directoryCluster ||
		slots[1].Offset != 64 {
		t.Fatalf(
			"second slot = %+v, want cluster=%d offset=64",
			slots[1],
			directoryCluster,
		)
	}
}

func TestFindFreeDirectorySlotsRequiresContiguousSlots(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		cluster         = uint32(2)
	)

	data := make([]byte, dataOffset+bytesPerCluster)

	binary.LittleEndian.PutUint32(
		data[fatOffset+cluster*4:],
		fat32EOF,
	)

	// slot 0: live
	// slot 1: free
	// slot 2: live
	// slot 3: free
	// all remaining slots: live
	data[dataOffset] = 'A'
	data[dataOffset+32] = 0xE5
	data[dataOffset+64] = 'B'
	data[dataOffset+96] = 0xE5

	for offset := 128; offset < bytesPerCluster; offset += 32 {
		data[dataOffset+offset] = 'C'
	}

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      2,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	_, err := fs.findFreeDirectorySlots(cluster, 2)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestWriteDirectoryEntry(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		cluster         = uint32(2)
	)

	data := make([]byte, dataOffset+bytesPerCluster)

	binary.LittleEndian.PutUint32(
		data[fatOffset+cluster*4:],
		fat32EOF,
	)

	writer := writerAtBuffer{
		data: append([]byte(nil), data...),
	}

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: writer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      2,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	var entry [32]byte
	entry[0] = 'T'
	entry[1] = 'E'
	entry[2] = 'S'
	entry[3] = 'T'

	location := directoryEntryLocation{
		Cluster: cluster,
		Offset:  32,
	}

	if err := fs.writeDirectoryEntry(location, entry); err != nil {
		t.Fatal(err)
	}

	got := writer.data[dataOffset+32 : dataOffset+64]

	if !bytes.Equal(got, entry[:]) {
		t.Fatalf("directory entry = %x, want %x", got, entry)
	}
}

func TestWriteEncodedDirectoryRecord(t *testing.T) {
	const (
		sectorSize       = 512
		bytesPerCluster  = 512
		fatOffset        = 0
		dataOffset       = 512
		directoryCluster = uint32(2)
	)

	data := make([]byte, dataOffset+bytesPerCluster)

	binary.LittleEndian.PutUint32(
		data[fatOffset+directoryCluster*4:],
		fat32EOF,
	)

	writer := writerAtBuffer{
		data: data,
	}

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: writer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      2,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	record, err := encodeDirectoryRecord(
		"This is a long filename.txt",
		1234,
		5678,
	)
	if err != nil {
		t.Fatal(err)
	}

	slots := []directoryEntrySlot{
		{Cluster: directoryCluster, Offset: 0},
		{Cluster: directoryCluster, Offset: 32},
		{Cluster: directoryCluster, Offset: 64},
		{Cluster: directoryCluster, Offset: 96},
	}

	if len(record.LFN)+1 != len(slots) {
		t.Fatalf(
			"record needs %d slots, got %d",
			len(record.LFN)+1,
			len(slots),
		)
	}

	// This test deliberately calls the function we are about to implement.
	if err := fs.writeEncodedDirectoryRecord(record, slots); err != nil {
		t.Fatal(err)
	}

	for i, entry := range record.LFN {
		offset := dataOffset + i*32
		if !bytes.Equal(data[offset:offset+32], entry[:]) {
			t.Fatalf("LFN entry %d was not written correctly", i)
		}
	}

	offset := dataOffset + len(record.LFN)*32
	if !bytes.Equal(data[offset:offset+32], record.Short[:]) {
		t.Fatal("short directory entry was not written correctly")
	}
}

func TestWriteEncodedDirectoryRecordRequiresContiguousSlots(t *testing.T) {
	const (
		sectorSize       = 512
		bytesPerCluster  = 512
		fatOffset        = 0
		dataOffset       = 512
		directoryCluster = uint32(2)
	)

	data := make([]byte, dataOffset+bytesPerCluster)

	binary.LittleEndian.PutUint32(
		data[fatOffset+directoryCluster*4:],
		fat32EOF,
	)

	writer := writerAtBuffer{
		data: data,
	}

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: writer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      2,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	record, err := encodeDirectoryRecord(
		"This is a long filename.txt",
		1234,
		5678,
	)
	if err != nil {
		t.Fatal(err)
	}

	slots := []directoryEntrySlot{
		{Cluster: directoryCluster, Offset: 0},
		{Cluster: directoryCluster, Offset: 32},
		{Cluster: directoryCluster, Offset: 96}, // gap at 64
		{Cluster: directoryCluster, Offset: 128},
	}

	err = fs.writeEncodedDirectoryRecord(record, slots)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestWriteEncodedDirectoryRecordFailureDoesNotSilentlySucceed(t *testing.T) {
	const (
		sectorSize       = 512
		bytesPerCluster  = 512
		fatOffset        = 0
		dataOffset       = 512
		directoryCluster = uint32(2)
	)

	data := make([]byte, dataOffset+bytesPerCluster)

	binary.LittleEndian.PutUint32(
		data[fatOffset+directoryCluster*4:],
		fat32EOF,
	)

	writer := &failingWriterAt{
		data:      data,
		failAfter: 1,
	}

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: writer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      2,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	record, err := encodeDirectoryRecord(
		"This is a long filename.txt",
		1234,
		5678,
	)
	if err != nil {
		t.Fatal(err)
	}

	slots := []directoryEntrySlot{
		{Cluster: directoryCluster, Offset: 0},
		{Cluster: directoryCluster, Offset: 32},
		{Cluster: directoryCluster, Offset: 64},
		{Cluster: directoryCluster, Offset: 96},
	}

	err = fs.writeEncodedDirectoryRecord(record, slots)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestWriteEncodedDirectoryRecordRollsBackOnFailure(t *testing.T) {
	const (
		sectorSize       = 512
		bytesPerCluster  = 512
		fatOffset        = 0
		dataOffset       = 512
		directoryCluster = uint32(2)
	)

	data := make([]byte, dataOffset+bytesPerCluster)

	binary.LittleEndian.PutUint32(
		data[fatOffset+directoryCluster*4:],
		fat32EOF,
	)

	original := append([]byte(nil), data...)

	writer := &failingWriterAt{
		data:      data,
		failAfter: 1,
		failOnce:  true,
	}

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: writer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      2,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	record, err := encodeDirectoryRecord(
		"This is a long filename.txt",
		1234,
		5678,
	)
	if err != nil {
		t.Fatal(err)
	}

	slots := []directoryEntrySlot{
		{Cluster: directoryCluster, Offset: 0},
		{Cluster: directoryCluster, Offset: 32},
		{Cluster: directoryCluster, Offset: 64},
		{Cluster: directoryCluster, Offset: 96},
	}

	if err := fs.writeEncodedDirectoryRecord(record, slots); err == nil {
		t.Fatal("expected error, got nil")
	}

	if !bytes.Equal(writer.data, original) {
		t.Fatal("directory data was modified despite write failure")
	}
}

func TestWriteEncodedDirectoryRecordRollsBackAcrossClusters(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		cluster1        = uint32(2)
		cluster2        = uint32(3)
	)

	data := make([]byte, dataOffset+2*bytesPerCluster)

	binary.LittleEndian.PutUint32(
		data[fatOffset+cluster1*4:],
		cluster2,
	)
	binary.LittleEndian.PutUint32(
		data[fatOffset+cluster2*4:],
		fat32EOF,
	)

	original := append([]byte(nil), data...)

	writer := &failingWriterAt{
		data:      data,
		failAfter: 1,
		failOnce:  true,
	}

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: writer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      3,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	record, err := encodeDirectoryRecord(
		"This is a long filename.txt",
		1234,
		5678,
	)
	if err != nil {
		t.Fatal(err)
	}

	slots := []directoryEntrySlot{
		{Cluster: cluster1, Offset: 480},
		{Cluster: cluster2, Offset: 0},
		{Cluster: cluster2, Offset: 32},
		{Cluster: cluster2, Offset: 64},
	}

	err = fs.writeEncodedDirectoryRecord(record, slots)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !bytes.Equal(writer.data, original) {
		t.Fatal("directory data was modified despite write failure")
	}
}

func TestWriteEncodedDirectoryRecordAcrossClusters(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		cluster1        = uint32(2)
		cluster2        = uint32(3)
	)

	data := make([]byte, dataOffset+2*bytesPerCluster)

	binary.LittleEndian.PutUint32(
		data[fatOffset+cluster1*4:],
		cluster2,
	)
	binary.LittleEndian.PutUint32(
		data[fatOffset+cluster2*4:],
		fat32EOF,
	)

	writer := writerAtBuffer{
		data: data,
	}

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: writer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      3,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	record, err := encodeDirectoryRecord(
		"This is a long filename.txt",
		1234,
		5678,
	)
	if err != nil {
		t.Fatal(err)
	}

	slots := []directoryEntrySlot{
		{Cluster: cluster1, Offset: 480},
		{Cluster: cluster2, Offset: 0},
		{Cluster: cluster2, Offset: 32},
		{Cluster: cluster2, Offset: 64},
	}

	if err := fs.writeEncodedDirectoryRecord(record, slots); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(
		data[dataOffset+480:dataOffset+512],
		record.LFN[0][:32],
	) {
		t.Fatal("first LFN entry was not written correctly")
	}

	cluster2Offset := dataOffset + bytesPerCluster

	if !bytes.Equal(
		data[cluster2Offset:cluster2Offset+32],
		record.LFN[1][:],
	) {
		t.Fatal("second LFN entry was not written correctly")
	}

	if !bytes.Equal(
		data[cluster2Offset+32:cluster2Offset+64],
		record.LFN[2][:],
	) {
		t.Fatal("third LFN entry was not written correctly")
	}

	if !bytes.Equal(
		data[cluster2Offset+64:cluster2Offset+96],
		record.Short[:],
	) {
		t.Fatal("short directory entry was not written correctly")
	}
}

func TestWriteEncodedDirectoryRecordRejectsClusterJump(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		cluster1        = uint32(2)
		cluster2        = uint32(3)
		cluster3        = uint32(4)
	)

	data := make([]byte, dataOffset+3*bytesPerCluster)

	// Directory chain: 2 -> 3 -> EOF.
	binary.LittleEndian.PutUint32(
		data[fatOffset+cluster1*4:],
		cluster2,
	)
	binary.LittleEndian.PutUint32(
		data[fatOffset+cluster2*4:],
		fat32EOF,
	)

	writer := writerAtBuffer{
		data: data,
	}

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: writer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      4,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	record, err := encodeDirectoryRecord(
		"This is a long filename.txt",
		1234,
		5678,
	)
	if err != nil {
		t.Fatal(err)
	}

	// Skip cluster 3 and jump from cluster 2 to cluster 4.
	slots := []directoryEntrySlot{
		{Cluster: cluster1, Offset: 480},
		{Cluster: cluster3, Offset: 0},
		{Cluster: cluster3, Offset: 32},
		{Cluster: cluster3, Offset: 64},
	}

	err = fs.writeEncodedDirectoryRecord(record, slots)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFindFreeDirectorySlotsAcrossClusters(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		cluster1        = uint32(2)
		cluster2        = uint32(3)
	)

	data := make([]byte, dataOffset+2*bytesPerCluster)

	// Directory chain: 2 -> 3 -> EOF.
	binary.LittleEndian.PutUint32(
		data[fatOffset+cluster1*4:],
		cluster2,
	)
	binary.LittleEndian.PutUint32(
		data[fatOffset+cluster2*4:],
		fat32EOF,
	)

	// All entries in cluster 1 are occupied except the final slot.
	for offset := 0; offset < bytesPerCluster-32; offset += 32 {
		data[dataOffset+offset] = 'A'
	}

	// Cluster 2 starts completely free.
	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      3,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	slots, err := fs.findFreeDirectorySlots(cluster1, 2)
	if err != nil {
		t.Fatal(err)
	}

	if len(slots) != 2 {
		t.Fatalf("got %d slots, want 2", len(slots))
	}

	if slots[0].Cluster != cluster1 || slots[0].Offset != 480 {
		t.Fatalf("first slot = %+v, want cluster %d offset 480",
			slots[0], cluster1)
	}

	if slots[1].Cluster != cluster2 || slots[1].Offset != 0 {
		t.Fatalf("second slot = %+v, want cluster %d offset 0",
			slots[1], cluster2)
	}
}

func TestFindFreeDirectorySlotsAfterEndOfDirectory(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		cluster1        = uint32(2)
		cluster2        = uint32(3)
	)

	data := make([]byte, dataOffset+2*bytesPerCluster)

	// Directory chain: 2 -> 3 -> EOF.
	binary.LittleEndian.PutUint32(
		data[fatOffset+cluster1*4:],
		cluster2,
	)
	binary.LittleEndian.PutUint32(
		data[fatOffset+cluster2*4:],
		fat32EOF,
	)

	// All entries in cluster 1 are occupied except the final
	// entry, which marks end-of-directory.
	for offset := 0; offset < bytesPerCluster-32; offset += 32 {
		data[dataOffset+offset] = 'A'
	}
	data[dataOffset+bytesPerCluster-32] = 0x00

	// Make the first entry in cluster 2 non-zero. It must still
	// be treated as unused because cluster 1 already ended.
	data[dataOffset+bytesPerCluster] = 'B'

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      3,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	slots, err := fs.findFreeDirectorySlots(cluster1, 2)
	if err != nil {
		t.Fatal(err)
	}

	if len(slots) != 2 {
		t.Fatalf("got %d slots, want 2", len(slots))
	}

	if slots[0].Cluster != cluster1 || slots[0].Offset != 480 {
		t.Fatalf(
			"first slot = %+v, want cluster %d offset 480",
			slots[0],
			cluster1,
		)
	}

	if slots[1].Cluster != cluster2 || slots[1].Offset != 0 {
		t.Fatalf(
			"second slot = %+v, want cluster %d offset 0",
			slots[1],
			cluster2,
		)
	}
}

func TestCreateDirectoryRecord(t *testing.T) {
	const (
		sectorSize       = 512
		bytesPerCluster  = 512
		fatOffset        = 0
		dataOffset       = 512
		directoryCluster = uint32(2)
	)

	data := make([]byte, dataOffset+bytesPerCluster)

	// Directory cluster 2 is terminated immediately.
	binary.LittleEndian.PutUint32(
		data[fatOffset+directoryCluster*4:],
		fat32EOF,
	)
	data[dataOffset] = 0x00

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: &writerAtBuffer{data: data},
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      2,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	const (
		name         = "Cartoni animati"
		startCluster = uint32(123)
		size         = uint32(456)
	)

	if err := fs.createDirectoryRecord(
		directoryCluster,
		name,
		startCluster,
		size,
	); err != nil {
		t.Fatal(err)
	}

	records, err := fs.readDirectoryRecords(directoryCluster)
	if err != nil {
		t.Fatal(err)
	}

	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}

	record := records[0]

	if record.Entry.Name != name {
		t.Fatalf("got name %q, want %q", record.Entry.Name, name)
	}

	if record.Entry.StartCluster != startCluster {
		t.Fatalf(
			"got start cluster %d, want %d",
			record.Entry.StartCluster,
			startCluster,
		)
	}

	if record.Entry.Size != size {
		t.Fatalf(
			"got size %d, want %d",
			record.Entry.Size,
			size,
		)
	}

	if record.Entry.LFNEntries != 2 {
		t.Fatalf(
			"got %d LFN entries, want 2",
			record.Entry.LFNEntries,
		)
	}

	if record.Short.Offset != 64 {
		t.Fatalf(
			"short entry offset = %d, want 64",
			record.Short.Offset,
		)

	}
}

func TestSplitFilePath(t *testing.T) {
	tests := []struct {
		path   string
		parent string
		name   string
	}{
		{
			path:   "/Music/Pollon.mp3",
			parent: "/Music",
			name:   "Pollon.mp3",
		},
		{
			path:   "/Music/Cartoni animati/New Song.mp3",
			parent: "/Music/Cartoni animati",
			name:   "New Song.mp3",
		},
		{
			path:   "Pollon.mp3",
			parent: ".",
			name:   "Pollon.mp3",
		},
		{
			path:   "/Pollon.mp3",
			parent: "/",
			name:   "Pollon.mp3",
		},
	}

	for _, test := range tests {
		parent, name, err := splitFilePath(test.path)
		if err != nil {
			t.Fatalf("%q: %v", test.path, err)
		}

		if parent != test.parent {
			t.Fatalf(
				"%q: parent = %q, want %q",
				test.path,
				parent,
				test.parent,
			)
		}

		if name != test.name {
			t.Fatalf(
				"%q: name = %q, want %q",
				test.path,
				name,
				test.name,
			)
		}
	}
}

// Test a two-level directory path:
//
//	root (cluster 2)
//	└── Music (cluster 3)
//	    └── Cartoni animati (cluster 4)
func TestFindDirectoryCluster(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512

		rootCluster    = uint32(2)
		musicCluster   = uint32(3)
		cartoniCluster = uint32(4)
	)

	data := make([]byte, dataOffset+3*bytesPerCluster)

	// Directory chain:
	// root (2) -> EOF
	// Music (3) -> EOF
	// Cartoni animati (4) -> EOF
	for _, cluster := range []uint32{
		rootCluster,
		musicCluster,
		cartoniCluster,
	} {
		binary.LittleEndian.PutUint32(
			data[fatOffset+cluster*4:],
			fat32EOF,
		)
	}

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      4,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	// Write directory entries directly into the fixture.
	rootRecord, err := encodeDirectoryRecord(
		"Music",
		musicCluster,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}

	musicSlots, err := fs.findFreeDirectorySlots(
		rootCluster,
		len(rootRecord.LFN)+1,
	)
	if err != nil {
		t.Fatal(err)
	}

	fs.writer = &writerAtBuffer{data: data}

	if err := fs.writeEncodedDirectoryRecord(rootRecord, musicSlots); err != nil {
		t.Fatal(err)
	}

	rootData, err := fs.readCluster(rootCluster)
	if err != nil {
		t.Fatal(err)
	}
	rootData[musicSlots[len(musicSlots)-1].Offset+0x0B] = 0x10

	if err := fs.writeCluster(rootCluster, rootData); err != nil {
		t.Fatal(err)
	}

	musicRecord, err := encodeDirectoryRecord(
		"Cartoni animati",
		cartoniCluster,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}

	musicSlots2, err := fs.findFreeDirectorySlots(
		musicCluster,
		len(musicRecord.LFN)+1,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := fs.writeEncodedDirectoryRecord(musicRecord, musicSlots2); err != nil {
		t.Fatal(err)
	}

	musicData, err := fs.readCluster(musicCluster)
	if err != nil {
		t.Fatal(err)
	}
	musicData[musicSlots2[len(musicSlots2)-1].Offset+0x0B] = 0x10

	if err := fs.writeCluster(musicCluster, musicData); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		path string
		want uint32
	}{
		{path: "/", want: rootCluster},
		{path: ".", want: rootCluster},
		{path: "/Music", want: musicCluster},
		{
			path: "/Music/Cartoni animati",
			want: cartoniCluster,
		},
	}

	for _, test := range tests {
		got, err := fs.findDirectoryCluster(test.path)
		if err != nil {
			t.Fatalf("%q: %v", test.path, err)
		}

		if got != test.want {
			t.Fatalf(
				"%q: got cluster %d, want %d",
				test.path,
				got,
				test.want,
			)
		}
	}
}

func TestFindDirectoryEntry(t *testing.T) {
	const (
		sectorSize       = 512
		bytesPerCluster  = 512
		fatOffset        = 0
		dataOffset       = 512
		directoryCluster = uint32(2)
	)

	data := make([]byte, dataOffset+bytesPerCluster)

	binary.LittleEndian.PutUint32(
		data[fatOffset+directoryCluster*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: &writerAtBuffer{data: data},
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      2,
			RootCluster:       directoryCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	record, err := encodeDirectoryRecord(
		"Test File.txt",
		123,
		42,
	)
	if err != nil {
		t.Fatal(err)
	}

	slots, err := fs.findFreeDirectorySlots(
		directoryCluster,
		len(record.LFN)+1,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := fs.writeEncodedDirectoryRecord(record, slots); err != nil {
		t.Fatal(err)
	}

	found, err := fs.findDirectoryEntry(directoryCluster, "test file.txt")
	if err != nil {
		t.Fatal(err)
	}

	if found == nil {
		t.Fatal("expected directory entry, got nil")
	}

	if found.Entry.Name != "Test File.txt" {
		t.Fatalf(
			"got name %q, want %q",
			found.Entry.Name,
			"Test File.txt",
		)
	}

	found, err = fs.findDirectoryEntry(
		directoryCluster,
		"does-not-exist.txt",
	)
	if err != nil {
		t.Fatal(err)
	}

	if found != nil {
		t.Fatal("expected nil for missing entry")
	}
}

func TestShortNameExists(t *testing.T) {
	const (
		sectorSize       = 512
		bytesPerCluster  = 512
		fatOffset        = 0
		dataOffset       = 512
		directoryCluster = uint32(2)
	)

	data := make([]byte, dataOffset+bytesPerCluster)

	binary.LittleEndian.PutUint32(
		data[fatOffset+directoryCluster*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: &writerAtBuffer{data: data},
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      2,
			RootCluster:       directoryCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	record, err := encodeDirectoryRecord(
		"Some long filename.txt",
		123,
		456,
	)
	if err != nil {
		t.Fatal(err)
	}

	slots, err := fs.findFreeDirectorySlots(
		directoryCluster,
		len(record.LFN)+1,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := fs.writeEncodedDirectoryRecord(record, slots); err != nil {
		t.Fatal(err)
	}

	exists, err := fs.shortNameExists(
		directoryCluster,
		recordShortName(record),
	)
	if err != nil {
		t.Fatal(err)
	}

	if !exists {
		t.Fatal("expected short name to exist")
	}

	exists, err = fs.shortNameExists(
		directoryCluster,
		"OTHER~1.TXT",
	)
	if err != nil {
		t.Fatal(err)
	}

	if exists {
		t.Fatal("unexpected short-name collision")
	}
}

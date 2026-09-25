package fat32

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestNew(t *testing.T) {
	data := []byte("test filesystem")
	reader := bytes.NewReader(data)

	fs := New(reader)

	if fs.reader != reader {
		t.Fatalf("reader = %v, want %v", fs.reader, reader)
	}
}

func TestBootSector(t *testing.T) {
	var boot [512]byte

	binary.LittleEndian.PutUint16(boot[0x0B:0x0D], 512)
	boot[0x0D] = 8
	binary.LittleEndian.PutUint16(boot[0x0E:0x10], 32)
	boot[0x10] = 2
	binary.LittleEndian.PutUint32(boot[0x24:0x28], 1000)
	binary.LittleEndian.PutUint32(boot[0x2C:0x30], 2)

	got, err := readBootSector(bytes.NewReader(boot[:]))
	if err != nil {
		t.Fatal(err)
	}

	if got.BytesPerSector != 512 {
		t.Errorf("BytesPerSector = %d, want 512", got.BytesPerSector)
	}

	if got.SectorsPerCluster != 8 {
		t.Errorf("SectorsPerCluster = %d, want 8", got.SectorsPerCluster)
	}

	if got.ReservedSectors != 32 {
		t.Errorf("ReservedSectors = %d, want 32", got.ReservedSectors)
	}

	if got.NumberOfFATs != 2 {
		t.Errorf("NumberOfFATs = %d, want 2", got.NumberOfFATs)
	}

	if got.SectorsPerFAT != 1000 {
		t.Errorf("SectorsPerFAT = %d, want 1000", got.SectorsPerFAT)
	}

	if got.RootCluster != 2 {
		t.Errorf("RootCluster = %d, want 2", got.RootCluster)
	}
}

func TestBootSectorLayout(t *testing.T) {
	boot := BootSector{
		BytesPerSector:    512,
		SectorsPerCluster: 8,
		ReservedSectors:   36,
		NumberOfFATs:      2,
		SectorsPerFAT:     3862,
		RootCluster:       2,
	}

	layout := boot.Layout()

	if layout.FATOffset != 18432 {
		t.Errorf("FATOffset = %d, want 18432", layout.FATOffset)
	}

	if layout.DataOffset != 3973120 {
		t.Errorf("DataOffset = %d, want 3973120", layout.DataOffset)
	}

	if layout.BytesPerCluster != 4096 {
		t.Errorf("BytesPerCluster = %d, want 4096", layout.BytesPerCluster)
	}
}

// Test the entire FAT32 deletion chain, end-to-end.
//
//	Delete("/Example.txt")
//	│
//	├── findDirectoryRecord()
//	│      └── LFN + short physical locations
//	│
//	├── readClusterChain()
//	│      └── 3 → 4 → EOF
//	│
//	├── deleteDirectoryRecord()
//	│      ├── LFN → E5
//	│      └── short → E5
//	│
//	└── writeFATEntry()
//	      ├── FAT[3] = 0
//	      └── FAT[4] = 0
//	         (both FAT copies)
func TestDeleteWriteFailure(t *testing.T) {
	data := make([]byte, 16384)

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
	// root directory: 2 -> EOF
	// file:           3 -> 4 -> EOF
	// unrelated:      5 -> EOF
	//
	// Write both FAT copies.
	fatValues := map[uint32]uint32{
		2: fat32EOF,
		3: 4,
		4: fat32EOF,
		5: fat32EOF,
	}

	secondFAT := layout.FATOffset +
		int64(boot.SectorsPerFAT)*int64(boot.BytesPerSector)

	for cluster, value := range fatValues {
		offset := layout.FATOffset + int64(cluster)*4
		binary.LittleEndian.PutUint32(
			data[offset:offset+4],
			value,
		)

		offset = secondFAT + int64(cluster)*4
		binary.LittleEndian.PutUint32(
			data[offset:offset+4],
			value,
		)
	}

	// Root directory is cluster 2.
	directoryOffset := layout.DataOffset

	// Short 8.3 name used by the LFN checksum.
	shortName := []byte("EXAMPL~1TXT")

	// LFN entry at offset 0.
	lfn := data[directoryOffset : directoryOffset+32]
	lfn[0] = 0x41 // sequence 1 + LAST flag
	lfn[11] = 0x0F
	lfn[13] = shortNameChecksum(shortName)

	// "Example.txt" is 11 UTF-16 characters.
	name := []uint16{
		'E', 'x', 'a', 'm', 'p', 'l', 'e', '.', 't', 'x', 't',
	}

	for i := range 13 {
		value := uint16(0xFFFF)

		if i < len(name) {
			value = name[i]
		} else if i == len(name) {
			value = 0x0000
		}

		switch {
		case i < 5:
			binary.LittleEndian.PutUint16(
				lfn[1+i*2:3+i*2],
				value,
			)
		case i < 11:
			j := i - 5
			binary.LittleEndian.PutUint16(
				lfn[14+j*2:16+j*2],
				value,
			)
		default:
			j := i - 11
			binary.LittleEndian.PutUint16(
				lfn[28+j*2:30+j*2],
				value,
			)
		}
	}

	// Short entry at offset 32.
	short := data[directoryOffset+32 : directoryOffset+64]
	copy(short[0:11], shortName)

	short[11] = 0x20 // archive attribute

	binary.LittleEndian.PutUint16(short[20:22], 0)
	binary.LittleEndian.PutUint16(short[26:28], 3)
	binary.LittleEndian.PutUint32(short[28:32], 9000)

	// End of directory marker.
	data[directoryOffset+64] = 0x00

	reader := bytes.NewReader(data)
	writer := &failingWriterAt{
		data:      data,
		failAfter: 4,
	}

	fs := NewReadWrite(reader, writer)
	fs.boot = boot
	fs.layout = layout

	if err := fs.Delete("/Example.txt"); err == nil {
		t.Fatal("Delete unexpectedly succeeded")
	}

	// The directory entries must have been deleted before we started
	// freeing the file's clusters.
	if got := data[directoryOffset]; got != 0xE5 {
		t.Fatalf("LFN entry was not marked deleted: got %#x", got)
	}

	if got := data[directoryOffset+32]; got != 0xE5 {
		t.Fatalf("short entry was not marked deleted: got %#x", got)
	}

	// Cluster 3 was freed before the failure.
	free, err := fs.IsFreeCluster(3)
	if err != nil {
		t.Fatal(err)
	}

	if !free {
		t.Fatal("cluster 3 was not freed")
	}

	// Cluster 4 was not reached.
	free, err = fs.IsFreeCluster(4)
	if err != nil {
		t.Fatal(err)
	}

	if free {
		t.Fatal("cluster 4 was unexpectedly freed")
	}

	t.Log("Delete failed as expected")

}

func TestDeleteShortNameOnlyFile(t *testing.T) {
	data := make([]byte, 16384)

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
	// root directory: 2 -> EOF
	// file:           3 -> EOF
	fatValues := map[uint32]uint32{
		2: fat32EOF,
		3: fat32EOF,
	}

	secondFAT := layout.FATOffset +
		int64(boot.SectorsPerFAT)*int64(boot.BytesPerSector)

	for cluster, value := range fatValues {
		offset := layout.FATOffset + int64(cluster)*4
		binary.LittleEndian.PutUint32(
			data[offset:offset+4],
			value,
		)

		offset = secondFAT + int64(cluster)*4
		binary.LittleEndian.PutUint32(
			data[offset:offset+4],
			value,
		)
	}

	// Root directory is cluster 2.
	directoryOffset := layout.DataOffset

	// Short entry at offset 0.
	short := data[directoryOffset : directoryOffset+32]

	copy(short[0:11], []byte("TEST    TXT"))
	short[11] = 0x20 // archive

	binary.LittleEndian.PutUint16(short[20:22], 0)
	binary.LittleEndian.PutUint16(short[26:28], 3)
	binary.LittleEndian.PutUint32(short[28:32], 100)

	// End of directory marker.
	data[directoryOffset+32] = 0x00

	reader := bytes.NewReader(data)
	writer := writerAtBuffer{data: data}

	fs := NewReadWrite(reader, writer)
	fs.boot = boot
	fs.layout = layout

	if err := fs.Delete("/TEST.TXT"); err != nil {
		t.Fatal(err)
	}

	// The short entry should be marked deleted.
	if got := data[directoryOffset]; got != 0xE5 {
		t.Fatalf(
			"short entry first byte = %#x, want %#x",
			got,
			byte(0xE5),
		)
	}

	// The file's cluster should be free.
	free, err := fs.IsFreeCluster(3)
	if err != nil {
		t.Fatal(err)
	}

	if !free {
		t.Fatal("cluster 3 is still allocated")
	}

	// The file should no longer be found.
	if _, err := fs.Lookup("/TEST.TXT"); err == nil {
		t.Fatal("deleted file was still found")
	}
}

func TestDeleteDirectory(t *testing.T) {
	data := make([]byte, 16384)

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
	// root directory: 2 -> EOF
	// subdirectory:   3 -> EOF
	fatValues := map[uint32]uint32{
		2: fat32EOF,
		3: fat32EOF,
	}

	secondFAT := layout.FATOffset +
		int64(boot.SectorsPerFAT)*int64(boot.BytesPerSector)

	for cluster, value := range fatValues {
		offset := layout.FATOffset + int64(cluster)*4
		binary.LittleEndian.PutUint32(
			data[offset:offset+4],
			value,
		)

		offset = secondFAT + int64(cluster)*4
		binary.LittleEndian.PutUint32(
			data[offset:offset+4],
			value,
		)
	}

	// Root directory is cluster 2.
	directoryOffset := layout.DataOffset

	// Directory entry for TESTDIR at offset 0.
	entry := data[directoryOffset : directoryOffset+32]

	copy(entry[0:11], []byte("TESTDIR    "))
	entry[11] = 0x10 // directory attribute

	binary.LittleEndian.PutUint16(entry[20:22], 0)
	binary.LittleEndian.PutUint16(entry[26:28], 3)

	// End of directory marker.
	data[directoryOffset+32] = 0x00

	reader := bytes.NewReader(data)
	writer := writerAtBuffer{data: data}

	fs := NewReadWrite(reader, writer)
	fs.boot = boot
	fs.layout = layout

	err := fs.Delete("/TESTDIR")
	if err == nil {
		t.Fatal("Delete unexpectedly succeeded on a directory")
	}

	// The directory entry must remain intact.
	if got := data[directoryOffset]; got != 'T' {
		t.Fatalf(
			"directory entry first byte = %#x, want %#x",
			got,
			byte('T'),
		)
	}

	// The directory's cluster must remain allocated.
	free, err := fs.IsFreeCluster(3)
	if err != nil {
		t.Fatal(err)
	}

	if free {
		t.Fatal("directory cluster 3 was unexpectedly freed")
	}
}

func TestDeleteCorruptClusterChain(t *testing.T) {
	data := make([]byte, 16384)

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
	// root directory: 2 -> EOF
	// file:           3 -> 0   <-- invalid
	fatValues := map[uint32]uint32{
		2: fat32EOF,
		3: 1, // invalid next cluster
	}

	// Expected behaviour
	//
	//  FAT[3] = 1
	//       ↓
	//  readClusterChain(3)
	//       ↓
	//  invalid cluster 1
	//       ↓
	//  Delete() returns error
	//       ↓
	//  directory entry remains intact
	//  FAT[3] remains 1

	secondFAT := layout.FATOffset +
		int64(boot.SectorsPerFAT)*int64(boot.BytesPerSector)

	for cluster, value := range fatValues {
		offset := layout.FATOffset + int64(cluster)*4
		binary.LittleEndian.PutUint32(
			data[offset:offset+4],
			value,
		)

		offset = secondFAT + int64(cluster)*4
		binary.LittleEndian.PutUint32(
			data[offset:offset+4],
			value,
		)
	}

	// Root directory is cluster 2.
	directoryOffset := layout.DataOffset

	// Short entry at offset 0.
	entry := data[directoryOffset : directoryOffset+32]

	copy(entry[0:11], []byte("BROKEN  TXT"))
	entry[11] = 0x20 // archive

	binary.LittleEndian.PutUint16(entry[20:22], 0)
	binary.LittleEndian.PutUint16(entry[26:28], 3)
	binary.LittleEndian.PutUint32(entry[28:32], 100)

	// End of directory marker.
	data[directoryOffset+32] = 0x00

	reader := bytes.NewReader(data)
	writer := writerAtBuffer{data: data}

	fs := NewReadWrite(reader, writer)
	fs.boot = boot
	fs.layout = layout

	err := fs.Delete("/BROKEN.TXT")
	if err == nil {
		t.Fatal("Delete unexpectedly succeeded on corrupt cluster chain")
	}

	// The directory entry must remain untouched.
	if got := data[directoryOffset]; got != 'B' {
		t.Fatalf(
			"directory entry first byte = %#x, want %#x",
			got,
			byte('B'),
		)
	}

	// Cluster 3 must remain allocated.
	free, err := fs.IsFreeCluster(3)
	if err != nil {
		t.Fatal(err)
	}

	if free {
		t.Fatal("cluster 3 was unexpectedly freed")
	}
}

func TestDeleteDirectoryRecordMultipleLFN(t *testing.T) {
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
	directoryOffset := layout.DataOffset

	// Two LFN entries followed by a short entry.
	//
	// offset 0:    LFN #2
	// offset 32:   LFN #1
	// offset 64:   short entry
	//
	// The actual LFN parsing/order is tested elsewhere. Here we only
	// need the physical locations for deletion.
	data[directoryOffset+0] = 0x42
	data[directoryOffset+32] = 0x01
	data[directoryOffset+64] = 'T'

	reader := bytes.NewReader(data)
	writer := writerAtBuffer{data: data}

	fs := NewReadWrite(reader, writer)
	fs.boot = boot
	fs.layout = layout

	record := directoryRecord{
		Entry: DirectoryEntry{
			Name:         "This is a reasonably long filename.txt",
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
				Offset:  0,
			},
			{
				Cluster: 2,
				Offset:  32,
			},
		},
	}

	if err := fs.deleteDirectoryRecord(record); err != nil {
		t.Fatal(err)
	}

	// Both LFN entries must be marked deleted.
	for _, offset := range []int64{0, 32} {
		if got := data[directoryOffset+offset]; got != 0xE5 {
			t.Fatalf(
				"LFN entry at offset %d = %#x, want %#x",
				offset,
				got,
				byte(0xE5),
			)
		}
	}

	// The short entry must also be marked deleted.
	if got := data[directoryOffset+64]; got != 0xE5 {
		t.Fatalf(
			"short entry first byte = %#x, want %#x",
			got,
			byte(0xE5),
		)
	}
}

func TestNewIsReadOnly(t *testing.T) {
	data := make([]byte, 4096)

	fs := New(bytes.NewReader(data))

	if err := fs.writable(); err == nil {
		t.Fatal("read-only filesystem unexpectedly has write access")
	}
}

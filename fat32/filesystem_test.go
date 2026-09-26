package fat32

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
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

func TestDeleteEmptyFile(t *testing.T) {
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
	// empty file:      no allocated clusters (start cluster 0)
	fatValues := map[uint32]uint32{
		2: fat32EOF,
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

	copy(short[0:11], []byte("EMPTY   TXT"))
	short[11] = 0x20 // archive

	// Start cluster = 0, size = 0.
	binary.LittleEndian.PutUint16(short[20:22], 0)
	binary.LittleEndian.PutUint16(short[26:28], 0)
	binary.LittleEndian.PutUint32(short[28:32], 0)

	// End of directory marker.
	data[directoryOffset+32] = 0x00

	reader := bytes.NewReader(data)
	writer := writerAtBuffer{data: data}

	fs := NewReadWrite(reader, writer)
	fs.boot = boot
	fs.layout = layout

	if err := fs.Delete("/EMPTY.TXT"); err != nil {
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

	// There were no allocated clusters, so FAT[2] must be unchanged.
	value, err := fs.readFATEntry(2)
	if err != nil {
		t.Fatal(err)
	}
	if value != fat32EOF {
		t.Fatalf(
			"root directory FAT entry = %#x, want %#x",
			value,
			fat32EOF,
		)
	}
}

func TestDeleteShortEntryWriteFailure(t *testing.T) {
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

	directoryOffset := layout.DataOffset

	shortName := []byte("EXAMPL~1TXT")

	// LFN entry at offset 0.
	lfn := data[directoryOffset : directoryOffset+32]
	lfn[0] = 0x41
	lfn[11] = 0x0F
	lfn[13] = shortNameChecksum(shortName)

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
	short[11] = 0x20
	binary.LittleEndian.PutUint16(short[20:22], 0)
	binary.LittleEndian.PutUint16(short[26:28], 3)
	binary.LittleEndian.PutUint32(short[28:32], 9000)

	data[directoryOffset+64] = 0x00

	reader := bytes.NewReader(data)
	writer := &failingWriterAt{
		data:      data,
		failAfter: 1,
	}

	fs := NewReadWrite(reader, writer)
	fs.boot = boot
	fs.layout = layout

	if err := fs.Delete("/Example.txt"); err == nil {
		t.Fatal("Delete unexpectedly succeeded")
	}

	// The LFN write succeeded.
	if got := data[directoryOffset]; got != 0xE5 {
		t.Fatalf("LFN entry was not marked deleted: got %#x", got)
	}

	// The short-entry write failed, so the short entry must remain intact.
	if got := data[directoryOffset+32]; got == 0xE5 {
		t.Fatal("short entry was unexpectedly marked deleted")
	}

	// No FAT writes should have happened.
	free, err := fs.IsFreeCluster(3)
	if err != nil {
		t.Fatal(err)
	}

	if free {
		t.Fatal("file cluster was unexpectedly freed")
	}
}

func TestWriteNewFileData(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		firstCluster    = uint32(2)
	)

	data := make([]byte, dataOffset+3*bytesPerCluster)

	// Cluster 2 is initially free.
	binary.LittleEndian.PutUint32(
		data[fatOffset+firstCluster*4:],
		0,
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: &writerAtBuffer{data: data},
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

	content := []byte("hello")

	chain, err := fs.writeNewFileData(content)
	if err != nil {
		t.Fatal(err)
	}

	if len(chain) != 1 {
		t.Fatalf("got %d clusters, want 1", len(chain))
	}

	if chain[0] != firstCluster {
		t.Fatalf("got cluster %d, want %d", chain[0], firstCluster)
	}

	got, err := fs.readCluster(firstCluster)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got[:len(content)], content) {
		t.Fatalf("got %q, want %q", got[:len(content)], content)
	}

	for _, b := range got[len(content):] {
		if b != 0 {
			t.Fatalf("expected unused portion of cluster to be zero, got %x", b)
		}
	}

	entry, err := fs.readFATEntry(firstCluster)
	if err != nil {
		t.Fatal(err)
	}

	if entry < fat32EOF {
		t.Fatalf("cluster %d is not marked EOF: %x", firstCluster, entry)
	}
}

func TestWriteFile(t *testing.T) {
	const (
		sectorSize       = 512
		bytesPerCluster  = 512
		fatOffset        = 0
		dataOffset       = 512
		rootCluster      = uint32(2)
		firstDataCluster = uint32(3)
	)

	data := make([]byte, dataOffset+3*bytesPerCluster)

	// Root directory cluster is allocated and terminated.
	binary.LittleEndian.PutUint32(
		data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	// Cluster 3 is free.
	binary.LittleEndian.PutUint32(
		data[fatOffset+firstDataCluster*4:],
		0,
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: &writerAtBuffer{data: data},
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

	content := []byte("hello world")

	if err := fs.WriteFile("/hello.txt", content); err != nil {
		t.Fatal(err)
	}

	file, err := fs.findDirectoryEntry(rootCluster, "HELLO.TXT")
	if err != nil {
		t.Fatal(err)
	}

	if file == nil {
		t.Fatal("file was not created")
	}

	if file.Entry.Size != uint32(len(content)) {
		t.Fatalf(
			"got size %d, want %d",
			file.Entry.Size,
			len(content),
		)
	}

	if file.Entry.StartCluster != firstDataCluster {
		t.Fatalf(
			"got start cluster %d, want %d",
			file.Entry.StartCluster,
			firstDataCluster,
		)
	}

	got, err := fs.readFile(file.Entry)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, content) {
		t.Fatalf(
			"got %q, want %q",
			got,
			content,
		)
	}
}

func TestWriteEmptyFile(t *testing.T) {
	const (
		sectorSize       = 512
		bytesPerCluster  = 512
		fatOffset        = 0
		dataOffset       = 512
		rootCluster      = uint32(2)
		firstDataCluster = uint32(3)
	)

	data := make([]byte, dataOffset+2*bytesPerCluster)

	// Root directory cluster is allocated.
	binary.LittleEndian.PutUint32(
		data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	// Cluster 3 is free.
	binary.LittleEndian.PutUint32(
		data[fatOffset+firstDataCluster*4:],
		0,
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: &writerAtBuffer{data: data},
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      3,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	if err := fs.WriteFile("/empty.txt", nil); err != nil {
		t.Fatal(err)
	}

	file, err := fs.findDirectoryEntry(rootCluster, "EMPTY.TXT")
	if err != nil {
		t.Fatal(err)
	}

	if file == nil {
		t.Fatal("file was not created")
	}

	if file.Entry.Size != 0 {
		t.Fatalf("got size %d, want 0", file.Entry.Size)
	}

	if file.Entry.StartCluster != 0 {
		t.Fatalf(
			"got start cluster %d, want 0",
			file.Entry.StartCluster,
		)
	}

	// No data cluster should have been allocated.
	entry, err := fs.readFATEntry(firstDataCluster)
	if err != nil {
		t.Fatal(err)
	}

	if entry != 0 {
		t.Fatalf(
			"cluster %d was allocated: FAT entry = %x",
			firstDataCluster,
			entry,
		)
	}
}

func TestWriteFileMultipleClusters(t *testing.T) {
	// Check these steps:
	//
	// WriteFile
	// → allocate 2 clusters
	// → link FAT chain
	// → write both clusters
	// → create directory entry
	// → read chain
	// → reconstruct exact file
	const (
		sectorSize        = 512
		bytesPerCluster   = 512
		fatOffset         = 0
		dataOffset        = 512
		rootCluster       = uint32(2)
		firstDataCluster  = uint32(3)
		secondDataCluster = uint32(4)
	)

	data := make([]byte, dataOffset+3*bytesPerCluster)

	// Root directory cluster.
	binary.LittleEndian.PutUint32(
		data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	// Both data clusters are initially free.
	binary.LittleEndian.PutUint32(
		data[fatOffset+firstDataCluster*4:],
		0,
	)
	binary.LittleEndian.PutUint32(
		data[fatOffset+secondDataCluster*4:],
		0,
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: &writerAtBuffer{data: data},
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      5,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	// More than one cluster, but not an exact multiple.
	content := make([]byte, bytesPerCluster+100)
	for i := range content {
		content[i] = byte(i % 251)
	}

	if err := fs.WriteFile("/large.bin", content); err != nil {
		t.Fatal(err)
	}

	file, err := fs.findDirectoryEntry(rootCluster, "LARGE.BIN")
	if err != nil {
		t.Fatal(err)
	}

	if file == nil {
		t.Fatal("file was not created")
	}

	if file.Entry.Size != uint32(len(content)) {
		t.Fatalf(
			"got size %d, want %d",
			file.Entry.Size,
			len(content),
		)
	}

	if file.Entry.StartCluster != firstDataCluster {
		t.Fatalf(
			"got start cluster %d, want %d",
			file.Entry.StartCluster,
			firstDataCluster,
		)
	}

	chain, err := fs.ClusterChain(firstDataCluster)
	if err != nil {
		t.Fatal(err)
	}

	if len(chain) != 2 {
		t.Fatalf("got chain length %d, want 2", len(chain))
	}

	if chain[0] != firstDataCluster || chain[1] != secondDataCluster {
		t.Fatalf(
			"got chain %v, want [%d %d]",
			chain,
			firstDataCluster,
			secondDataCluster,
		)
	}

	got, err := fs.readFile(file.Entry)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, content) {
		t.Fatal("read-back data does not match original data")
	}
}

func TestWriteFileRollsBackWhenDirectoryIsFull(t *testing.T) {
	// A failed WriteFile must not leak its newly allocated clusters.
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
		dataCluster     = uint32(3)
	)

	data := make([]byte, dataOffset+2*bytesPerCluster)

	// Root directory cluster is allocated.
	binary.LittleEndian.PutUint32(
		data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	// Data cluster is initially free.
	binary.LittleEndian.PutUint32(
		data[fatOffset+dataCluster*4:],
		0,
	)

	// Fill every directory entry.
	for offset := 0; offset < bytesPerCluster; offset += 32 {
		data[dataOffset+offset] = 'X'
	}

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: &writerAtBuffer{data: data},
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      3,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	err := fs.WriteFile("/new.txt", []byte("hello"))
	if err == nil {
		t.Fatal("expected WriteFile to fail")
	}

	// The data cluster must have been released.
	entry, err := fs.readFATEntry(dataCluster)
	if err != nil {
		t.Fatal(err)
	}

	if entry != 0 {
		t.Fatalf(
			"data cluster %d was not released: FAT entry = %x",
			dataCluster,
			entry,
		)
	}
}

func TestWriteFileLongNameCollision(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
	)

	data := make([]byte, dataOffset+5*bytesPerCluster)

	// Root directory.
	binary.LittleEndian.PutUint32(
		data[fatOffset+rootCluster*4:],
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
			TotalSectors:      6,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	firstName := "Some long filename.txt"
	secondName := "Some long file-name.txt"

	if err := fs.WriteFile(firstName, []byte("first")); err != nil {
		t.Fatal(err)
	}

	if err := fs.WriteFile(secondName, []byte("second")); err != nil {
		t.Fatal(err)
	}

	first, err := fs.findDirectoryEntry(rootCluster, firstName)
	if err != nil {
		t.Fatal(err)
	}

	second, err := fs.findDirectoryEntry(rootCluster, secondName)
	if err != nil {
		t.Fatal(err)
	}

	if first == nil {
		t.Fatalf("file %q was not found", firstName)
	}

	if second == nil {
		t.Fatalf("file %q was not found", secondName)
	}

	if strings.EqualFold(
		first.Entry.ShortName,
		second.Entry.ShortName,
	) {
		t.Fatalf(
			"short-name collision: both use %q",
			first.Entry.ShortName,
		)
	}

	if first.Entry.ShortName != "SOMELO~1.TXT" {
		t.Fatalf(
			"first short name = %q, want %q",
			first.Entry.ShortName,
			"SOMELO~1.TXT",
		)
	}

	if second.Entry.ShortName != "SOMELO~2.TXT" {
		t.Fatalf(
			"second short name = %q, want %q",
			second.Entry.ShortName,
			"SOMELO~2.TXT",
		)
	}
}

func TestWriteFileDuplicateShortName(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
	)

	data := make([]byte, dataOffset+3*bytesPerCluster)

	binary.LittleEndian.PutUint32(
		data[fatOffset+rootCluster*4:],
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
			TotalSectors:      4,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	if err := fs.WriteFile("/test.txt", []byte("first")); err != nil {
		t.Fatal(err)
	}

	err := fs.WriteFile("/TEST.TXT", []byte("second"))
	if err == nil {
		t.Fatal("expected duplicate filename to be rejected")
	}

	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteFileShortNameCollidesWithLongName(t *testing.T) {
	// 1. Create Some long filename.txt → gets SOMELO~1.TXT.
	// 2. Try to create a file whose actual 8.3 name is SOMELO~1.TXT.
	// 3. It must be rejected, because the short directory name is already occupied.

	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
	)

	data := make([]byte, dataOffset+4*bytesPerCluster)

	binary.LittleEndian.PutUint32(
		data[fatOffset+rootCluster*4:],
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
			TotalSectors:      5,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	longName := "Some long filename.txt"
	shortName := "SOMELO~1.TXT"

	if err := fs.WriteFile(longName, []byte("first")); err != nil {
		t.Fatal(err)
	}

	err := fs.WriteFile(shortName, []byte("second"))
	if err == nil {
		t.Fatal("expected short-name collision to be rejected")
	}

	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenerateUniqueShortNameAvoidsExistingShortName(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
	)

	data := make([]byte, dataOffset+3*bytesPerCluster)

	binary.LittleEndian.PutUint32(
		data[fatOffset+rootCluster*4:],
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
			TotalSectors:      4,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	// Occupy the first generated alias.
	if err := fs.WriteFile("SOMELO~1.TXT", []byte("existing")); err != nil {
		t.Fatal(err)
	}

	shortName, err := fs.generateUniqueShortName(
		rootCluster,
		"Some long filename.txt",
	)
	if err != nil {
		t.Fatal(err)
	}

	got := parseShortName(shortName[:])
	want := "SOMELO~2.TXT"

	if got != want {
		t.Fatalf("short name = %q, want %q", got, want)
	}
}

func TestWriteFileLongNameRequiresContiguousDirectorySlots(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
	)

	data := make([]byte, dataOffset+4*bytesPerCluster)

	binary.LittleEndian.PutUint32(
		data[fatOffset+rootCluster*4:],
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
			TotalSectors:      5,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	// Fill the entire directory with occupied entries, except
	// for the first slot. A long filename requires two contiguous
	// slots (LFN + short entry), so the single free slot is not enough.
	root := make([]byte, bytesPerCluster)

	for offset := 0; offset < bytesPerCluster; offset += 32 {
		root[offset] = 0x20 // occupied entry
		root[offset+0x0B] = 0x20
	}

	root[0] = 0xE5 // single deleted/free entry

	if err := fs.writeCluster(rootCluster, root); err != nil {
		t.Fatal(err)
	}

	err := fs.WriteFile(
		"Some long filename.txt",
		[]byte("data"),
	)
	if err == nil {
		t.Fatal("expected directory slot allocation to fail")
	}

	if !strings.Contains(err.Error(), "no run of 3 free entries") {
		t.Fatalf("unexpected error: %v", err)
	}

	// The file data allocation must have been rolled back.
	free, err := fs.IsFreeCluster(3)
	if err != nil {
		t.Fatal(err)
	}

	if !free {
		t.Fatal("expected allocated data cluster to be rolled back")
	}
}

func TestWriteFileLongNameAcrossDirectoryClusterBoundary(t *testing.T) {
	// Test writing a long filename whose LFN record crosses a directory-cluster boundary

	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
		secondCluster   = uint32(3)
	)

	data := make([]byte, dataOffset+5*bytesPerCluster)

	// Root directory spans clusters 2 -> 3.
	binary.LittleEndian.PutUint32(
		data[fatOffset+rootCluster*4:],
		secondCluster,
	)
	binary.LittleEndian.PutUint32(
		data[fatOffset+secondCluster*4:],
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
			TotalSectors:      6,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	// Fill all but the final directory entry of cluster 2.
	// The LFN record will therefore start at the final slot
	// of cluster 2 and continue into cluster 3.
	first := make([]byte, bytesPerCluster)

	for offset := 0; offset < bytesPerCluster-32; offset += 32 {
		first[offset] = 0x20
		first[offset+0x0B] = 0x20
	}

	if err := fs.writeCluster(rootCluster, first); err != nil {
		t.Fatal(err)
	}

	// Make the first slot of cluster 3 available and mark the
	// following entries as occupied so there are exactly enough
	// contiguous slots across the cluster boundary.
	second := make([]byte, bytesPerCluster)

	for offset := 0; offset < bytesPerCluster; offset += 32 {
		second[offset] = 0x20
		second[offset+0x0B] = 0x20
	}

	if err := fs.writeCluster(secondCluster, second); err != nil {
		t.Fatal(err)
	}

	// We need three entries for this name:
	//   LFN + LFN + short entry.
	//
	// Make the final slot of cluster 2 and first two slots of
	// cluster 3 free.
	first[bytesPerCluster-32] = 0xE5
	if err := fs.writeCluster(rootCluster, first); err != nil {
		t.Fatal(err)
	}

	second[0] = 0xE5
	second[32] = 0xE5
	if err := fs.writeCluster(secondCluster, second); err != nil {
		t.Fatal(err)
	}

	name := "Some long filename.txt"
	content := []byte("hello across clusters")

	if err := fs.WriteFile("/"+name, content); err != nil {
		t.Fatal(err)
	}

	record, err := fs.findDirectoryEntry(rootCluster, name)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatalf("file %q was not found", name)
	}

	if record.Entry.Name != name {
		t.Fatalf(
			"filename = %q, want %q",
			record.Entry.Name,
			name,
		)
	}

	if record.Entry.Size != uint32(len(content)) {
		t.Fatalf(
			"size = %d, want %d",
			record.Entry.Size,
			len(content),
		)
	}

	if record.Short.Cluster != secondCluster ||
		record.Short.Offset != 32 {
		t.Fatalf(
			"short entry location = cluster %d offset %d, want cluster %d offset %d",
			record.Short.Cluster,
			record.Short.Offset,
			secondCluster,
			32,
		)
	}

	got, err := (&File{
		entry: record.Entry,
		fs:    fs,
	}).ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, content) {
		t.Fatalf("file content = %q, want %q", got, content)
	}
}

type failOnClusterWriteAt struct {
	data       []byte
	failOffset int64
}

func (w *failOnClusterWriteAt) WriteAt(p []byte, off int64) (int, error) {
	if off == int64(w.failOffset) {
		return 0, errors.New("injected directory write failure")
	}

	if off < 0 || off+int64(len(p)) > int64(len(w.data)) {
		return 0, io.ErrShortBuffer
	}

	copy(w.data[off:off+int64(len(p))], p)
	return len(p), nil
}

func TestWriteFileRollsBackWhenDirectoryWriteFails(t *testing.T) {
	// Test that a directory-write failure doesn't leave the newly allocated file cluster orphaned.

	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
		dataCluster     = uint32(3)
	)

	data := make([]byte, dataOffset+4*bytesPerCluster)

	// Root directory cluster is allocated.
	binary.LittleEndian.PutUint32(
		data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	// Data cluster 3 is initially free.
	binary.LittleEndian.PutUint32(
		data[fatOffset+dataCluster*4:],
		0,
	)

	writer := &failOnClusterWriteAt{
		data: append([]byte(nil), data...),

		// Fail when the root directory cluster is written.
		failOffset: int64(dataOffset + int(rootCluster-2)*bytesPerCluster),
	}

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: writer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			ReservedSectors:   0,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      5,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	name := "hello.txt"
	content := []byte("hello")

	err := fs.WriteFile(name, content)
	if err == nil {
		t.Fatal("expected directory write to fail")
	}

	if !strings.Contains(err.Error(), "injected directory write failure") {
		t.Fatalf("unexpected error: %v", err)
	}

	// The newly allocated data cluster must have been freed.
	gotFAT, err := fs.readFATEntry(dataCluster)
	if err != nil {
		t.Fatal(err)
	}

	if gotFAT != 0 {
		t.Fatalf(
			"data cluster %d still allocated with FAT value %#x",
			dataCluster,
			gotFAT,
		)
	}

	// The directory entry must not have been created.
	record, err := fs.findDirectoryEntry(rootCluster, name)
	if err != nil {
		t.Fatal(err)
	}

	if record != nil {
		t.Fatalf("file %q exists after failed write", name)
	}
}

type failOnWriteCountAt struct {
	data       []byte
	failOffset int64
	failCount  int
	writes     int
}

func (w *failOnWriteCountAt) WriteAt(p []byte, off int64) (int, error) {
	if off == w.failOffset {
		w.writes++

		if w.writes == w.failCount {
			return 0, errors.New("injected FAT rollback failure")
		}
	}

	if off < 0 || off+int64(len(p)) > int64(len(w.data)) {
		return 0, io.ErrShortBuffer
	}

	copy(w.data[off:off+int64(len(p))], p)
	return len(p), nil
}

type failDirectoryAndFATRollbackBuffer struct {
	data []byte

	directoryOffset int64
	fatOffset       int64

	failDirectoryWrite bool
	failFATWriteCount  int
	fatWrites          int
}

func (b *failDirectoryAndFATRollbackBuffer) ReadAt(
	p []byte,
	off int64,
) (int, error) {
	if off < 0 || off+int64(len(p)) > int64(len(b.data)) {
		return 0, io.EOF
	}

	copy(p, b.data[off:off+int64(len(p))])
	return len(p), nil
}

func (b *failDirectoryAndFATRollbackBuffer) WriteAt(
	p []byte,
	off int64,
) (int, error) {
	if off == b.directoryOffset && b.failDirectoryWrite {
		return 0, errors.New("injected directory write failure")
	}

	if off == b.fatOffset {
		b.fatWrites++

		if b.failFATWriteCount > 0 &&
			b.fatWrites == b.failFATWriteCount {
			return 0, errors.New("injected FAT rollback failure")
		}
	}

	if off < 0 || off+int64(len(p)) > int64(len(b.data)) {
		return 0, io.ErrShortBuffer
	}

	copy(b.data[off:off+int64(len(p))], p)
	return len(p), nil
}

func TestWriteFileReportsRollbackFailure(t *testing.T) {
	const (
		bytesPerSector    = 512
		sectorsPerCluster = 1
		reservedSectors   = 1
		numberOfFATs      = 2
		sectorsPerFAT     = 1
		rootCluster       = 2
	)

	fatOffset := int64(reservedSectors * bytesPerSector)
	dataOffset := int64(
		(reservedSectors +
			numberOfFATs*sectorsPerFAT) * bytesPerSector,
	)

	data := make([]byte, 8*bytesPerSector)

	// Mark the root directory cluster as end-of-chain in both FATs.
	for fat := 0; fat < numberOfFATs; fat++ {
		offset := fatOffset +
			int64(fat*sectorsPerFAT*bytesPerSector) +
			int64(rootCluster*4)

		binary.LittleEndian.PutUint32(
			data[offset:offset+4],
			fat32EOF,
		)
	}

	layout := Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: bytesPerSector * sectorsPerCluster,
	}

	boot := BootSector{
		BytesPerSector:    bytesPerSector,
		SectorsPerCluster: sectorsPerCluster,
		ReservedSectors:   reservedSectors,
		NumberOfFATs:      numberOfFATs,
		SectorsPerFAT:     sectorsPerFAT,
		TotalSectors:      uint32(len(data) / bytesPerSector),
		RootCluster:       rootCluster,
	}

	buffer := &failDirectoryAndFATRollbackBuffer{
		data:               data,
		directoryOffset:    dataOffset,
		fatOffset:          fatOffset,
		failDirectoryWrite: true,
		failFATWriteCount:  2,
	}

	fs := &Filesystem{
		reader: buffer,
		writer: buffer,
		layout: layout,
		boot:   boot,
	}

	err := fs.WriteFile("/hello.txt", []byte("hello"))
	if err == nil {
		t.Fatal("expected WriteFile to fail")
	}

	if !strings.Contains(err.Error(), "injected directory write failure") {
		t.Fatalf("missing directory write error: %v", err)
	}

	if !strings.Contains(err.Error(), "injected FAT rollback failure") {
		t.Fatalf("missing rollback error: %v", err)
	}

	value, err := fs.readFATEntry(3)
	if err != nil {
		t.Fatalf("read FAT entry: %v", err)
	}

	if value == 0 {
		t.Fatal("expected cluster 3 to remain allocated after rollback failure")
	}
}

func TestWriteFileReportsPartialChainRollbackFailure(t *testing.T) {
	// Makes the file span two clusters
	// Fails the directory write
	// then fails while freeing the second cluster during rollback
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
		dataCluster1    = uint32(3)
		dataCluster2    = uint32(4)
	)

	buffer := &failDirectoryAndFATRollbackBuffer{
		data: make([]byte, dataOffset+5*bytesPerCluster),

		directoryOffset: int64(dataOffset),
		fatOffset:       int64(fatOffset),
	}

	// Root directory cluster is allocated.
	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	// Data clusters 3 and 4 are initially free.
	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+dataCluster1*4:],
		0,
	)
	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+dataCluster2*4:],
		0,
	)

	// We need a writer that:
	//   1. allows the two FAT writes used to allocate clusters 3 and 4;
	//   2. fails the directory write;
	//   3. allows the first rollback FAT write;
	//   4. fails the second rollback FAT write.
	buffer.failDirectoryWrite = true
	buffer.failFATWriteCount = 5

	fs := &Filesystem{
		reader: buffer,
		writer: buffer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			ReservedSectors:   0,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      6,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	content := make([]byte, bytesPerCluster+1)

	err := fs.WriteFile("hello.txt", content)
	if err == nil {
		t.Fatal("expected WriteFile to fail")
	}

	if !strings.Contains(
		err.Error(),
		"injected directory write failure",
	) {
		t.Fatalf("missing directory write error: %v", err)
	}

	if !strings.Contains(
		err.Error(),
		"injected FAT rollback failure",
	) {
		t.Fatalf("missing rollback error: %v", err)
	}

	// The first cluster should have been freed before the second
	// rollback write failed.
	cluster1FAT, err := fs.readFATEntry(dataCluster1)
	if err != nil {
		t.Fatal(err)
	}

	if cluster1FAT != 0 {
		t.Fatalf(
			"cluster %d FAT entry = %#x, want 0",
			dataCluster1,
			cluster1FAT,
		)
	}
}


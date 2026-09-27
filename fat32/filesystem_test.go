package fat32

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
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
	data := make([]byte, 6656) // 4096 + 2560 (layout.DataOffset)

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

	// Create a long filename whose generated short name is SOMELO~1.TXT.
	if err := fs.WriteFile(
		"Some long filename.txt",
		[]byte("first"),
	); err != nil {
		t.Fatalf("first WriteFile failed: %v", err)
	}

	// This is a different long filename, but its generated alias would
	// also be SOMELO~1.TXT. It must therefore receive ~2.
	if err := fs.WriteFile(
		"Some long file-name.txt",
		[]byte("second"),
	); err != nil {
		t.Fatalf("second WriteFile failed: %v", err)
	}

	records, err := fs.readDirectoryRecords(rootCluster)
	if err != nil {
		t.Fatal(err)
	}

	expected := map[string]string{
		"Some long filename.txt":  "SOMELO~1.TXT",
		"Some long file-name.txt": "SOMELO~2.TXT",
	}

	for _, record := range records {
		want, ok := expected[record.Entry.Name]
		if !ok {
			continue
		}

		if record.Entry.ShortName != want {
			t.Fatalf(
				"%q: short name = %q, want %q",
				record.Entry.Name,
				record.Entry.ShortName,
				want,
			)
		}

		delete(expected, record.Entry.Name)
	}

	if len(expected) != 0 {
		t.Fatalf("missing directory entries: %v", expected)
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

// type failDirectoryAndFATRollbackBuffer struct {
// 	data []byte
//
// 	directoryOffset         int64
// 	failDirectoryWrite      bool
// 	failDirectoryWriteCount int
// 	directoryWrites         int
//
// 	fatOffset         int64
// 	failFATWriteCount int
// 	fatWrites         int
// }
//
// func (b *failDirectoryAndFATRollbackBuffer) ReadAt(
// 	p []byte,
// 	off int64,
// ) (int, error) {
// 	if off < 0 || off+int64(len(p)) > int64(len(b.data)) {
// 		return 0, io.EOF
// 	}
//
// 	copy(p, b.data[off:off+int64(len(p))])
// 	return len(p), nil
// }
//
// func (b *failDirectoryAndFATRollbackBuffer) WriteAt(
// 	p []byte,
// 	off int64,
// ) (int, error) {
// 	if off == b.directoryOffset {
// 		b.directoryWrites++
//
// 		if b.failDirectoryWrite ||
// 			(b.failDirectoryWriteCount > 0 &&
// 				b.directoryWrites == b.failDirectoryWriteCount) {
// 			return 0, errors.New(
// 				"injected directory write failure",
// 			)
// 		}
// 	}
//
// 	if off == b.fatOffset {
// 		b.fatWrites++
//
// 		if b.failFATWriteCount > 0 &&
// 			b.fatWrites == b.failFATWriteCount {
// 			return 0, errors.New(
// 				"injected FAT rollback failure",
// 			)
// 		}
// 	}
//
// 	if off < 0 ||
// 		off+int64(len(p)) > int64(len(b.data)) {
// 		return 0, io.ErrShortBuffer
// 	}
//
// 	copy(b.data[off:off+int64(len(p))], p)
//
// 	return len(p), nil
// }

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

func TestWriteFileInSubdirectory(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
		dirCluster      = uint32(3)
	)

	buffer := &failDirectoryAndFATRollbackBuffer{
		data: make([]byte, dataOffset+10*bytesPerCluster),
	}

	// Root directory cluster 2 -> end of chain.
	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	// Directory cluster 3 -> end of chain.
	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+dirCluster*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: buffer,
		writer: buffer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			ReservedSectors:   0,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      10,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	// Create the subdirectory entry in the root directory.
	dirEntry := encodeDirectoryEntry(
		[11]byte{'M', 'U', 'S', 'I', 'C', ' ', ' ', ' ', ' ', ' ', ' '},
		0x10,
		dirCluster,
		0,
	)

	root := make([]byte, bytesPerCluster)
	copy(root[:32], dirEntry[:])

	if err := fs.writeCluster(rootCluster, root); err != nil {
		t.Fatalf("write root directory: %v", err)
	}

	content := []byte("hello")

	if err := fs.WriteFile("/MUSIC/hello.txt", content); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entry, err := fs.findDirectoryEntry(dirCluster, "hello.txt")
	if err != nil {
		t.Fatalf("find directory entry: %v", err)
	}

	if entry == nil {
		t.Fatal("hello.txt was not created")
	}

	if entry.Entry.Size != uint32(len(content)) {
		t.Fatalf(
			"file size = %d, want %d",
			entry.Entry.Size,
			len(content),
		)
	}

	file := &File{
		entry: entry.Entry,
		fs:    fs,
	}

	readBack, err := file.ReadAll()
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	if !bytes.Equal(readBack, content) {
		t.Fatalf(
			"file contents = %q, want %q",
			readBack,
			content,
		)
	}
}

func TestWriteFileInNestedSubdirectory(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
		musicCluster    = uint32(3)
		cartoonsCluster = uint32(4)
	)

	buffer := &failDirectoryAndFATRollbackBuffer{
		data: make([]byte, dataOffset+10*bytesPerCluster),
	}

	// Root directory cluster 2 -> end of chain.
	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	// /MUSIC directory cluster 3 -> end of chain.
	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+musicCluster*4:],
		fat32EOF,
	)

	// /MUSIC/Cartoons directory cluster 4 -> end of chain.
	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+cartoonsCluster*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: buffer,
		writer: buffer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			ReservedSectors:   0,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      10,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	// Create /MUSIC in the root directory.
	musicEntry := encodeDirectoryEntry(
		[11]byte{'M', 'U', 'S', 'I', 'C', ' ', ' ', ' ', ' ', ' ', ' '},
		0x10,
		musicCluster,
		0,
	)

	root := make([]byte, bytesPerCluster)
	copy(root[:32], musicEntry[:])

	if err := fs.writeCluster(rootCluster, root); err != nil {
		t.Fatalf("write root directory: %v", err)
	}

	// Create /MUSIC/Cartoons.
	cartoonsEntry := encodeDirectoryEntry(
		[11]byte{'C', 'A', 'R', 'T', 'O', 'O', 'N', 'S', ' ', ' ', ' '},
		0x10,
		cartoonsCluster,
		0,
	)

	music := make([]byte, bytesPerCluster)
	copy(music[:32], cartoonsEntry[:])

	if err := fs.writeCluster(musicCluster, music); err != nil {
		t.Fatalf("write MUSIC directory: %v", err)
	}

	content := []byte("hello from nested directory")

	if err := fs.WriteFile(
		"/MUSIC/Cartoons/hello.txt",
		content,
	); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entry, err := fs.findDirectoryEntry(
		cartoonsCluster,
		"hello.txt",
	)
	if err != nil {
		t.Fatalf("find directory entry: %v", err)
	}

	if entry == nil {
		t.Fatal("hello.txt was not created")
	}

	if entry.Entry.Size != uint32(len(content)) {
		t.Fatalf(
			"file size = %d, want %d",
			entry.Entry.Size,
			len(content),
		)
	}

	file := &File{
		entry: entry.Entry,
		fs:    fs,
	}

	readBack, err := file.ReadAll()
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	if !bytes.Equal(readBack, content) {
		t.Fatalf(
			"file contents = %q, want %q",
			readBack,
			content,
		)
	}
}

func TestWriteFileExpandsFullDirectory(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
		dirCluster      = uint32(3)
		newDirCluster   = uint32(4)
	)

	buffer := &failDirectoryAndFATRollbackBuffer{
		data: make([]byte, dataOffset+10*bytesPerCluster),
	}

	// Root directory cluster 2 -> end of chain.
	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	// /MUSIC directory cluster 3 -> currently end of chain.
	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+dirCluster*4:],
		fat32EOF,
	)

	// Cluster 4 is free and will be used to extend /MUSIC.
	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+newDirCluster*4:],
		0,
	)

	fs := &Filesystem{
		reader: buffer,
		writer: buffer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			ReservedSectors:   0,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      10,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	// Create /MUSIC in the root directory.
	musicEntry := encodeDirectoryEntry(
		[11]byte{'M', 'U', 'S', 'I', 'C', ' ', ' ', ' ', ' ', ' ', ' '},
		0x10,
		dirCluster,
		0,
	)

	root := make([]byte, bytesPerCluster)
	copy(root[:32], musicEntry[:])

	if err := fs.writeCluster(rootCluster, root); err != nil {
		t.Fatalf("write root directory: %v", err)
	}

	// Fill every slot in /MUSIC with valid short directory entries.
	music := make([]byte, bytesPerCluster)

	for i := 0; i < bytesPerCluster/32; i++ {
		name := fmt.Sprintf("FILE%04d.TXT", i)

		shortName, err := encodeShortName(name)
		if err != nil {
			t.Fatalf("encode short name %q: %v", name, err)
		}

		entry := encodeDirectoryEntry(
			shortName,
			0x20,
			0,
			0,
		)

		copy(music[i*32:(i+1)*32], entry[:])
	}

	if err := fs.writeCluster(dirCluster, music); err != nil {
		t.Fatalf("write MUSIC directory: %v", err)
	}

	content := []byte("hello")

	if err := fs.WriteFile("/MUSIC/hello.txt", content); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// The original directory cluster should now point to the new cluster.
	nextCluster, err := fs.readFATEntry(dirCluster)
	if err != nil {
		t.Fatalf("read directory FAT entry: %v", err)
	}

	if nextCluster < 2 {
		t.Fatalf(
			"directory cluster %d points to invalid cluster %d",
			dirCluster,
			nextCluster,
		)
	}

	if nextCluster == dirCluster {
		t.Fatalf(
			"directory cluster %d points to itself",
			dirCluster,
		)
	}

	// The new directory cluster should contain the file.
	entry, err := fs.findDirectoryEntry(
		nextCluster,
		"hello.txt",
	)
	if err != nil {
		t.Fatalf("find directory entry: %v", err)
	}

	if entry == nil {
		t.Fatal("hello.txt was not created in expanded directory")
	}

	if entry.Entry.Size != uint32(len(content)) {
		t.Fatalf(
			"file size = %d, want %d",
			entry.Entry.Size,
			len(content),
		)
	}

	file := &File{
		entry: entry.Entry,
		fs:    fs,
	}

	readBack, err := file.ReadAll()
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	if !bytes.Equal(readBack, content) {
		t.Fatalf(
			"file contents = %q, want %q",
			readBack,
			content,
		)
	}
}

func TestWriteFileExpansionFailureRollsBack(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
		dirCluster      = uint32(3)
	)

	buffer := &failDirectoryAndFATRollbackBuffer{
		data: make([]byte, dataOffset+10*bytesPerCluster),
	}

	// Root directory cluster 2 -> end of chain.
	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	// /MUSIC directory cluster 3 -> end of chain.
	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+dirCluster*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: buffer,
		writer: buffer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			ReservedSectors:   0,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      10,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	// Create /MUSIC in the root directory.
	musicEntry := encodeDirectoryEntry(
		[11]byte{'M', 'U', 'S', 'I', 'C', ' ', ' ', ' ', ' ', ' ', ' '},
		0x10,
		dirCluster,
		0,
	)

	root := make([]byte, bytesPerCluster)
	copy(root[:32], musicEntry[:])

	if err := fs.writeCluster(rootCluster, root); err != nil {
		t.Fatalf("write root directory: %v", err)
	}

	// Fill /MUSIC completely.
	music := make([]byte, bytesPerCluster)

	for i := 0; i < bytesPerCluster/32; i++ {
		name := fmt.Sprintf("FILE%04d.TXT", i)

		shortName, err := encodeShortName(name)
		if err != nil {
			t.Fatalf("encode short name %q: %v", name, err)
		}

		entry := encodeDirectoryEntry(
			shortName,
			0x20,
			0,
			0,
		)

		copy(music[i*32:(i+1)*32], entry[:])
	}

	if err := fs.writeCluster(dirCluster, music); err != nil {
		t.Fatalf("write MUSIC directory: %v", err)
	}

	// The first FAT write made by WriteFile is allocation of the
	// file-data cluster. The second is the directory expansion
	// cluster allocation. The third links the old directory cluster
	// to the new directory cluster.
	//
	// We don't want to fail those FAT writes. Instead, fail the
	// directory-cluster initialization write by making it the
	// injected directory write.
	buffer.failDirectoryWrite = true
	buffer.directoryOffset = dataOffset + 2*bytesPerCluster

	err := fs.WriteFile(
		"/MUSIC/hello.txt",
		[]byte("hello"),
	)
	if err == nil {
		t.Fatal("expected WriteFile to fail")
	}

	if !strings.Contains(
		err.Error(),
		"injected directory write failure",
	) {
		t.Fatalf(
			"missing directory expansion write error: %v",
			err,
		)
	}

	// The original directory must still be the end of its chain.
	value, err := fs.readFATEntry(dirCluster)
	if err != nil {
		t.Fatalf("read directory FAT entry: %v", err)
	}

	if value != fat32EOF {
		t.Fatalf(
			"directory cluster %d FAT entry = %#x, want EOF",
			dirCluster,
			value,
		)
	}

	// There must be no file entry left behind.
	entry, err := fs.findDirectoryEntry(
		dirCluster,
		"hello.txt",
	)
	if err != nil {
		t.Fatalf("find directory entry: %v", err)
	}

	if entry != nil {
		t.Fatal("file directory entry exists after failed expansion")
	}
}

func TestWriteFileLongNameExpandsDirectoryAcrossMultipleClusters(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
		dirCluster      = uint32(3)
	)

	buffer := &failDirectoryAndFATRollbackBuffer{
		data: make([]byte, dataOffset+20*bytesPerCluster),
	}

	// Root directory cluster 2 -> end of chain.
	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	// /MUSIC directory cluster 3 -> end of chain.
	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+dirCluster*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: buffer,
		writer: buffer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			ReservedSectors:   0,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      20,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	// Create /MUSIC in the root directory.
	musicEntry := encodeDirectoryEntry(
		[11]byte{'M', 'U', 'S', 'I', 'C', ' ', ' ', ' ', ' ', ' ', ' '},
		0x10,
		dirCluster,
		0,
	)

	root := make([]byte, bytesPerCluster)
	copy(root[:32], musicEntry[:])

	if err := fs.writeCluster(rootCluster, root); err != nil {
		t.Fatalf("write root directory: %v", err)
	}

	// Fill the entire /MUSIC directory cluster.
	music := make([]byte, bytesPerCluster)

	for i := 0; i < bytesPerCluster/32; i++ {
		name := fmt.Sprintf("FILE%04d.TXT", i)

		shortName, err := encodeShortName(name)
		if err != nil {
			t.Fatalf("encode short name %q: %v", name, err)
		}

		entry := encodeDirectoryEntry(
			shortName,
			0x20,
			0,
			0,
		)

		copy(music[i*32:(i+1)*32], entry[:])
	}

	if err := fs.writeCluster(dirCluster, music); err != nil {
		t.Fatalf("write MUSIC directory: %v", err)
	}

	// 208 ASCII characters require 16 LFN entries plus one
	// short entry, i.e. 17 directory slots.
	name := strings.Repeat("a", 208) + ".txt"
	content := []byte("hello")

	if err := fs.WriteFile(
		"/MUSIC/"+name,
		content,
	); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// The original directory cluster must now point to an
	// expanded directory chain.
	firstNewCluster, err := fs.readFATEntry(dirCluster)
	if err != nil {
		t.Fatalf("read directory FAT entry: %v", err)
	}

	if firstNewCluster < 2 || firstNewCluster == fat32EOF {
		t.Fatalf(
			"directory cluster %d points to invalid cluster %#x",
			dirCluster,
			firstNewCluster,
		)
	}

	// Follow the expanded directory chain.
	chain, err := fs.readClusterChain(dirCluster)
	if err != nil {
		t.Fatalf("read expanded directory chain: %v", err)
	}

	if len(chain) < 3 {
		t.Fatalf(
			"expanded directory chain has %d clusters, want at least 3",
			len(chain),
		)
	}

	// The long filename should be readable from the expanded
	// directory chain.
	entry, err := fs.findDirectoryEntry(dirCluster, name)
	if err != nil {
		t.Fatalf("find directory entry: %v", err)
	}

	if entry == nil {
		t.Fatal("long filename was not created")
	}

	if entry.Entry.Size != uint32(len(content)) {
		t.Fatalf(
			"file size = %d, want %d",
			entry.Entry.Size,
			len(content),
		)
	}

	file := &File{
		entry: entry.Entry,
		fs:    fs,
	}

	readBack, err := file.ReadAll()
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	if !bytes.Equal(readBack, content) {
		t.Fatalf(
			"file contents = %q, want %q",
			readBack,
			content,
		)
	}
}

func TestWriteFileMultiClusterExpansionFailureRollsBack(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
		dirCluster      = uint32(3)
	)

	buffer := &failDirectoryAndFATRollbackBuffer{
		data: make([]byte, dataOffset+20*bytesPerCluster),
	}

	// Root directory cluster 2 -> end of chain.
	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	// /MUSIC directory cluster 3 -> end of chain.
	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+dirCluster*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: buffer,
		writer: buffer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			ReservedSectors:   0,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      20,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	// Create /MUSIC in the root directory.
	musicEntry := encodeDirectoryEntry(
		[11]byte{'M', 'U', 'S', 'I', 'C', ' ', ' ', ' ', ' ', ' ', ' '},
		0x10,
		dirCluster,
		0,
	)

	root := make([]byte, bytesPerCluster)
	copy(root[:32], musicEntry[:])

	if err := fs.writeCluster(rootCluster, root); err != nil {
		t.Fatalf("write root directory: %v", err)
	}

	// Fill the entire /MUSIC directory cluster.
	music := make([]byte, bytesPerCluster)

	for i := 0; i < bytesPerCluster/32; i++ {
		name := fmt.Sprintf("FILE%04d.TXT", i)

		shortName, err := encodeShortName(name)
		if err != nil {
			t.Fatalf("encode short name %q: %v", name, err)
		}

		entry := encodeDirectoryEntry(
			shortName,
			0x20,
			0,
			0,
		)

		copy(music[i*32:(i+1)*32], entry[:])
	}

	if err := fs.writeCluster(dirCluster, music); err != nil {
		t.Fatalf("write MUSIC directory: %v", err)
	}

	// 208 ASCII characters require 16 LFN entries plus
	// one short entry, so expansion needs two clusters.
	name := strings.Repeat("a", 208) + ".txt"

	// Fail while initializing the second newly allocated
	// directory cluster.
	buffer.directoryOffset =
		dataOffset + 2*bytesPerCluster
	buffer.failDirectoryWrite = true

	err := fs.WriteFile(
		"/MUSIC/"+name,
		[]byte("hello"),
	)
	if err == nil {
		t.Fatal("expected WriteFile to fail")
	}

	if !strings.Contains(
		err.Error(),
		"injected directory write failure",
	) {
		t.Fatalf(
			"missing directory write error: %v",
			err,
		)
	}

	// The original directory cluster must still be the end
	// of the directory chain.
	value, err := fs.readFATEntry(dirCluster)
	if err != nil {
		t.Fatalf("read directory FAT entry: %v", err)
	}

	if value != fat32EOF {
		t.Fatalf(
			"directory cluster %d FAT entry = %#x, want EOF",
			dirCluster,
			value,
		)
	}

	// The newly allocated directory clusters must have been
	// freed by rollback.
	for cluster := uint32(4); cluster <= 5; cluster++ {
		value, err := fs.readFATEntry(cluster)
		if err != nil {
			t.Fatalf(
				"read FAT entry for cluster %d: %v",
				cluster,
				err,
			)
		}

		if value != 0 {
			t.Fatalf(
				"cluster %d FAT entry = %#x, want free",
				cluster,
				value,
			)
		}
	}

	// The file data cluster allocated before directory creation
	// must also have been rolled back.
	value, err = fs.readFATEntry(6)
	if err != nil {
		t.Fatalf("read file data FAT entry: %v", err)
	}

	if value != 0 {
		t.Fatalf(
			"file data cluster 6 FAT entry = %#x, want free",
			value,
		)
	}

	entry, err := fs.findDirectoryEntry(dirCluster, name)
	if err != nil {
		t.Fatalf("find failed file: %v", err)
	}

	if entry != nil {
		t.Fatal("failed file should not exist in directory")
	}
}

func TestWriteFileExpansionAvoidsShortNameCollision(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
		dirCluster      = uint32(3)
	)

	buffer := &failDirectoryAndFATRollbackBuffer{
		data: make([]byte, dataOffset+20*bytesPerCluster),
	}

	// Root directory cluster 2 -> end of chain.
	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	// /MUSIC directory cluster 3 -> end of chain.
	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+dirCluster*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: buffer,
		writer: buffer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			ReservedSectors:   0,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      20,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	// Create /MUSIC in the root directory.
	musicEntry := encodeDirectoryEntry(
		[11]byte{'M', 'U', 'S', 'I', 'C', ' ', ' ', ' ', ' ', ' ', ' '},
		0x10,
		dirCluster,
		0,
	)

	root := make([]byte, bytesPerCluster)
	copy(root[:32], musicEntry[:])

	if err := fs.writeCluster(rootCluster, root); err != nil {
		t.Fatalf("write root directory: %v", err)
	}

	// The long filename below would normally generate
	// SOMELO~1.TXT. Put that short name in the full directory
	// first so the writer must select SOMELO~2.TXT.
	collidingName := "Some long filename.txt"

	music := make([]byte, bytesPerCluster)

	shortName, err := encodeShortName("SOMELO~1.TXT")
	if err != nil {
		t.Fatalf("encode colliding short name: %v", err)
	}

	collidingEntry := encodeDirectoryEntry(
		shortName,
		0x20,
		0,
		0,
	)

	copy(music[:32], collidingEntry[:])

	// Fill the remaining entries.
	for i := 1; i < bytesPerCluster/32; i++ {
		name := fmt.Sprintf("FILE%04d.TXT", i)

		shortName, err := encodeShortName(name)
		if err != nil {
			t.Fatalf("encode short name %q: %v", name, err)
		}

		entry := encodeDirectoryEntry(
			shortName,
			0x20,
			0,
			0,
		)

		copy(music[i*32:(i+1)*32], entry[:])
	}

	if err := fs.writeCluster(dirCluster, music); err != nil {
		t.Fatalf("write MUSIC directory: %v", err)
	}

	if err := fs.WriteFile(
		"/MUSIC/"+collidingName,
		[]byte("hello"),
	); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entry, err := fs.findDirectoryEntry(
		dirCluster,
		collidingName,
	)
	if err != nil {
		t.Fatalf("find directory entry: %v", err)
	}

	if entry == nil {
		t.Fatal("file was not created")
	}

	if entry.Entry.ShortName != "SOMELO~2.TXT" {
		t.Fatalf(
			"short name = %q, want %q",
			entry.Entry.ShortName,
			"SOMELO~2.TXT",
		)
	}

	chain, err := fs.readClusterChain(dirCluster)
	if err != nil {
		t.Fatalf("read expanded directory chain: %v", err)
	}

	if len(chain) < 2 {
		t.Fatalf(
			"directory chain has %d clusters, want at least 2",
			len(chain),
		)
	}
}

func TestWriteFileDoesNotExpandDirectoryWithScatteredFreeSlots(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
		dirCluster      = uint32(3)
	)

	buffer := &failDirectoryAndFATRollbackBuffer{
		data: make([]byte, dataOffset+20*bytesPerCluster),
	}

	// Root directory cluster 2 -> end of chain.
	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	// /MUSIC directory cluster 3 -> end of chain.
	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+dirCluster*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: buffer,
		writer: buffer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			ReservedSectors:   0,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      20,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	// Create /MUSIC in the root directory.
	musicEntry := encodeDirectoryEntry(
		[11]byte{'M', 'U', 'S', 'I', 'C', ' ', ' ', ' ', ' ', ' ', ' '},
		0x10,
		dirCluster,
		0,
	)

	root := make([]byte, bytesPerCluster)
	copy(root[:32], musicEntry[:])

	if err := fs.writeCluster(rootCluster, root); err != nil {
		t.Fatalf("write root directory: %v", err)
	}

	// Leave isolated free entries, but never enough contiguous
	// entries for the long filename.
	music := make([]byte, bytesPerCluster)

	for i := 0; i < bytesPerCluster/32; i++ {
		if i == 2 || i == 5 || i == 8 || i == 11 {
			music[i*32] = 0xE5
			continue
		}

		name := fmt.Sprintf("FILE%04d.TXT", i)

		shortName, err := encodeShortName(name)
		if err != nil {
			t.Fatalf("encode short name %q: %v", name, err)
		}

		entry := encodeDirectoryEntry(
			shortName,
			0x20,
			0,
			0,
		)

		copy(music[i*32:(i+1)*32], entry[:])
	}

	if err := fs.writeCluster(dirCluster, music); err != nil {
		t.Fatalf("write MUSIC directory: %v", err)
	}

	// This requires multiple LFN entries, so the isolated free
	// slots are insufficient.
	name := strings.Repeat("a", 30) + ".txt"

	err := fs.WriteFile(
		"/MUSIC/"+name,
		[]byte("hello"),
	)
	if err == nil {
		t.Fatal("expected WriteFile to fail")
	}

	if !strings.Contains(
		err.Error(),
		"no contiguous free slots",
	) {
		t.Fatalf(
			"unexpected error: %v",
			err,
		)
	}

	// The directory must not have been expanded.
	value, err := fs.readFATEntry(dirCluster)
	if err != nil {
		t.Fatalf("read directory FAT entry: %v", err)
	}

	if value != fat32EOF {
		t.Fatalf(
			"directory cluster %d FAT entry = %#x, want EOF",
			dirCluster,
			value,
		)
	}

	// The file data allocated before directory creation must
	// have been rolled back.
	value, err = fs.readFATEntry(4)
	if err != nil {
		t.Fatalf("read file data FAT entry: %v", err)
	}

	if value != 0 {
		t.Fatalf(
			"file data cluster 4 FAT entry = %#x, want free",
			value,
		)
	}
}

func TestWriteLongFilenameToEmptyDirectory(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
		dirCluster      = uint32(3)
	)

	buffer := &failDirectoryAndFATRollbackBuffer{
		data: make([]byte, dataOffset+20*bytesPerCluster),
	}

	// Root directory cluster 2 -> end of chain.
	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	// /MUSIC directory cluster 3 -> end of chain.
	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+dirCluster*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: buffer,
		writer: buffer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			ReservedSectors:   0,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      20,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	// Create /MUSIC in the root directory.
	musicEntry := encodeDirectoryEntry(
		[11]byte{'M', 'U', 'S', 'I', 'C', ' ', ' ', ' ', ' ', ' ', ' '},
		0x10,
		dirCluster,
		0,
	)

	root := make([]byte, bytesPerCluster)
	copy(root[:32], musicEntry[:])

	if err := fs.writeCluster(rootCluster, root); err != nil {
		t.Fatalf("write root directory: %v", err)
	}

	// The directory is completely empty: the first byte is 0x00.
	music := make([]byte, bytesPerCluster)

	if err := fs.writeCluster(dirCluster, music); err != nil {
		t.Fatalf("write MUSIC directory: %v", err)
	}

	name := strings.Repeat("a", 30) + ".txt"
	content := []byte("hello")

	if err := fs.WriteFile(
		"/MUSIC/"+name,
		content,
	); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entry, err := fs.findDirectoryEntry(
		dirCluster,
		name,
	)
	if err != nil {
		t.Fatalf("find directory entry: %v", err)
	}

	if entry == nil {
		t.Fatal("long filename was not created")
	}

	if entry.Entry.Size != uint32(len(content)) {
		t.Fatalf(
			"file size = %d, want %d",
			entry.Entry.Size,
			len(content),
		)
	}

	file := &File{
		entry: entry.Entry,
		fs:    fs,
	}

	readBack, err := file.ReadAll()
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	if !bytes.Equal(readBack, content) {
		t.Fatalf(
			"file contents = %q, want %q",
			readBack,
			content,
		)
	}

	// The directory should not have needed expansion.
	value, err := fs.readFATEntry(dirCluster)
	if err != nil {
		t.Fatalf("read directory FAT entry: %v", err)
	}

	if value != fat32EOF {
		t.Fatalf(
			"directory cluster %d FAT entry = %#x, want EOF",
			dirCluster,
			value,
		)
	}
}

func TestWriteLongFilenameExactlyFillsDirectory(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
		dirCluster      = uint32(3)
	)

	buffer := &failDirectoryAndFATRollbackBuffer{
		data: make([]byte, dataOffset+20*bytesPerCluster),
	}

	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+dirCluster*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: buffer,
		writer: buffer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			ReservedSectors:   0,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      20,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	// Create /MUSIC in the root directory.
	musicEntry := encodeDirectoryEntry(
		[11]byte{'M', 'U', 'S', 'I', 'C', ' ', ' ', ' ', ' ', ' ', ' '},
		0x10,
		dirCluster,
		0,
	)

	root := make([]byte, bytesPerCluster)
	copy(root[:32], musicEntry[:])

	if err := fs.writeCluster(rootCluster, root); err != nil {
		t.Fatalf("write root directory: %v", err)
	}

	// Leave exactly 5 directory entries free.
	//
	// A filename consisting of 48 ASCII characters plus ".txt"
	// contains 52 UTF-16 code units:
	//
	//   ceil(52 / 13) = 4 LFN entries
	//   + 1 short entry
	//   = 5 entries.
	music := make([]byte, bytesPerCluster)

	for i := 0; i < 11; i++ {
		name := fmt.Sprintf("FILE%04d.TXT", i)

		shortName, err := encodeShortName(name)
		if err != nil {
			t.Fatalf("encode short name %q: %v", name, err)
		}

		entry := encodeDirectoryEntry(
			shortName,
			0x20,
			0,
			0,
		)

		copy(music[i*32:(i+1)*32], entry[:])
	}

	if err := fs.writeCluster(dirCluster, music); err != nil {
		t.Fatalf("write MUSIC directory: %v", err)
	}

	name := strings.Repeat("a", 48) + ".txt"
	content := []byte("hello")

	if err := fs.WriteFile(
		"/MUSIC/"+name,
		content,
	); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// The directory should still consist of exactly one cluster.
	value, err := fs.readFATEntry(dirCluster)
	if err != nil {
		t.Fatalf("read directory FAT entry: %v", err)
	}

	if value != fat32EOF {
		t.Fatalf(
			"directory cluster %d FAT entry = %#x, want EOF",
			dirCluster,
			value,
		)
	}

	entry, err := fs.findDirectoryEntry(
		dirCluster,
		name,
	)
	if err != nil {
		t.Fatalf("find directory entry: %v", err)
	}

	if entry == nil {
		t.Fatal("file was not created")
	}

	if entry.Entry.Size != uint32(len(content)) {
		t.Fatalf(
			"file size = %d, want %d",
			entry.Entry.Size,
			len(content),
		)
	}
}

func TestWriteLongFilenameExactlyFillsAcrossDirectoryClusters(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
		dirCluster1     = uint32(3)
		dirCluster2     = uint32(4)
	)

	buffer := &failDirectoryAndFATRollbackBuffer{
		data: make([]byte, dataOffset+20*bytesPerCluster),
	}

	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	// /MUSIC: cluster 3 -> cluster 4 -> EOF.
	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+dirCluster1*4:],
		dirCluster2,
	)

	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+dirCluster2*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: buffer,
		writer: buffer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			ReservedSectors:   0,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      20,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	// Create /MUSIC in the root directory.
	musicEntry := encodeDirectoryEntry(
		[11]byte{'M', 'U', 'S', 'I', 'C', ' ', ' ', ' ', ' ', ' ', ' '},
		0x10,
		dirCluster1,
		0,
	)

	root := make([]byte, bytesPerCluster)
	copy(root[:32], musicEntry[:])

	if err := fs.writeCluster(rootCluster, root); err != nil {
		t.Fatalf("write root directory: %v", err)
	}

	// Leave exactly 2 slots at the end of cluster 3.
	//
	// The target filename requires 4 LFN entries + 1 short
	// entry = 5 slots, so the record must continue into
	// cluster 4.
	music1 := make([]byte, bytesPerCluster)

	for i := 0; i < 14; i++ {
		name := fmt.Sprintf("FILE%04d.TXT", i)

		shortName, err := encodeShortName(name)
		if err != nil {
			t.Fatalf("encode short name %q: %v", name, err)
		}

		entry := encodeDirectoryEntry(
			shortName,
			0x20,
			0,
			0,
		)

		copy(music1[i*32:(i+1)*32], entry[:])
	}

	if err := fs.writeCluster(dirCluster1, music1); err != nil {
		t.Fatalf("write first MUSIC cluster: %v", err)
	}

	// Cluster 4 is completely free.
	music2 := make([]byte, bytesPerCluster)

	if err := fs.writeCluster(dirCluster2, music2); err != nil {
		t.Fatalf("write second MUSIC cluster: %v", err)
	}

	name := strings.Repeat("a", 48) + ".txt"
	content := []byte("hello")

	if err := fs.WriteFile(
		"/MUSIC/"+name,
		content,
	); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entry, err := fs.findDirectoryEntry(
		dirCluster1,
		name,
	)
	if err != nil {
		t.Fatalf("find directory entry: %v", err)
	}

	if entry == nil {
		t.Fatal("file was not created")
	}

	expected := uint32(len(content))
	if entry.Entry.Size != expected {
		t.Fatalf(
			"file size = %d, want %d",
			entry.Entry.Size,
			expected,
		)
	}

	// The short entry must be the first entry of the second
	// cluster after four LFN entries occupy the final two
	// slots of cluster 3 and the first two slots of cluster 4.
	if entry.Short.Cluster != dirCluster2 ||
		entry.Short.Offset != 2*32 {
		t.Fatalf(
			"short entry at cluster %d offset %d, want cluster %d offset %d",
			entry.Short.Cluster,
			entry.Short.Offset,
			dirCluster2,
			2*32,
		)
	}

	file := &File{
		entry: entry.Entry,
		fs:    fs,
	}

	readBack, err := file.ReadAll()
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	if !bytes.Equal(readBack, content) {
		t.Fatalf(
			"file contents = %q, want %q",
			readBack,
			content,
		)
	}
}

func TestWriteUnicodeLongFilename(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
		dirCluster      = uint32(3)
	)

	buffer := &failDirectoryAndFATRollbackBuffer{
		data: make([]byte, dataOffset+20*bytesPerCluster),
	}

	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+dirCluster*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: buffer,
		writer: buffer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			ReservedSectors:   0,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      20,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	// Create /MUSIC in the root directory.
	musicEntry := encodeDirectoryEntry(
		[11]byte{'M', 'U', 'S', 'I', 'C', ' ', ' ', ' ', ' ', ' ', ' '},
		0x10,
		dirCluster,
		0,
	)

	root := make([]byte, bytesPerCluster)
	copy(root[:32], musicEntry[:])

	if err := fs.writeCluster(rootCluster, root); err != nil {
		t.Fatalf("write root directory: %v", err)
	}

	// Empty /MUSIC directory.
	music := make([]byte, bytesPerCluster)

	if err := fs.writeCluster(dirCluster, music); err != nil {
		t.Fatalf("write MUSIC directory: %v", err)
	}

	name := "Cartoni animati – è più bello 🎵.mp3"
	content := []byte("hello")

	if err := fs.WriteFile(
		"/MUSIC/"+name,
		content,
	); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entry, err := fs.findDirectoryEntry(
		dirCluster,
		name,
	)
	if err != nil {
		t.Fatalf("find directory entry: %v", err)
	}

	if entry == nil {
		t.Fatal("Unicode filename was not created")
	}

	if entry.Entry.Name != name {
		t.Fatalf(
			"filename = %q, want %q",
			entry.Entry.Name,
			name,
		)
	}

	if entry.Entry.ShortName == name {
		t.Fatal("Unicode filename should use an LFN")
	}

	if entry.Entry.LFNEntries == 0 {
		t.Fatal("expected LFN entries")
	}

	file := &File{
		entry: entry.Entry,
		fs:    fs,
	}

	readBack, err := file.ReadAll()
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	if !bytes.Equal(readBack, content) {
		t.Fatalf(
			"file contents = %q, want %q",
			readBack,
			content,
		)
	}
}

func TestWriteFilenameAtLFNBoundary(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
		dirCluster      = uint32(3)
	)

	buffer := &failDirectoryAndFATRollbackBuffer{
		data: make([]byte, dataOffset+20*bytesPerCluster),
	}

	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+dirCluster*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: buffer,
		writer: buffer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			ReservedSectors:   0,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      20,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	// Create /MUSIC in the root directory.
	musicEntry := encodeDirectoryEntry(
		[11]byte{'M', 'U', 'S', 'I', 'C', ' ', ' ', ' ', ' ', ' ', ' '},
		0x10,
		dirCluster,
		0,
	)

	root := make([]byte, bytesPerCluster)
	copy(root[:32], musicEntry[:])

	if err := fs.writeCluster(rootCluster, root); err != nil {
		t.Fatalf("write root directory: %v", err)
	}

	music := make([]byte, bytesPerCluster)

	if err := fs.writeCluster(dirCluster, music); err != nil {
		t.Fatalf("write MUSIC directory: %v", err)
	}

	// Exactly 13 UTF-16 code units.
	name := "123456789.txt"
	content := []byte("hello")

	if err := fs.WriteFile(
		"/MUSIC/"+name,
		content,
	); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entry, err := fs.findDirectoryEntry(
		dirCluster,
		name,
	)
	if err != nil {
		t.Fatalf("find directory entry: %v", err)
	}

	if entry == nil {
		t.Fatal("file was not created")
	}

	// The name is not an 8.3 filename, so it must use exactly
	// one LFN entry.
	if entry.Entry.LFNEntries != 1 {
		t.Fatalf(
			"LFN entries = %d, want 1",
			entry.Entry.LFNEntries,
		)
	}

	file := &File{
		entry: entry.Entry,
		fs:    fs,
	}

	readBack, err := file.ReadAll()
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	if !bytes.Equal(readBack, content) {
		t.Fatalf(
			"file contents = %q, want %q",
			readBack,
			content,
		)
	}
}

func TestWriteFilenameJustOverLFNBoundary(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
		dirCluster      = uint32(3)
	)

	buffer := &failDirectoryAndFATRollbackBuffer{
		data: make([]byte, dataOffset+20*bytesPerCluster),
	}

	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+dirCluster*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: buffer,
		writer: buffer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			ReservedSectors:   0,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      20,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	musicEntry := encodeDirectoryEntry(
		[11]byte{'M', 'U', 'S', 'I', 'C', ' ', ' ', ' ', ' ', ' ', ' '},
		0x10,
		dirCluster,
		0,
	)

	root := make([]byte, bytesPerCluster)
	copy(root[:32], musicEntry[:])

	if err := fs.writeCluster(rootCluster, root); err != nil {
		t.Fatalf("write root directory: %v", err)
	}

	music := make([]byte, bytesPerCluster)

	if err := fs.writeCluster(dirCluster, music); err != nil {
		t.Fatalf("write MUSIC directory: %v", err)
	}

	// 10-character basename + ".txt" = 14 UTF-16 code units.
	name := "1234567890.txt"
	content := []byte("hello")

	if err := fs.WriteFile(
		"/MUSIC/"+name,
		content,
	); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entry, err := fs.findDirectoryEntry(
		dirCluster,
		name,
	)
	if err != nil {
		t.Fatalf("find directory entry: %v", err)
	}

	if entry == nil {
		t.Fatal("file was not created")
	}

	if entry.Entry.LFNEntries != 2 {
		t.Fatalf(
			"LFN entries = %d, want 2",
			entry.Entry.LFNEntries,
		)
	}

	file := &File{
		entry: entry.Entry,
		fs:    fs,
	}

	readBack, err := file.ReadAll()
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	if !bytes.Equal(readBack, content) {
		t.Fatalf(
			"file contents = %q, want %q",
			readBack,
			content,
		)
	}
}

func TestWriteFilenameWithSurrogatePairAtLFNBoundary(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
		dirCluster      = uint32(3)
	)

	buffer := &failDirectoryAndFATRollbackBuffer{
		data: make([]byte, dataOffset+20*bytesPerCluster),
	}

	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+dirCluster*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: buffer,
		writer: buffer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			ReservedSectors:   0,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      20,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	musicEntry := encodeDirectoryEntry(
		[11]byte{'M', 'U', 'S', 'I', 'C', ' ', ' ', ' ', ' ', ' ', ' '},
		0x10,
		dirCluster,
		0,
	)

	root := make([]byte, bytesPerCluster)
	copy(root[:32], musicEntry[:])

	if err := fs.writeCluster(rootCluster, root); err != nil {
		t.Fatalf("write root directory: %v", err)
	}

	music := make([]byte, bytesPerCluster)

	if err := fs.writeCluster(dirCluster, music); err != nil {
		t.Fatalf("write MUSIC directory: %v", err)
	}

	// UTF-16 code units:
	//
	//   9 ASCII characters = 9
	//   😀                = 2
	//   ".txt"            = 4
	//   total              = 15
	//
	// This must therefore require exactly 2 LFN entries.
	name := "123456789😀.txt"
	content := []byte("hello")

	if err := fs.WriteFile(
		"/MUSIC/"+name,
		content,
	); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entry, err := fs.findDirectoryEntry(
		dirCluster,
		name,
	)
	if err != nil {
		t.Fatalf("find directory entry: %v", err)
	}

	if entry == nil {
		t.Fatal("file was not created")
	}

	if entry.Entry.Name != name {
		t.Fatalf(
			"filename = %q, want %q",
			entry.Entry.Name,
			name,
		)
	}

	if entry.Entry.LFNEntries != 2 {
		t.Fatalf(
			"LFN entries = %d, want 2",
			entry.Entry.LFNEntries,
		)
	}

	file := &File{
		entry: entry.Entry,
		fs:    fs,
	}

	readBack, err := file.ReadAll()
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	if !bytes.Equal(readBack, content) {
		t.Fatalf(
			"file contents = %q, want %q",
			readBack,
			content,
		)
	}
}

func TestWriteFilenameDoesNotSplitSurrogatePair(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
		dirCluster      = uint32(3)
	)

	buffer := &failDirectoryAndFATRollbackBuffer{
		data: make([]byte, dataOffset+20*bytesPerCluster),
	}

	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		buffer.data[fatOffset+dirCluster*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: buffer,
		writer: buffer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			ReservedSectors:   0,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      20,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	musicEntry := encodeDirectoryEntry(
		[11]byte{'M', 'U', 'S', 'I', 'C', ' ', ' ', ' ', ' ', ' ', ' '},
		0x10,
		dirCluster,
		0,
	)

	root := make([]byte, bytesPerCluster)
	copy(root[:32], musicEntry[:])

	if err := fs.writeCluster(rootCluster, root); err != nil {
		t.Fatalf("write root directory: %v", err)
	}

	music := make([]byte, bytesPerCluster)

	if err := fs.writeCluster(dirCluster, music); err != nil {
		t.Fatalf("write MUSIC directory: %v", err)
	}

	// 12 ASCII characters + emoji + ".txt":
	//
	//   12 ASCII = 12 UTF-16 code units
	//   emoji     = 2 UTF-16 code units
	//   ".txt"    = 4 UTF-16 code units
	//   total     = 18
	//
	// The emoji's surrogate pair falls across the 13-code-unit
	// boundary if the filename is naively split by UTF-16 units.
	name := "123456789012😀.txt"
	content := []byte("hello")

	if err := fs.WriteFile(
		"/MUSIC/"+name,
		content,
	); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entry, err := fs.findDirectoryEntry(
		dirCluster,
		name,
	)
	if err != nil {
		t.Fatalf("find directory entry: %v", err)
	}

	if entry == nil {
		t.Fatal("file was not created")
	}

	if entry.Entry.Name != name {
		t.Fatalf(
			"filename = %q, want %q",
			entry.Entry.Name,
			name,
		)
	}

	if entry.Entry.LFNEntries != 2 {
		t.Fatalf(
			"LFN entries = %d, want 2",
			entry.Entry.LFNEntries,
		)
	}

	file := &File{
		entry: entry.Entry,
		fs:    fs,
	}

	readBack, err := file.ReadAll()
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	if !bytes.Equal(readBack, content) {
		t.Fatalf(
			"file contents = %q, want %q",
			readBack,
			content,
		)
	}
}

func TestWriteFileLongNameCollisionBeyondNine(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
	)

	data := make([]byte, dataOffset+40*bytesPerCluster)

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
			TotalSectors:      41,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	// Each filename requires three directory entries:
	// two LFN entries plus one short entry.
	//
	// Ten files therefore require 30 entries, which fits in the
	// synthetic filesystem because the directory can expand.
	const baseName = "Some long filename"

	for n := 0; n < 10; n++ {
		name := fmt.Sprintf("%s %d.txt", baseName, n)

		if err := fs.WriteFile(name, []byte("data")); err != nil {
			t.Fatalf("WriteFile(%q) failed: %v", name, err)
		}
	}

	expectedShortNames := []string{
		"SOMELO~1.TXT",
		"SOMELO~2.TXT",
		"SOMELO~3.TXT",
		"SOMELO~4.TXT",
		"SOMELO~5.TXT",
		"SOMELO~6.TXT",
		"SOMELO~7.TXT",
		"SOMELO~8.TXT",
		"SOMELO~9.TXT",
		"SOMEL~10.TXT",
	}

	for n, expectedShortName := range expectedShortNames {
		name := fmt.Sprintf("%s %d.txt", baseName, n)

		entry, err := fs.findDirectoryEntry(rootCluster, name)
		if err != nil {
			t.Fatalf("findDirectoryEntry(%q) failed: %v", name, err)
		}

		if entry == nil {
			t.Fatalf("file %q was not found", name)
		}

		if entry.Entry.ShortName != expectedShortName {
			t.Fatalf(
				"%q: short name = %q, want %q",
				name,
				entry.Entry.ShortName,
				expectedShortName,
			)
		}
	}
}

func TestFindFreeDirectorySlotsReportsEndOfDirectory(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
	)

	data := make([]byte, dataOffset+bytesPerCluster)

	binary.LittleEndian.PutUint32(
		data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	// The first directory entry is live.
	data[dataOffset] = 0x41

	// The second entry marks the end of the directory.
	data[dataOffset+32] = 0x00

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: &writerAtBuffer{data: data},
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      2,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	// asking for 16 contiguous entries, while only 15 are available after the live entry.
	_, err := fs.findFreeDirectorySlots(rootCluster, 16)
	if err == nil {
		t.Fatal("findFreeDirectorySlots unexpectedly succeeded")
	}

	if !errors.Is(err, errDirectoryEndReached) {
		t.Fatalf(
			"error = %v, want errDirectoryEndReached",
			err,
		)
	}
}

func TestWriteFileExpandsDirectoryAfterEndOfDirectory(t *testing.T) {
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

	// Fill every directory entry except the last one with live entries.
	for offset := 0; offset < bytesPerCluster-32; offset += 32 {
		data[dataOffset+offset] = 0x41
	}

	// The final entry is the end-of-directory marker.
	data[dataOffset+bytesPerCluster-32] = 0x00

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

	name := "A long filename.txt"

	if err := fs.WriteFile(name, []byte("data")); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	entry, err := fs.findDirectoryEntry(rootCluster, name)
	if err != nil {
		t.Fatal(err)
	}

	if entry == nil {
		t.Fatalf("file %q was not found", name)
	}

	nextCluster, err := fs.readFATEntry(rootCluster)
	if err != nil {
		t.Fatal(err)
	}

	if nextCluster == fat32EOF {
		t.Fatal("directory was not expanded")
	}

	if nextCluster < 2 {
		t.Fatalf("invalid directory cluster: %d", nextCluster)
	}

	if entry.Entry.EntryCluster != nextCluster {
		t.Fatalf(
			"file entry cluster = %d, want %d",
			entry.Entry.EntryCluster,
			nextCluster,
		)
	}
}

func TestWriteFileAfterDirectoryExpansion(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
	)

	data := make([]byte, dataOffset+5*bytesPerCluster)

	binary.LittleEndian.PutUint32(
		data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	// Leave only the final directory entry available.
	for offset := 0; offset < bytesPerCluster-32; offset += 32 {
		data[dataOffset+offset] = 0x41
	}

	data[dataOffset+bytesPerCluster-32] = 0x00

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

	firstName := "First long filename.txt"
	if err := fs.WriteFile(firstName, []byte("first")); err != nil {
		t.Fatalf("first WriteFile failed: %v", err)
	}

	secondName := "Second long filename.txt"
	if err := fs.WriteFile(secondName, []byte("second")); err != nil {
		t.Fatalf("second WriteFile failed: %v", err)
	}

	first, err := fs.findDirectoryEntry(rootCluster, firstName)
	if err != nil {
		t.Fatal(err)
	}

	if first == nil {
		t.Fatalf("file %q was not found", firstName)
	}

	second, err := fs.findDirectoryEntry(rootCluster, secondName)
	if err != nil {
		t.Fatal(err)
	}

	if second == nil {
		t.Fatalf("file %q was not found", secondName)
	}

	nextCluster, err := fs.readFATEntry(rootCluster)
	if err != nil {
		t.Fatal(err)
	}

	if nextCluster == fat32EOF || nextCluster < 2 {
		t.Fatalf("root directory was not expanded: next cluster = %d", nextCluster)
	}

	expandedEnd, err := fs.readFATEntry(nextCluster)
	if err != nil {
		t.Fatal(err)
	}

	if expandedEnd != fat32EOF {
		t.Fatalf(
			"expanded directory cluster ends at %d, want EOF",
			expandedEnd,
		)
	}
}

func TestWriteFileDoesNotExpandDirectoryWithScatteredFreeSlotsAndNoEndMarker(t *testing.T) {
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

	// Entry 0: live.
	data[dataOffset] = 0x41

	// Entry 1: deleted.
	data[dataOffset+32] = 0xE5

	// Entry 2: live.
	data[dataOffset+64] = 0x41

	// Entry 3: deleted.
	data[dataOffset+96] = 0xE5

	// Every remaining entry is live. Crucially, there is no 0x00
	// end-of-directory marker anywhere in this cluster.
	for offset := 128; offset < bytesPerCluster; offset += 32 {
		data[dataOffset+offset] = 0x41
	}

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

	name := "A long filename.txt"

	err := fs.WriteFile(name, []byte("data"))
	if err == nil {
		t.Fatal("WriteFile unexpectedly succeeded")
	}

	if errors.Is(err, errDirectoryEndReached) {
		t.Fatalf(
			"WriteFile exposed errDirectoryEndReached: %v",
			err,
		)
	}

	nextCluster, err := fs.readFATEntry(rootCluster)
	if err != nil {
		t.Fatal(err)
	}

	if nextCluster != fat32EOF {
		t.Fatalf(
			"directory was expanded unexpectedly: next cluster = %d",
			nextCluster,
		)
	}
}

func TestWriteFileReusesContiguousDeletedSlotsBeforeEndMarker(t *testing.T) {
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

	// Entries 0-2 are live.
	for offset := 0; offset < 96; offset += 32 {
		data[dataOffset+offset] = 0x41
	}

	// Entries 3-5 are a contiguous run of deleted entries.
	for offset := 96; offset < 192; offset += 32 {
		data[dataOffset+offset] = 0xE5
	}

	// Entry 6 is the end-of-directory marker.
	data[dataOffset+192] = 0x00

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

	name := "A long filename.txt"

	if err := fs.WriteFile(name, []byte("data")); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	nextCluster, err := fs.readFATEntry(rootCluster)
	if err != nil {
		t.Fatal(err)
	}

	if nextCluster != fat32EOF {
		t.Fatalf(
			"directory was expanded unexpectedly: next cluster = %d",
			nextCluster,
		)
	}

	entry, err := fs.findDirectoryEntry(rootCluster, name)
	if err != nil {
		t.Fatal(err)
	}

	if entry == nil {
		t.Fatalf("file %q was not found", name)
	}

	if entry.Entry.EntryCluster != rootCluster {
		t.Fatalf(
			"directory entry cluster = %d, want %d",
			entry.Entry.EntryCluster,
			rootCluster,
		)
	}

	if entry.Entry.EntryOffset != 160 {
		t.Fatalf(
			"directory entry offset = %d, want %d",
			entry.Entry.EntryOffset,
			160,
		)
	}
}

func TestWriteFileExpansionDirectoryWriteFailureRollsBack(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
	)

	data := make([]byte, dataOffset+5*bytesPerCluster)

	binary.LittleEndian.PutUint32(
		data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	// Fill the directory except for the final end-of-directory entry.
	for offset := 0; offset < bytesPerCluster-32; offset += 32 {
		data[dataOffset+offset] = 0x41
	}

	data[dataOffset+bytesPerCluster-32] = 0x00

	// The first write to the newly expanded directory cluster will fail.
	newDirectoryCluster := uint32(3)
	directoryWriteOffset := int64(
		dataOffset + int(newDirectoryCluster-2)*bytesPerCluster,
	)

	writer := &failOnWriteCountAt{
		data:       data,
		failOffset: directoryWriteOffset,
		failCount:  1,
	}

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: writer,
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

	err := fs.WriteFile(
		"A long filename.txt",
		[]byte("data"),
	)
	if err == nil {
		t.Fatal("WriteFile unexpectedly succeeded")
	}

	nextCluster, err := fs.readFATEntry(rootCluster)
	if err != nil {
		t.Fatal(err)
	}

	if nextCluster != fat32EOF {
		t.Fatalf(
			"root directory chain changed: next cluster = %d, want EOF",
			nextCluster,
		)
	}

	allocated, err := fs.IsFreeCluster(newDirectoryCluster)
	if err != nil {
		t.Fatal(err)
	}

	if !allocated {
		t.Fatalf(
			"expanded directory cluster %d was not released",
			newDirectoryCluster,
		)
	}

	entry, err := fs.findDirectoryEntry(
		rootCluster,
		"A long filename.txt",
	)
	if err != nil {
		t.Fatal(err)
	}

	if entry != nil {
		t.Fatal("failed WriteFile left a directory entry behind")
	}
}

func TestWriteFileRoundTrip(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
	)

	data := make([]byte, dataOffset+8*bytesPerCluster)

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
			TotalSectors:      9,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	content := bytes.Repeat([]byte("0123456789abcdef"), 100)

	name := "Round trip test.txt"

	if err := fs.WriteFile(name, content); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	entry, err := fs.findDirectoryEntry(rootCluster, name)
	if err != nil {
		t.Fatal(err)
	}

	if entry == nil {
		t.Fatalf("file %q was not found", name)
	}

	file := &File{
		entry: entry.Entry,
		fs:    fs,
	}

	got, err := file.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if !bytes.Equal(got, content) {
		t.Fatalf(
			"ReadAll returned %d bytes, want %d",
			len(got),
			len(content),
		)
	}
}

func TestWriteFileExactClusterBoundary(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
	)

	data := make([]byte, dataOffset+8*bytesPerCluster)

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
			TotalSectors:      9,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	content := bytes.Repeat([]byte{0xAB}, bytesPerCluster*2)

	name := "exact-boundary.bin"

	if err := fs.WriteFile(name, content); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	entry, err := fs.findDirectoryEntry(rootCluster, name)
	if err != nil {
		t.Fatal(err)
	}

	if entry == nil {
		t.Fatalf("file %q was not found", name)
	}

	chain, err := fs.ClusterChain(entry.Entry.StartCluster)
	if err != nil {
		t.Fatal(err)
	}

	if len(chain) != 2 {
		t.Fatalf(
			"file cluster chain length = %d, want 2",
			len(chain),
		)
	}

	if next, err := fs.readFATEntry(chain[len(chain)-1]); err != nil {
		t.Fatal(err)
	} else if next != fat32EOF {
		t.Fatalf(
			"last cluster points to %d, want EOF",
			next,
		)
	}

	file := &File{
		entry: entry.Entry,
		fs:    fs,
	}

	got, err := file.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if !bytes.Equal(got, content) {
		t.Fatalf("ReadAll returned incorrect data")
	}
}

func TestWriteFileOneByteOverClusterBoundary(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
	)

	data := make([]byte, dataOffset+8*bytesPerCluster)

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
			TotalSectors:      9,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	content := bytes.Repeat(
		[]byte{0xCD},
		bytesPerCluster*2+1,
	)

	name := "over-boundary.bin"

	if err := fs.WriteFile(name, content); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	entry, err := fs.findDirectoryEntry(rootCluster, name)
	if err != nil {
		t.Fatal(err)
	}

	if entry == nil {
		t.Fatalf("file %q was not found", name)
	}

	chain, err := fs.ClusterChain(entry.Entry.StartCluster)
	if err != nil {
		t.Fatal(err)
	}

	if len(chain) != 3 {
		t.Fatalf(
			"file cluster chain length = %d, want 3",
			len(chain),
		)
	}

	file := &File{
		entry: entry.Entry,
		fs:    fs,
	}

	got, err := file.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if !bytes.Equal(got, content) {
		t.Fatalf("ReadAll returned incorrect data")
	}
}

func TestWriteEmptyFileHasNoClusterChain(t *testing.T) {
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

	name := "empty.bin"

	if err := fs.WriteFile(name, nil); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	entry, err := fs.findDirectoryEntry(rootCluster, name)
	if err != nil {
		t.Fatal(err)
	}

	if entry == nil {
		t.Fatalf("file %q was not found", name)
	}

	if entry.Entry.Size != 0 {
		t.Fatalf(
			"file size = %d, want 0",
			entry.Entry.Size,
		)
	}

	if entry.Entry.StartCluster != 0 {
		t.Fatalf(
			"start cluster = %d, want 0",
			entry.Entry.StartCluster,
		)
	}

	file := &File{
		entry: entry.Entry,
		fs:    fs,
	}

	got, err := file.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if len(got) != 0 {
		t.Fatalf(
			"ReadAll returned %d bytes, want 0",
			len(got),
		)
	}
}

func TestWriteNewFileDataRollsBackEntireChainOnDataWriteFailure(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
	)

	data := make([]byte, dataOffset+8*bytesPerCluster)

	binary.LittleEndian.PutUint32(
		data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	// Fail when writing the second data cluster.
	secondDataCluster := uint32(4)

	failOffset := int64(
		dataOffset +
			int(secondDataCluster-2)*bytesPerCluster,
	)

	writer := &failOnWriteCountAt{
		data:       data,
		failOffset: failOffset,
		failCount:  1,
	}

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: writer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      9,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	content := bytes.Repeat(
		[]byte{0xEF},
		bytesPerCluster*3,
	)

	_, err := fs.writeNewFileData(content)
	if err == nil {
		t.Fatal("writeNewFileData unexpectedly succeeded")
	}

	for cluster := uint32(3); cluster <= 5; cluster++ {
		free, err := fs.IsFreeCluster(cluster)
		if err != nil {
			t.Fatal(err)
		}

		if !free {
			t.Fatalf(
				"cluster %d remained allocated after rollback",
				cluster,
			)
		}
	}
}

func TestAllocateClusterChainLinksClusters(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
	)

	data := make([]byte, dataOffset+8*bytesPerCluster)

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
			TotalSectors:      9,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	chain, err := fs.allocateClusterChain(3)
	if err != nil {
		t.Fatalf("allocateClusterChain failed: %v", err)
	}

	if len(chain) != 3 {
		t.Fatalf(
			"chain length = %d, want 3",
			len(chain),
		)
	}

	for i := 0; i < len(chain)-1; i++ {
		next, err := fs.readFATEntry(chain[i])
		if err != nil {
			t.Fatal(err)
		}

		if next != chain[i+1] {
			t.Fatalf(
				"FAT[%d] = %d, want %d",
				chain[i],
				next,
				chain[i+1],
			)
		}
	}

	last, err := fs.readFATEntry(chain[len(chain)-1])
	if err != nil {
		t.Fatal(err)
	}

	if last != fat32EOF {
		t.Fatalf(
			"FAT[%d] = 0x%08x, want EOF",
			chain[len(chain)-1],
			last,
		)
	}
}

func TestWriteFATEntryPreservesHighFourBits(t *testing.T) {
	const (
		sectorSize   = 512
		fatOffset    = 0
		cluster      = uint32(2)
		initialValue = uint32(0xA1234567)
		newValue     = uint32(0x00000009)
	)

	data := make([]byte, sectorSize*2)

	binary.LittleEndian.PutUint32(
		data[fatOffset+cluster*4:],
		initialValue,
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
			RootCluster:       cluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      sectorSize,
			BytesPerCluster: sectorSize,
		},
	}

	if err := fs.writeFATEntry(cluster, newValue); err != nil {
		t.Fatalf("writeFATEntry failed: %v", err)
	}

	got := binary.LittleEndian.Uint32(
		data[cluster*4 : cluster*4+4],
	)

	want := uint32(0xA0000009)

	if got != want {
		t.Fatalf(
			"FAT[%d] raw value = 0x%08x, want 0x%08x",
			cluster,
			got,
			want,
		)
	}
}

func TestWriteEmptyFileCreatesDirectoryEntry(t *testing.T) {
	const (
		sectorSize    = 512
		sectorsPerFAT = 1
		numberOfFATs  = 1
		totalSectors  = 20
		rootCluster   = uint32(2)
		name          = "empty.txt"
	)

	data := make([]byte, sectorSize*totalSectors)

	fatOffset := int64(0)

	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(rootCluster)*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: &writerAtBuffer{data: data},
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      numberOfFATs,
			SectorsPerFAT:     sectorsPerFAT,
			TotalSectors:      totalSectors,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      sectorSize * 2,
			BytesPerCluster: sectorSize,
		},
	}

	if err := fs.WriteFile(name, nil); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	record, err := fs.findDirectoryEntry(
		rootCluster,
		name,
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatalf("directory entry %q not found", name)
	}

	if record.Entry.Size != 0 {
		t.Fatalf(
			"file size = %d, want 0",
			record.Entry.Size,
		)
	}

	if record.Entry.StartCluster != 0 {
		t.Fatalf(
			"start cluster = %d, want 0",
			record.Entry.StartCluster,
		)
	}

	for cluster := uint32(2); cluster <= fs.boot.MaxCluster(); cluster++ {
		value, err := fs.readFATEntry(cluster)
		if err != nil {
			t.Fatal(err)
		}

		if cluster == rootCluster {
			if value < fat32EOF {
				t.Fatalf(
					"root FAT[%d] = 0x%08x, want EOF",
					cluster,
					value,
				)
			}
			continue
		}

		if value != 0 {
			t.Fatalf(
				"FAT[%d] = 0x%08x, want free cluster",
				cluster,
				value,
			)
		}
	}
}

func TestWriteFileMultiClusterRoundTrip(t *testing.T) {
	// Exercise the 3 full clusters + partial fourth cluster case
	// and verify the directory size, chain length, and exact data round-trip.

	const (
		sectorSize      = 512
		sectorsPerFAT   = 1
		numberOfFATs    = 1
		totalSectors    = 40
		rootCluster     = uint32(2)
		bytesPerCluster = sectorSize
	)

	data := make([]byte, sectorSize*totalSectors)

	fatOffset := int64(0)

	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(rootCluster)*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: &writerAtBuffer{data: data},
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      numberOfFATs,
			SectorsPerFAT:     sectorsPerFAT,
			TotalSectors:      totalSectors,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      sectorSize * 2,
			BytesPerCluster: bytesPerCluster,
		},
	}

	const extraBytes = 37

	payloadSize := bytesPerCluster*3 + extraBytes
	payload := make([]byte, payloadSize)

	for i := range payload {
		payload[i] = byte((i * 37) % 251)
	}

	const name = "multi.bin"

	if err := fs.WriteFile(name, payload); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	record, err := fs.findDirectoryEntry(rootCluster, name)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatalf("directory entry %q not found", name)
	}

	if record.Entry.Size != uint32(len(payload)) {
		t.Fatalf(
			"file size = %d, want %d",
			record.Entry.Size,
			len(payload),
		)
	}

	if record.Entry.StartCluster < 2 {
		t.Fatalf(
			"start cluster = %d, want valid cluster",
			record.Entry.StartCluster,
		)
	}

	chain, err := fs.ClusterChain(record.Entry.StartCluster)
	if err != nil {
		t.Fatal(err)
	}

	wantClusters := 4

	if len(chain) != wantClusters {
		t.Fatalf(
			"cluster chain length = %d, want %d",
			len(chain),
			wantClusters,
		)
	}

	got, err := (&File{
		entry: record.Entry,
		fs:    fs,
	}).ReadAll()
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if !bytes.Equal(got, payload) {
		t.Fatal("read-back data differs from written data")
	}
}

func TestDeleteMultiClusterFileFreesEntireChain(t *testing.T) {
	// Deleting a multi-cluster file:
	// 1. create a multi-cluster file;
	// 2. confirm its chain;
	// 3. delete it;
	// 4. confirm the directory entry is gone;
	// 5. confirm every cluster in the former chain is free;
	// 6. confirm unrelated/root clusters remain allocated.

	const (
		sectorSize    = 512
		sectorsPerFAT = 1
		numberOfFATs  = 1
		totalSectors  = 40
		rootCluster   = uint32(2)
	)

	data := make([]byte, sectorSize*totalSectors)

	fatOffset := int64(0)

	// Keep the root directory allocated.
	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(rootCluster)*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: &writerAtBuffer{data: data},
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      numberOfFATs,
			SectorsPerFAT:     sectorsPerFAT,
			TotalSectors:      totalSectors,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      sectorSize * 2,
			BytesPerCluster: sectorSize,
		},
	}

	payload := make([]byte, sectorSize*2+17)

	for i := range payload {
		payload[i] = byte((i * 19) % 251)
	}

	const name = "delete-me.bin"

	if err := fs.WriteFile(name, payload); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	record, err := fs.findDirectoryEntry(rootCluster, name)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatalf("directory entry %q not found", name)
	}

	chain, err := fs.ClusterChain(record.Entry.StartCluster)
	if err != nil {
		t.Fatal(err)
	}

	if len(chain) != 3 {
		t.Fatalf(
			"cluster chain length = %d, want 3",
			len(chain),
		)
	}

	for _, cluster := range chain {
		free, err := fs.IsFreeCluster(cluster)
		if err != nil {
			t.Fatal(err)
		}

		if free {
			t.Fatalf(
				"cluster %d is free before deletion",
				cluster,
			)
		}
	}

	if err := fs.Delete(name); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	record, err = fs.findDirectoryEntry(rootCluster, name)
	if err != nil {
		t.Fatal(err)
	}

	if record != nil {
		t.Fatalf(
			"directory entry %q still exists after deletion",
			name,
		)
	}

	for _, cluster := range chain {
		free, err := fs.IsFreeCluster(cluster)
		if err != nil {
			t.Fatal(err)
		}

		if !free {
			t.Fatalf(
				"cluster %d is still allocated after deletion",
				cluster,
			)
		}
	}

	free, err := fs.IsFreeCluster(rootCluster)
	if err != nil {
		t.Fatal(err)
	}

	if free {
		t.Fatal("root directory cluster was freed")
	}
}

func TestDeleteDoesNotFreeFATChainWhenDirectoryDeletionFails(t *testing.T) {
	const (
		sectorSize    = 512
		sectorsPerFAT = 1
		numberOfFATs  = 1
		totalSectors  = 30

		rootCluster = uint32(2)
		fileCluster = uint32(5)
	)

	data := make([]byte, sectorSize*totalSectors)

	fatOffset := int64(0)
	dataOffset := int64(sectorSize * 2)

	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(rootCluster)*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(fileCluster)*4:],
		fat32EOF,
	)

	buffer := &failDirectoryAndFATRollbackBuffer{
		data:               data,
		directoryOffset:    dataOffset,
		failDirectoryWrite: true,
		failFATWriteCount:  0,
	}

	fs := &Filesystem{
		reader: buffer,
		writer: buffer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      numberOfFATs,
			SectorsPerFAT:     sectorsPerFAT,
			TotalSectors:      totalSectors,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: sectorSize,
		},
	}

	entry := encodeDirectoryEntry(
		[11]byte{'F', 'I', 'L', 'E', ' ', ' ', ' ', ' ', 'T', 'X', 'T'},
		0x20,
		fileCluster,
		1,
	)

	copy(data[dataOffset:dataOffset+32], entry[:])
	data[dataOffset+32] = 0x00

	before := append([]byte(nil), data...)

	err := fs.Delete("FILE.TXT")
	if err == nil {
		t.Fatal("Delete succeeded, want directory write failure")
	}

	if !bytes.Contains(
		[]byte(err.Error()),
		[]byte("injected directory write failure"),
	) {
		t.Fatalf(
			"error = %q, want injected directory write failure",
			err,
		)
	}

	if !bytes.Equal(data, before) {
		t.Fatal("filesystem changed despite directory deletion failure")
	}

	free, err := fs.IsFreeCluster(fileCluster)
	if err != nil {
		t.Fatal(err)
	}

	if free {
		t.Fatalf(
			"file cluster %d was freed despite directory deletion failure",
			fileCluster,
		)
	}
}

func TestDeleteLongFilenameRemovesLFNEntries(t *testing.T) {
	const (
		sectorSize    = 512
		sectorsPerFAT = 1
		numberOfFATs  = 1
		totalSectors  = 20
		rootCluster   = uint32(2)
	)

	data := make([]byte, sectorSize*totalSectors)

	fatOffset := int64(0)

	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(rootCluster)*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: &writerAtBuffer{data: data},
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      numberOfFATs,
			SectorsPerFAT:     sectorsPerFAT,
			TotalSectors:      totalSectors,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      sectorSize * 2,
			BytesPerCluster: sectorSize,
		},
	}

	const name = "This is a sufficiently long filename.txt"

	if err := fs.WriteFile(name, []byte("hello")); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	record, err := fs.findDirectoryEntry(rootCluster, name)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatalf("directory entry %q not found", name)
	}

	if len(record.LFN) == 0 {
		t.Fatal("expected at least one LFN entry")
	}

	shortLocation := record.Short
	lfnLocations := append(
		[]directoryEntryLocation(nil),
		record.LFN...,
	)

	if err := fs.Delete(name); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	record, err = fs.findDirectoryEntry(rootCluster, name)
	if err != nil {
		t.Fatal(err)
	}

	if record != nil {
		t.Fatalf(
			"directory entry %q still exists after deletion",
			name,
		)
	}

	for _, location := range lfnLocations {
		clusterData, err := fs.readCluster(location.Cluster)
		if err != nil {
			t.Fatal(err)
		}

		if clusterData[location.Offset] != 0xE5 {
			t.Fatalf(
				"LFN entry at cluster %d offset %d has first byte 0x%02x, want 0xE5",
				location.Cluster,
				location.Offset,
				clusterData[location.Offset],
			)
		}
	}

	clusterData, err := fs.readCluster(shortLocation.Cluster)
	if err != nil {
		t.Fatal(err)
	}

	if clusterData[shortLocation.Offset] != 0xE5 {
		t.Fatalf(
			"short entry at cluster %d offset %d has first byte 0x%02x, want 0xE5",
			shortLocation.Cluster,
			shortLocation.Offset,
			clusterData[shortLocation.Offset],
		)
	}
}

func TestWriteFileReusesSlotsAfterDeletingLongFilename(t *testing.T) {
	const (
		sectorSize    = 512
		sectorsPerFAT = 1
		numberOfFATs  = 1
		totalSectors  = 20
		rootCluster   = uint32(2)
	)

	data := make([]byte, sectorSize*totalSectors)

	fatOffset := int64(0)

	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(rootCluster)*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: &writerAtBuffer{data: data},
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      numberOfFATs,
			SectorsPerFAT:     sectorsPerFAT,
			TotalSectors:      totalSectors,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      sectorSize * 2,
			BytesPerCluster: sectorSize,
		},
	}

	firstName := "First sufficiently long filename.txt"
	secondName := "Second sufficiently long filename.txt"

	if err := fs.WriteFile(firstName, []byte("first")); err != nil {
		t.Fatalf("first WriteFile failed: %v", err)
	}

	first, err := fs.findDirectoryEntry(rootCluster, firstName)
	if err != nil {
		t.Fatal(err)
	}

	if first == nil {
		t.Fatalf("first file %q not found", firstName)
	}

	if len(first.LFN) == 0 {
		t.Fatal("first file has no LFN entries")
	}

	firstSlots := append(
		[]directoryEntryLocation(nil),
		first.LFN...,
	)
	firstSlots = append(firstSlots, first.Short)

	if err := fs.Delete(firstName); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if err := fs.WriteFile(secondName, []byte("second")); err != nil {
		t.Fatalf("second WriteFile failed: %v", err)
	}

	second, err := fs.findDirectoryEntry(rootCluster, secondName)
	if err != nil {
		t.Fatal(err)
	}

	if second == nil {
		t.Fatalf("second file %q not found", secondName)
	}

	secondSlots := append(
		[]directoryEntryLocation(nil),
		second.LFN...,
	)
	secondSlots = append(secondSlots, second.Short)

	if len(secondSlots) != len(firstSlots) {
		t.Fatalf(
			"second record uses %d slots, first used %d",
			len(secondSlots),
			len(firstSlots),
		)
	}

	for i := range firstSlots {
		if secondSlots[i] != firstSlots[i] {
			t.Fatalf(
				"second record slot %d = %+v, want %+v",
				i,
				secondSlots[i],
				firstSlots[i],
			)
		}
	}
}

func TestDeleteDirectoryIsRejectedWithoutMutation(t *testing.T) {
	const (
		sectorSize    = 512
		sectorsPerFAT = 1
		numberOfFATs  = 1
		totalSectors  = 20
		rootCluster   = uint32(2)
		dirCluster    = uint32(3)
	)

	data := make([]byte, sectorSize*totalSectors)

	fatOffset := int64(0)

	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(rootCluster)*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(dirCluster)*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: &writerAtBuffer{data: data},
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      numberOfFATs,
			SectorsPerFAT:     sectorsPerFAT,
			TotalSectors:      totalSectors,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      sectorSize * 2,
			BytesPerCluster: sectorSize,
		},
	}

	entry := encodeDirectoryEntry(
		[11]byte{'T', 'E', 'S', 'T', 'D', 'I', 'R', ' ', ' ', ' ', ' '},
		0x10,
		dirCluster,
		0,
	)

	dirOffset := int64(sectorSize * 2)

	copy(data[dirOffset:dirOffset+32], entry[:])
	data[dirOffset+32] = 0x00

	before := append([]byte(nil), data...)

	err := fs.Delete("TESTDIR")
	if err == nil {
		t.Fatal("Delete succeeded, want directory error")
	}

	if !bytes.Contains(
		[]byte(err.Error()),
		[]byte("is a directory"),
	) {
		t.Fatalf(
			"error = %q, want directory error",
			err,
		)
	}

	if !bytes.Equal(data, before) {
		t.Fatal("filesystem changed when directory deletion was rejected")
	}
}

func TestDeleteFragmentedFileFreesFATChain(t *testing.T) {
	const (
		sectorSize    = 512
		sectorsPerFAT = 1
		numberOfFATs  = 1
		totalSectors  = 30
		rootCluster   = uint32(2)

		firstCluster  = uint32(5)
		secondCluster = uint32(9)
		thirdCluster  = uint32(14)
	)

	data := make([]byte, sectorSize*totalSectors)

	fatOffset := int64(0)

	// Root directory remains allocated.
	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(rootCluster)*4:],
		fat32EOF,
	)

	// Fragmented file chain: 5 -> 9 -> 14 -> EOF.
	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(firstCluster)*4:],
		secondCluster,
	)

	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(secondCluster)*4:],
		thirdCluster,
	)

	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(thirdCluster)*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: &writerAtBuffer{data: data},
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      numberOfFATs,
			SectorsPerFAT:     sectorsPerFAT,
			TotalSectors:      totalSectors,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      sectorSize * 2,
			BytesPerCluster: sectorSize,
		},
	}

	entry := encodeDirectoryEntry(
		[11]byte{'F', 'R', 'A', 'G', 'M', 'E', 'N', 'T', 'B', 'I', 'N'},
		0x20,
		firstCluster,
		sectorSize*2+1,
	)

	dirOffset := int64(sectorSize * 2)

	copy(data[dirOffset:dirOffset+32], entry[:])
	data[dirOffset+32] = 0x00

	if err := fs.Delete("FRAGMENT.BIN"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	for _, cluster := range []uint32{
		firstCluster,
		secondCluster,
		thirdCluster,
	} {
		free, err := fs.IsFreeCluster(cluster)
		if err != nil {
			t.Fatal(err)
		}

		if !free {
			t.Fatalf(
				"cluster %d is still allocated after deletion",
				cluster,
			)
		}
	}

	free, err := fs.IsFreeCluster(rootCluster)
	if err != nil {
		t.Fatal(err)
	}

	if free {
		t.Fatal("root directory cluster was freed")
	}
}

func TestDeleteEmptyFileDoesNotTouchFAT(t *testing.T) {
	const (
		sectorSize    = 512
		sectorsPerFAT = 1
		numberOfFATs  = 1
		totalSectors  = 20
		rootCluster   = uint32(2)
	)

	data := make([]byte, sectorSize*totalSectors)

	fatOffset := int64(0)

	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(rootCluster)*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: &writerAtBuffer{data: data},
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      numberOfFATs,
			SectorsPerFAT:     sectorsPerFAT,
			TotalSectors:      totalSectors,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      sectorSize * 2,
			BytesPerCluster: sectorSize,
		},
	}

	entry := encodeDirectoryEntry(
		[11]byte{'E', 'M', 'P', 'T', 'Y', ' ', ' ', ' ', 'T', 'X', 'T'},
		0x20,
		0,
		0,
	)

	dirOffset := int64(sectorSize * 2)

	copy(data[dirOffset:dirOffset+32], entry[:])
	data[dirOffset+32] = 0x00

	fatStart := int(fatOffset)
	fatEnd := fatStart + sectorSize

	beforeFAT := append(
		[]byte(nil),
		data[fatStart:fatEnd]...,
	)

	if err := fs.Delete("EMPTY.TXT"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	afterFAT := data[fatStart:fatEnd]

	if !bytes.Equal(afterFAT, beforeFAT) {
		t.Fatal("FAT changed when deleting an empty file")
	}

	if data[dirOffset] != 0xE5 {
		t.Fatalf(
			"directory entry marker = 0x%02x, want 0xE5",
			data[dirOffset],
		)
	}
}

func TestDeleteNonexistentFileDoesNotMutate(t *testing.T) {
	const (
		sectorSize    = 512
		sectorsPerFAT = 1
		numberOfFATs  = 1
		totalSectors  = 20
		rootCluster   = uint32(2)
	)

	data := make([]byte, sectorSize*totalSectors)

	fatOffset := int64(0)

	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(rootCluster)*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: &writerAtBuffer{data: data},
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      numberOfFATs,
			SectorsPerFAT:     sectorsPerFAT,
			TotalSectors:      totalSectors,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      sectorSize * 2,
			BytesPerCluster: sectorSize,
		},
	}

	before := append([]byte(nil), data...)

	err := fs.Delete("DOESNOT.EXIST")
	if err == nil {
		t.Fatal("Delete succeeded, want entry-not-found error")
	}

	if !bytes.Contains(
		[]byte(err.Error()),
		[]byte("entry not found"),
	) {
		t.Fatalf(
			"error = %q, want entry-not-found error",
			err,
		)
	}

	if !bytes.Equal(data, before) {
		t.Fatal("filesystem changed when deleting nonexistent file")
	}
}

func TestDeleteFileInSubdirectory(t *testing.T) {
	const (
		sectorSize    = 512
		sectorsPerFAT = 1
		numberOfFATs  = 1
		totalSectors  = 30

		rootCluster = uint32(2)
		dirCluster  = uint32(3)
		fileCluster = uint32(5)
	)

	data := make([]byte, sectorSize*totalSectors)

	fatOffset := int64(0)

	// Root directory.
	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(rootCluster)*4:],
		fat32EOF,
	)

	// Subdirectory.
	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(dirCluster)*4:],
		fat32EOF,
	)

	// File.
	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(fileCluster)*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: &writerAtBuffer{data: data},
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      numberOfFATs,
			SectorsPerFAT:     sectorsPerFAT,
			TotalSectors:      totalSectors,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      sectorSize * 2,
			BytesPerCluster: sectorSize,
		},
	}

	rootOffset := int64(sectorSize * 2)
	dirOffset := int64(sectorSize * 3)

	// Root entry: TESTDIR/
	dirEntry := encodeDirectoryEntry(
		[11]byte{'T', 'E', 'S', 'T', 'D', 'I', 'R', ' ', ' ', ' ', ' '},
		0x10,
		dirCluster,
		0,
	)

	copy(data[rootOffset:rootOffset+32], dirEntry[:])
	data[rootOffset+32] = 0x00

	// Subdirectory entry: FILE.TXT
	fileEntry := encodeDirectoryEntry(
		[11]byte{'F', 'I', 'L', 'E', ' ', ' ', ' ', ' ', 'T', 'X', 'T'},
		0x20,
		fileCluster,
		1,
	)

	copy(data[dirOffset:dirOffset+32], fileEntry[:])
	data[dirOffset+32] = 0x00

	if err := fs.Delete("TESTDIR/FILE.TXT"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if data[dirOffset] != 0xE5 {
		t.Fatalf(
			"file directory entry marker = 0x%02x, want 0xE5",
			data[dirOffset],
		)
	}

	free, err := fs.IsFreeCluster(fileCluster)
	if err != nil {
		t.Fatal(err)
	}

	if !free {
		t.Fatalf("file cluster %d is still allocated", fileCluster)
	}

	free, err = fs.IsFreeCluster(dirCluster)
	if err != nil {
		t.Fatal(err)
	}

	if free {
		t.Fatalf("directory cluster %d was freed", dirCluster)
	}
}

func TestDeleteThroughFilePathDoesNotMutate(t *testing.T) {
	const (
		sectorSize    = 512
		sectorsPerFAT = 1
		numberOfFATs  = 1
		totalSectors  = 30

		rootCluster = uint32(2)
		fileCluster = uint32(5)
	)

	data := make([]byte, sectorSize*totalSectors)

	fatOffset := int64(0)

	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(rootCluster)*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(fileCluster)*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: &writerAtBuffer{data: data},
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      numberOfFATs,
			SectorsPerFAT:     sectorsPerFAT,
			TotalSectors:      totalSectors,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      sectorSize * 2,
			BytesPerCluster: sectorSize,
		},
	}

	dirOffset := int64(sectorSize * 2)

	entry := encodeDirectoryEntry(
		[11]byte{'F', 'I', 'L', 'E', ' ', ' ', ' ', ' ', 'T', 'X', 'T'},
		0x20,
		fileCluster,
		1,
	)

	copy(data[dirOffset:dirOffset+32], entry[:])
	data[dirOffset+32] = 0x00

	before := append([]byte(nil), data...)

	err := fs.Delete("FILE.TXT/CHILD.TXT")
	if err == nil {
		t.Fatal("Delete succeeded, want not-a-directory error")
	}

	if !bytes.Contains(
		[]byte(err.Error()),
		[]byte("is not a directory"),
	) {
		t.Fatalf(
			"error = %q, want not-a-directory error",
			err,
		)
	}

	if !bytes.Equal(data, before) {
		t.Fatal("filesystem changed when parent path was a file")
	}
}

func TestDeleteFATFailureRollsBackCurrentFATEntry(t *testing.T) {
	const (
		sectorSize    = 512
		sectorsPerFAT = 1
		numberOfFATs  = 1
		totalSectors  = 30

		rootCluster   = uint32(2)
		firstCluster  = uint32(5)
		secondCluster = uint32(9)
	)

	data := make([]byte, sectorSize*totalSectors)

	fatOffset := int64(0)
	dataOffset := int64(sectorSize * 2)

	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(rootCluster)*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(firstCluster)*4:],
		secondCluster,
	)

	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(secondCluster)*4:],
		fat32EOF,
	)

	buffer := &failDirectoryAndFATRollbackBuffer{
		data:              data,
		directoryOffset:   dataOffset,
		failFATWriteCount: 2,
	}

	fs := &Filesystem{
		reader: buffer,
		writer: buffer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      numberOfFATs,
			SectorsPerFAT:     sectorsPerFAT,
			TotalSectors:      totalSectors,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: sectorSize,
		},
	}

	entry := encodeDirectoryEntry(
		[11]byte{'F', 'I', 'L', 'E', ' ', ' ', ' ', ' ', 'T', 'X', 'T'},
		0x20,
		firstCluster,
		sectorSize+1,
	)

	copy(data[dataOffset:dataOffset+32], entry[:])
	data[dataOffset+32] = 0x00

	if err := fs.Delete("FILE.TXT"); err == nil {
		t.Fatal("Delete succeeded, want FAT write failure")
	}

	// The directory entry was successfully deleted before FAT freeing
	// began.
	if data[dataOffset] != 0xE5 {
		t.Fatalf(
			"directory entry marker = 0x%02x, want 0xE5",
			data[dataOffset],
		)
	}

	// The first cluster was freed successfully.
	free, err := fs.IsFreeCluster(firstCluster)
	if err != nil {
		t.Fatal(err)
	}

	if !free {
		t.Fatalf(
			"first file cluster %d was not freed",
			firstCluster,
		)
	}

	// The second FAT update failed, so its original EOF value must
	// still be present.
	free, err = fs.IsFreeCluster(secondCluster)
	if err != nil {
		t.Fatal(err)
	}

	if free {
		t.Fatalf(
			"second file cluster %d was freed despite injected failure",
			secondCluster,
		)
	}

	value, err := fs.readFATEntry(secondCluster)
	if err != nil {
		t.Fatal(err)
	}

	if value != fat32EOF {
		t.Fatalf(
			"second file cluster FAT value = %#x, want EOF",
			value,
		)
	}
}

type failDirectoryAndFATRollbackBuffer struct {
	data []byte

	directoryOffset         int64
	failDirectoryWrite      bool
	failDirectoryWriteCount int
	directoryWrites         int

	fatOffset         int64
	failFATWriteCount int
	fatWrites         int
}

func (b *failDirectoryAndFATRollbackBuffer) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off+int64(len(p)) > int64(len(b.data)) {
		return 0, io.EOF
	}

	start := int(off)
	end := start + len(p)

	copy(p, b.data[start:end])

	return len(p), nil
}

func (b *failDirectoryAndFATRollbackBuffer) WriteAt(p []byte, off int64) (int, error) {
	if off == b.directoryOffset {
		b.directoryWrites++

		if b.failDirectoryWrite ||
			(b.failDirectoryWriteCount > 0 &&
				b.directoryWrites == b.failDirectoryWriteCount) {
			return 0, errors.New("injected directory write failure")
		}
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

	start := int(off)
	end := start + len(p)

	copy(b.data[start:end], p)

	return len(p), nil
}

func TestDeleteLongFilenameFailureDoesNotPartiallyDeleteRecord(t *testing.T) {
	const (
		sectorSize    = 512
		sectorsPerFAT = 1
		numberOfFATs  = 1
		totalSectors  = 30

		rootCluster = uint32(2)
		fileCluster = uint32(5)
	)

	data := make([]byte, sectorSize*totalSectors)

	fatOffset := int64(0)
	dataOffset := int64(sectorSize * 2)

	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(rootCluster)*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(fileCluster)*4:],
		fat32EOF,
	)

	buffer := &failDirectoryAndFATRollbackBuffer{
		data:                    data,
		directoryOffset:         dataOffset,
		failDirectoryWriteCount: 2,
		fatOffset:               fatOffset,
	}

	fs := &Filesystem{
		reader: buffer,
		writer: buffer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      numberOfFATs,
			SectorsPerFAT:     sectorsPerFAT,
			TotalSectors:      totalSectors,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: sectorSize,
		},
	}

	name := "A very long filename.txt"

	shortName, err := encodeShortName("A~1.TXT")
	if err != nil {
		t.Fatal(err)
	}

	shortEntryForChecksum := encodeDirectoryEntry(
		shortName,
		0x20,
		fileCluster,
		1,
	)

	checksum := shortNameChecksum(shortEntryForChecksum[:])

	lfnEntries, err := encodeLFNEntries(name, checksum)
	if err != nil {
		t.Fatal(err)
	}

	if len(lfnEntries) < 2 {
		t.Fatalf(
			"LFN entry count = %d, want at least 2",
			len(lfnEntries),
		)
	}

	for i, lfn := range lfnEntries {
		copy(
			data[dataOffset+int64(i*32):dataOffset+int64(i*32)+32],
			lfn[:],
		)
	}

	shortEntry := encodeDirectoryEntry(
		shortName,
		0x20,
		fileCluster,
		1,
	)

	shortOffset := dataOffset + int64(len(lfnEntries)*32)

	copy(
		data[shortOffset:shortOffset+32],
		shortEntry[:],
	)

	data[shortOffset+32] = 0x00

	before := append([]byte(nil), data...)

	err = fs.Delete(name)
	if err == nil {
		t.Fatal("Delete succeeded, want directory write failure")
	}

	if !bytes.Contains(
		[]byte(err.Error()),
		[]byte("injected directory write failure"),
	) {
		t.Fatalf(
			"error = %q, want injected directory write failure",
			err,
		)
	}

	if !bytes.Equal(data, before) {
		t.Fatal(
			"directory record was partially modified after deletion failure",
		)
	}

	free, err := fs.IsFreeCluster(fileCluster)
	if err != nil {
		t.Fatal(err)
	}

	if free {
		t.Fatalf(
			"file cluster %d was freed after directory deletion failure",
			fileCluster,
		)
	}
}

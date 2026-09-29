package fat32

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
)

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

// Test deleting a directory.
// Delete must reject a directory without modifying either its directory entry
// or its FAT chain.
func TestDeleteDirectoryWithDotEntriesIsRejectedWithoutMutation(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 40

		rootCluster = uint32(2)
		dirCluster  = uint32(3)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(dirCluster)*4:],
		fat32EOF,
	)

	boot := BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: sectorsPerCluster,
		NumberOfFATs:      numberOfFATs,
		SectorsPerFAT:     sectorsPerFAT,
		TotalSectors:      totalSectors,
		RootCluster:       rootCluster,
	}

	layout := Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: int64(sectorSize),
	}

	dirName, err := encodeShortName("TESTDIR")
	if err != nil {
		t.Fatal(err)
	}

	dirEntry := encodeDirectoryEntry(
		dirName,
		0x10,
		dirCluster,
		0,
	)

	rootOffset := int(dataOffset)

	copy(
		data[rootOffset:rootOffset+32],
		dirEntry[:],
	)

	data[rootOffset+32] = 0x00

	dotEntry, err := encodeSpecialDirectoryEntry(
		".",
		dirCluster,
	)
	if err != nil {
		t.Fatal(err)
	}

	dotDotEntry, err := encodeSpecialDirectoryEntry(
		"..",
		0,
	)
	if err != nil {
		t.Fatal(err)
	}

	dirOffset := int(dataOffset) +
		(int(dirCluster)-2)*int(layout.BytesPerCluster)

	copy(
		data[dirOffset:dirOffset+32],
		dotEntry[:],
	)

	copy(
		data[dirOffset+32:dirOffset+64],
		dotDotEntry[:],
	)

	data[dirOffset+64] = 0x00

	before := append([]byte(nil), data...)

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	err = fs.Delete("TESTDIR")
	if err == nil {
		t.Fatal("Delete succeeded for directory")
	}

	if !bytes.Equal(buffer.data, before) {
		t.Fatal("filesystem was modified when directory deletion was rejected")
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

// Reject deletion of a non-empty directory.
// Verifies that attempting to delete a non-empty directory is rejected
// and leaves the entire filesystem image unchanged.
func TestDeleteNonEmptyDirectoryIsRejectedWithoutMutation(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 60

		rootCluster = uint32(2)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	boot := BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: sectorsPerCluster,
		NumberOfFATs:      numberOfFATs,
		SectorsPerFAT:     sectorsPerFAT,
		TotalSectors:      totalSectors,
		RootCluster:       rootCluster,
	}

	layout := Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: int64(sectorSize),
	}

	// Make root directory empty.
	data[int(dataOffset)] = 0x00

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	if err := fs.Mkdir("PARENT"); err != nil {
		t.Fatal(err)
	}

	if err := fs.WriteFile(
		"PARENT/CHILD.TXT",
		[]byte("content"),
	); err != nil {
		t.Fatal(err)
	}

	before := append([]byte(nil), data...)

	err := fs.Delete("PARENT")

	if err == nil {
		t.Fatal("Delete(PARENT) unexpectedly succeeded")
	}

	if !strings.Contains(err.Error(), "directory") {
		t.Fatalf(
			"Delete(PARENT) error = %q, want directory error",
			err,
		)
	}

	if !bytes.Equal(data, before) {
		t.Fatal("filesystem changed after rejected directory deletion")
	}

	parent, err := fs.findDirectoryEntry(
		rootCluster,
		"PARENT",
	)
	if err != nil {
		t.Fatal(err)
	}

	if parent == nil {
		t.Fatal("PARENT disappeared after rejected deletion")
	}

	child, err := fs.findDirectoryEntry(
		parent.Entry.StartCluster,
		"CHILD.TXT",
	)
	if err != nil {
		t.Fatal(err)
	}

	if child == nil {
		t.Fatal("CHILD.TXT disappeared after rejected deletion")
	}
}

// Delete a file from an expanded directory.
// Verifies that Delete correctly locates and removes an entry
// in a continuation cluster of a directory, while also freeing
// the file's data cluster.
func TestDeleteFileFromExpandedDirectory(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 80

		rootCluster = uint32(2)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	boot := BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: sectorsPerCluster,
		NumberOfFATs:      numberOfFATs,
		SectorsPerFAT:     sectorsPerFAT,
		TotalSectors:      totalSectors,
		RootCluster:       rootCluster,
	}

	layout := Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: int64(sectorSize),
	}

	data[int(dataOffset)] = 0x00

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	for i := 0; i < 16; i++ {
		name := fmt.Sprintf("FILE%02d.TXT", i)

		if err := fs.WriteFile(
			name,
			[]byte("content"),
		); err != nil {
			t.Fatalf(
				"WriteFile(%q): %v",
				name,
				err,
			)
		}
	}

	target := "TARGET.TXT"

	if err := fs.WriteFile(
		target,
		[]byte("target content"),
	); err != nil {
		t.Fatal(err)
	}

	beforeDelete, err := fs.findDirectoryEntry(
		rootCluster,
		target,
	)
	if err != nil {
		t.Fatal(err)
	}

	if beforeDelete == nil {
		t.Fatal("TARGET.TXT was not created")
	}

	if beforeDelete.Short.Cluster == rootCluster {
		t.Fatal(
			"TARGET.TXT is still in the first directory cluster; " +
				"test did not exercise expanded-directory deletion",
		)
	}

	targetCluster := beforeDelete.Entry.StartCluster

	if targetCluster < 2 {
		t.Fatalf(
			"TARGET.TXT start cluster = %d, want valid cluster",
			targetCluster,
		)
	}

	free, err := fs.IsFreeCluster(targetCluster)
	if err != nil {
		t.Fatal(err)
	}

	if free {
		t.Fatalf(
			"TARGET.TXT data cluster %d is unexpectedly free",
			targetCluster,
		)
	}

	if err := fs.Delete(target); err != nil {
		t.Fatal(err)
	}

	afterDelete, err := fs.findDirectoryEntry(
		rootCluster,
		target,
	)
	if err != nil {
		t.Fatal(err)
	}

	if afterDelete != nil {
		t.Fatal("TARGET.TXT still exists after deletion")
	}

	free, err = fs.IsFreeCluster(targetCluster)
	if err != nil {
		t.Fatal(err)
	}

	if !free {
		t.Fatalf(
			"TARGET.TXT data cluster %d is still allocated",
			targetCluster,
		)
	}
}

// LFN spanning a directory-cluster boundary.
func TestDeleteLongFilenameAcrossDirectoryClusterBoundary(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 80

		rootCluster   = uint32(2)
		secondCluster = uint32(3)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		secondCluster,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(secondCluster)*4:],
		fat32EOF,
	)

	boot := BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: sectorsPerCluster,
		NumberOfFATs:      numberOfFATs,
		SectorsPerFAT:     sectorsPerFAT,
		TotalSectors:      totalSectors,
		RootCluster:       rootCluster,
	}

	layout := Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: int64(sectorSize),
	}

	// Leave the first 14 directory slots occupied. Slots 14 and 15
	// are available, so a long filename requiring at least four
	// entries must cross the cluster boundary.
	for i := 0; i < 14; i++ {
		name := fmt.Sprintf("FILE%02d.TXT", i)

		shortName, err := encodeShortName(name)
		if err != nil {
			t.Fatal(err)
		}

		entry := encodeDirectoryEntry(
			shortName,
			0x20,
			0,
			0,
		)

		offset := int(dataOffset) + i*32

		copy(
			data[offset:offset+32],
			entry[:],
		)
	}

	data[int(dataOffset)+14*32] = 0xE5
	data[int(dataOffset)+15*32] = 0xE5

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	name := "This is a filename that crosses the directory cluster boundary.txt"

	content := []byte("content")

	if err := fs.WriteFile(name, content); err != nil {
		t.Fatal(err)
	}

	record, err := fs.findDirectoryEntry(
		rootCluster,
		name,
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("long filename was not created")
	}

	if len(record.LFN) < 2 {
		t.Fatalf(
			"LFN entry count = %d, want at least 2",
			len(record.LFN),
		)
	}

	if record.Short.Cluster != secondCluster {
		t.Fatalf(
			"short entry cluster = %d, want %d",
			record.Short.Cluster,
			secondCluster,
		)
	}

	// Capture the physical locations before deletion. Delete will
	// remove the directory record, so these locations must be saved
	// now in order to verify the physical changes afterward.
	locations := make([]directoryEntryLocation, 0, len(record.LFN)+1)

	locations = append(locations, record.LFN...)
	locations = append(locations, record.Short)

	// Confirm that the record actually crosses the directory-cluster
	// boundary.
	crossesBoundary := false

	for _, location := range locations {
		if location.Cluster == rootCluster {
			continue
		}

		if location.Cluster == secondCluster {
			crossesBoundary = true
			break
		}
	}

	if !crossesBoundary {
		t.Fatal(
			"long filename record does not cross the directory cluster boundary",
		)
	}

	if err := fs.Delete(name); err != nil {
		t.Fatal(err)
	}

	deletedRecord, err := fs.findDirectoryEntry(
		rootCluster,
		name,
	)
	if err != nil {
		t.Fatal(err)
	}

	if deletedRecord != nil {
		t.Fatal("long filename still exists after deletion")
	}

	// Every physical directory entry belonging to the record must
	// now have the FAT32 deleted-entry marker 0xE5.
	for i, location := range locations {
		clusterData, err := fs.readCluster(location.Cluster)
		if err != nil {
			t.Fatal(err)
		}

		offset := int(location.Offset)

		if offset < 0 || offset >= len(clusterData) {
			t.Fatalf(
				"deleted entry %d has invalid offset %d in cluster %d",
				i,
				location.Offset,
				location.Cluster,
			)
		}

		if clusterData[offset] != 0xE5 {
			t.Fatalf(
				"deleted entry %d at cluster %d offset %d = 0x%02x, want 0xE5",
				i,
				location.Cluster,
				location.Offset,
				clusterData[offset],
			)
		}
	}

	// The file's data cluster must also have been freed.
	free, err := fs.IsFreeCluster(record.Entry.StartCluster)
	if err != nil {
		t.Fatal(err)
	}

	if !free {
		t.Fatalf(
			"file data cluster %d is still allocated",
			record.Entry.StartCluster,
		)
	}
}

// Reuse a cross-cluster deleted LFN record.
// The free run begins in one directory cluster and continues into the next.
func TestDeleteLongFilenameAcrossDirectoryClusterBoundaryReusesSlots(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 80

		rootCluster   = uint32(2)
		secondCluster = uint32(3)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		secondCluster,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(secondCluster)*4:],
		fat32EOF,
	)

	boot := BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: sectorsPerCluster,
		NumberOfFATs:      numberOfFATs,
		SectorsPerFAT:     sectorsPerFAT,
		TotalSectors:      totalSectors,
		RootCluster:       rootCluster,
	}

	layout := Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: int64(sectorSize),
	}

	// Occupy the first 14 directory slots. The remaining two slots
	// in the first cluster are free, forcing a sufficiently long LFN
	// record to cross into the second cluster.
	for i := 0; i < 14; i++ {
		name := fmt.Sprintf("FILE%02d.TXT", i)

		shortName, err := encodeShortName(name)
		if err != nil {
			t.Fatal(err)
		}

		entry := encodeDirectoryEntry(
			shortName,
			0x20,
			0,
			0,
		)

		offset := int(dataOffset) + i*32

		copy(
			data[offset:offset+32],
			entry[:],
		)
	}

	data[int(dataOffset)+14*32] = 0xE5
	data[int(dataOffset)+15*32] = 0xE5

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	name := "This is a filename that crosses the directory cluster boundary.txt"

	if err := fs.WriteFile(
		name,
		[]byte("content"),
	); err != nil {
		t.Fatal(err)
	}

	record, err := fs.findDirectoryEntry(
		rootCluster,
		name,
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("long filename was not created")
	}

	if len(record.LFN) < 2 {
		t.Fatalf(
			"LFN entry count = %d, want at least 2",
			len(record.LFN),
		)
	}

	locations := make(
		[]directoryEntryLocation,
		0,
		len(record.LFN)+1,
	)

	locations = append(locations, record.LFN...)
	locations = append(locations, record.Short)

	crossesBoundary := false

	for _, location := range locations {
		if location.Cluster == secondCluster {
			crossesBoundary = true
			break
		}
	}

	if !crossesBoundary {
		t.Fatal(
			"long filename record does not cross the directory cluster boundary",
		)
	}

	originalEntryCount := len(locations)

	if err := fs.Delete(name); err != nil {
		t.Fatal(err)
	}

	// Same UTF-16 length as the original filename, but a different
	// name, so it requires exactly the same number of LFN entries.
	replacementName := "That is a filename that crosses the directory cluster boundary.txt"

	if len([]rune(replacementName)) != len([]rune(name)) {
		t.Fatalf(
			"replacement filename length = %d runes, want %d",
			len([]rune(replacementName)),
			len([]rune(name)),
		)
	}

	if err := fs.WriteFile(
		replacementName,
		[]byte("replacement"),
	); err != nil {
		t.Fatal(err)
	}

	replacement, err := fs.findDirectoryEntry(
		rootCluster,
		replacementName,
	)
	if err != nil {
		t.Fatal(err)
	}

	if replacement == nil {
		t.Fatal("replacement file was not created")
	}

	replacementLocations := make(
		[]directoryEntryLocation,
		0,
		len(replacement.LFN)+1,
	)

	replacementLocations = append(
		replacementLocations,
		replacement.LFN...,
	)
	replacementLocations = append(
		replacementLocations,
		replacement.Short,
	)

	if len(replacementLocations) != originalEntryCount {
		t.Fatalf(
			"replacement entry count = %d, want %d",
			len(replacementLocations),
			originalEntryCount,
		)
	}

	// The replacement must reuse exactly the physical slots that
	// were occupied by the deleted record.
	for i := range locations {
		if replacementLocations[i] != locations[i] {
			t.Fatalf(
				"replacement entry %d at cluster %d offset %d, want cluster %d offset %d",
				i,
				replacementLocations[i].Cluster,
				replacementLocations[i].Offset,
				locations[i].Cluster,
				locations[i].Offset,
			)
		}
	}
}

// Replace a file in an expanded directory.
// Check that ReplaceFile can update the short directory entry
// in a continuation cluster, while preserving its physical
// directory location and correctly replacing/freeing the data chain.
func TestReplaceFileInExpandedDirectory(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 80

		rootCluster = uint32(2)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	boot := BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: sectorsPerCluster,
		NumberOfFATs:      numberOfFATs,
		SectorsPerFAT:     sectorsPerFAT,
		TotalSectors:      totalSectors,
		RootCluster:       rootCluster,
	}

	layout := Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: int64(sectorSize),
	}

	data[int(dataOffset)] = 0x00

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	// Fill the first root-directory cluster.
	for i := 0; i < 16; i++ {
		name := fmt.Sprintf("FILE%02d.TXT", i)

		if err := fs.WriteFile(
			name,
			[]byte("content"),
		); err != nil {
			t.Fatalf(
				"WriteFile(%q): %v",
				name,
				err,
			)
		}
	}

	name := "TARGET.TXT"

	if err := fs.WriteFile(
		name,
		[]byte("initial content"),
	); err != nil {
		t.Fatal(err)
	}

	record, err := fs.findDirectoryEntry(
		rootCluster,
		name,
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("TARGET.TXT was not created")
	}

	if record.Short.Cluster == rootCluster {
		t.Fatal(
			"TARGET.TXT is still in the first directory cluster; " +
				"test did not exercise expanded-directory replacement",
		)
	}

	oldStartCluster := record.Entry.StartCluster

	replacement := []byte(
		"replacement content that is longer than the original",
	)

	if err := fs.ReplaceFile(name, replacement); err != nil {
		t.Fatal(err)
	}

	record, err = fs.findDirectoryEntry(
		rootCluster,
		name,
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("TARGET.TXT disappeared after replacement")
	}

	if record.Short.Cluster == rootCluster {
		t.Fatal(
			"TARGET.TXT moved to the first directory cluster",
		)
	}

	if record.Entry.Size != uint32(len(replacement)) {
		t.Fatalf(
			"file size = %d, want %d",
			record.Entry.Size,
			len(replacement),
		)
	}

	content, err := fs.readFile(record.Entry)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(content, replacement) {
		t.Fatalf(
			"file contents = %q, want %q",
			content,
			replacement,
		)
	}

	if record.Entry.StartCluster == oldStartCluster {
		t.Fatal(
			"replacement unexpectedly reused old data cluster",
		)
	}

	free, err := fs.IsFreeCluster(oldStartCluster)
	if err != nil {
		t.Fatal(err)
	}

	if !free {
		t.Fatalf(
			"old data cluster %d is still allocated",
			oldStartCluster,
		)
	}
}

// Replace a long filename in an expanded directory.
// Replacement is data-only: the LFN entries and their physical locations
// remain untouched, while the short entry's cluster/size fields are updated.
func TestReplaceLongFilenameInExpandedDirectory(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 80

		rootCluster = uint32(2)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	boot := BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: sectorsPerCluster,
		NumberOfFATs:      numberOfFATs,
		SectorsPerFAT:     sectorsPerFAT,
		TotalSectors:      totalSectors,
		RootCluster:       rootCluster,
	}

	layout := Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: int64(sectorSize),
	}

	data[int(dataOffset)] = 0x00

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	// Fill the first root-directory cluster so that the target
	// long filename must be placed in the expanded cluster.
	for i := 0; i < 16; i++ {
		name := fmt.Sprintf("FILE%02d.TXT", i)

		if err := fs.WriteFile(
			name,
			[]byte("content"),
		); err != nil {
			t.Fatalf(
				"WriteFile(%q): %v",
				name,
				err,
			)
		}
	}

	name := "This is a long filename in an expanded directory.txt"

	initial := []byte("initial content")

	if err := fs.WriteFile(name, initial); err != nil {
		t.Fatal(err)
	}

	record, err := fs.findDirectoryEntry(
		rootCluster,
		name,
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("long filename was not created")
	}

	if len(record.LFN) == 0 {
		t.Fatal("long filename has no LFN entries")
	}

	if record.Short.Cluster == rootCluster {
		t.Fatal(
			"long filename is still in the first directory cluster; " +
				"test did not exercise expanded-directory replacement",
		)
	}

	originalShortLocation := record.Short

	originalLFNLocations := append(
		[]directoryEntryLocation(nil),
		record.LFN...,
	)

	replacement := []byte(
		"replacement content that is longer",
	)

	if err := fs.ReplaceFile(name, replacement); err != nil {
		t.Fatal(err)
	}

	record, err = fs.findDirectoryEntry(
		rootCluster,
		name,
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("long filename disappeared after replacement")
	}

	if len(record.LFN) != len(originalLFNLocations) {
		t.Fatalf(
			"LFN entry count = %d, want %d",
			len(record.LFN),
			len(originalLFNLocations),
		)
	}

	if record.Short != originalShortLocation {
		t.Fatalf(
			"short entry moved from cluster %d offset %d to cluster %d offset %d",
			originalShortLocation.Cluster,
			originalShortLocation.Offset,
			record.Short.Cluster,
			record.Short.Offset,
		)
	}

	for i := range originalLFNLocations {
		if record.LFN[i] != originalLFNLocations[i] {
			t.Fatalf(
				"LFN entry %d moved from cluster %d offset %d to cluster %d offset %d",
				i,
				originalLFNLocations[i].Cluster,
				originalLFNLocations[i].Offset,
				record.LFN[i].Cluster,
				record.LFN[i].Offset,
			)
		}
	}

	if record.Entry.Size != uint32(len(replacement)) {
		t.Fatalf(
			"file size = %d, want %d",
			record.Entry.Size,
			len(replacement),
		)
	}

	content, err := fs.readFile(record.Entry)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(content, replacement) {
		t.Fatalf(
			"file contents = %q, want %q",
			content,
			replacement,
		)
	}
}

// Replacing an empty file with another empty file.
// This should update nothing in the FAT and should leave the directory entry valid.
// ReplaceFile should not unnecessarily allocate a temporary cluster chain
// when both the old and new files have size zero.
func TestReplaceEmptyFileWithEmptyFile(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 80

		rootCluster = uint32(2)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	boot := BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: sectorsPerCluster,
		NumberOfFATs:      numberOfFATs,
		SectorsPerFAT:     sectorsPerFAT,
		TotalSectors:      totalSectors,
		RootCluster:       rootCluster,
	}

	layout := Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: int64(sectorSize),
	}

	data[int(dataOffset)] = 0x00

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	const name = "EMPTY.TXT"

	if err := fs.WriteFile(name, nil); err != nil {
		t.Fatal(err)
	}

	record, err := fs.findDirectoryEntry(
		rootCluster,
		name,
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("empty file was not created")
	}

	if record.Entry.StartCluster != 0 {
		t.Fatalf(
			"empty file start cluster = %d, want 0",
			record.Entry.StartCluster,
		)
	}

	before := append([]byte(nil), data...)

	if err := fs.ReplaceFile(name, nil); err != nil {
		t.Fatal(err)
	}

	record, err = fs.findDirectoryEntry(
		rootCluster,
		name,
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("empty file disappeared after replacement")
	}

	if record.Entry.Size != 0 {
		t.Fatalf(
			"file size = %d, want 0",
			record.Entry.Size,
		)
	}

	if record.Entry.StartCluster != 0 {
		t.Fatalf(
			"empty file start cluster = %d, want 0",
			record.Entry.StartCluster,
		)
	}

	content, err := fs.readFile(record.Entry)
	if err != nil {
		t.Fatal(err)
	}

	if len(content) != 0 {
		t.Fatalf(
			"file contents length = %d, want 0",
			len(content),
		)
	}

	// No FAT or directory data should have changed.
	if !bytes.Equal(data, before) {
		t.Fatal("replacing an empty file with an empty file mutated the image")
	}
}

// Non empty -> empty replacement/
// Allocation of no new clusters and freeing the entire old chain
// after the directory entry is committed.
func TestReplaceFileWithEmptyFileFreesOldChain(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 80

		rootCluster = uint32(2)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	boot := BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: sectorsPerCluster,
		NumberOfFATs:      numberOfFATs,
		SectorsPerFAT:     sectorsPerFAT,
		TotalSectors:      totalSectors,
		RootCluster:       rootCluster,
	}

	layout := Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: int64(sectorSize),
	}

	data[int(dataOffset)] = 0x00

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	const name = "DATA.TXT"

	initial := bytes.Repeat([]byte("A"), 700)

	if err := fs.WriteFile(name, initial); err != nil {
		t.Fatal(err)
	}

	record, err := fs.findDirectoryEntry(
		rootCluster,
		name,
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("file was not created")
	}

	oldChain, err := fs.ClusterChain(record.Entry.StartCluster)
	if err != nil {
		t.Fatal(err)
	}

	if len(oldChain) != 2 {
		t.Fatalf(
			"old cluster chain length = %d, want 2",
			len(oldChain),
		)
	}

	if err := fs.ReplaceFile(name, nil); err != nil {
		t.Fatal(err)
	}

	record, err = fs.findDirectoryEntry(
		rootCluster,
		name,
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("file disappeared after replacement")
	}

	if record.Entry.Size != 0 {
		t.Fatalf(
			"file size = %d, want 0",
			record.Entry.Size,
		)
	}

	if record.Entry.StartCluster != 0 {
		t.Fatalf(
			"file start cluster = %d, want 0",
			record.Entry.StartCluster,
		)
	}

	content, err := fs.readFile(record.Entry)
	if err != nil {
		t.Fatal(err)
	}

	if len(content) != 0 {
		t.Fatalf(
			"file contents length = %d, want 0",
			len(content),
		)
	}

	for _, cluster := range oldChain {
		free, err := fs.IsFreeCluster(cluster)
		if err != nil {
			t.Fatal(err)
		}

		if !free {
			t.Fatalf(
				"old cluster %d is still allocated",
				cluster,
			)
		}
	}
}

// Verify Delete is case-insensitive
func TestDeleteFileIsCaseInsensitive(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 80

		rootCluster = uint32(2)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	boot := BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: sectorsPerCluster,
		NumberOfFATs:      numberOfFATs,
		SectorsPerFAT:     sectorsPerFAT,
		TotalSectors:      totalSectors,
		RootCluster:       rootCluster,
	}

	layout := Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: int64(sectorSize),
	}

	data[int(dataOffset)] = 0x00

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	const filename = "Example.TXT"

	if err := fs.WriteFile(
		filename,
		[]byte("hello"),
	); err != nil {
		t.Fatal(err)
	}

	if err := fs.Delete("example.txt"); err != nil {
		t.Fatalf(
			"Delete with different case: %v",
			err,
		)
	}

	if _, err := fs.ReadFile(filename); err == nil {
		t.Fatal("ReadFile succeeded after case-insensitive Delete")
	}
}

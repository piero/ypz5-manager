package fat32

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"testing"
)

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

// Verify files can still be created inside a newly created directory
// The normal directory-slot scanner skips the . and .. entries
// and places the first real file after them.
func TestWriteFileInNewDirectory(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 40

		rootCluster   = uint32(2)
		parentCluster = uint32(3)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(parentCluster)*4:],
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

	parentShortName, err := encodeShortName("PARENT")
	if err != nil {
		t.Fatal(err)
	}

	parentEntry := encodeDirectoryEntry(
		parentShortName,
		0x10,
		parentCluster,
		0,
	)

	rootOffset := int(dataOffset)

	copy(
		data[rootOffset:rootOffset+32],
		parentEntry[:],
	)

	data[rootOffset+32] = 0x00

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

	if err := fs.Mkdir("PARENT/CHILD"); err != nil {
		t.Fatal(err)
	}

	child, err := fs.findDirectoryEntry(
		parentCluster,
		"CHILD",
	)
	if err != nil {
		t.Fatal(err)
	}

	if child == nil {
		t.Fatal("child directory was not created")
	}

	dataToWrite := []byte("hello from FAT32")

	if err := fs.WriteFile(
		"PARENT/CHILD/TEST.TXT",
		dataToWrite,
	); err != nil {
		t.Fatal(err)
	}

	file, err := fs.findDirectoryEntry(
		child.Entry.StartCluster,
		"TEST.TXT",
	)
	if err != nil {
		t.Fatal(err)
	}

	if file == nil {
		t.Fatal("file was not created")
	}

	if file.Entry.IsDir {
		t.Fatal("TEST.TXT was created as a directory")
	}

	if file.Entry.Size != uint32(len(dataToWrite)) {
		t.Fatalf(
			"file size = %d, want %d",
			file.Entry.Size,
			len(dataToWrite),
		)
	}

	content, err := fs.readFile(file.Entry)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(content, dataToWrite) {
		t.Fatalf(
			"file contents = %q, want %q",
			content,
			dataToWrite,
		)
	}

	childData, err := fs.readCluster(
		child.Entry.StartCluster,
	)
	if err != nil {
		t.Fatal(err)
	}

	if childData[0] == 0x00 || childData[0] == 0xE5 {
		t.Fatal("'.' entry was overwritten")
	}

	if childData[32] == 0x00 || childData[32] == 0xE5 {
		t.Fatal("'..' entry was overwritten")
	}
}

// Invalid WriteFile paths must not mutate.
func TestWriteFileInvalidPathDoesNotMutate(t *testing.T) {
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

	invalidPaths := []string{
		"",
		"/",
		"///",
		"dir/",
		"/dir/",
		"dir//file.txt",
	}

	for _, path := range invalidPaths {
		before := append([]byte(nil), data...)

		if err := fs.WriteFile(path, []byte("data")); err == nil {
			t.Fatalf(
				"WriteFile(%q) unexpectedly succeeded",
				path,
			)
		}

		if !bytes.Equal(data, before) {
			t.Fatalf(
				"WriteFile(%q) mutated the image after failure",
				path,
			)
		}
	}
}

// Ensure that . is a supported path component
func TestWriteFileDotPathComponents(t *testing.T) {
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

	if err := fs.WriteFile(
		"./ROOT.TXT",
		[]byte("root"),
	); err != nil {
		t.Fatalf("WriteFile(./ROOT.TXT): %v", err)
	}

	content, err := fs.ReadFile("./ROOT.TXT")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(content, []byte("root")) {
		t.Fatalf(
			"ROOT.TXT contents = %q, want %q",
			content,
			"root",
		)
	}

	if err := fs.Mkdir("PARENT"); err != nil {
		t.Fatal(err)
	}

	if err := fs.WriteFile(
		"PARENT/./CHILD.TXT",
		[]byte("child"),
	); err != nil {
		t.Fatalf(
			"WriteFile(PARENT/./CHILD.TXT): %v",
			err,
		)
	}

	content, err = fs.ReadFile("PARENT/CHILD.TXT")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(content, []byte("child")) {
		t.Fatalf(
			"CHILD.TXT contents = %q, want %q",
			content,
			"child",
		)
	}

	content, err = fs.ReadFile("./PARENT/./CHILD.TXT")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(content, []byte("child")) {
		t.Fatalf(
			"CHILD.TXT contents through dot path = %q, want %q",
			content,
			"child",
		)
	}

	entry, err := fs.Lookup("./PARENT/./CHILD.TXT")
	if err != nil {
		t.Fatal(err)
	}

	if entry.Name != "CHILD.TXT" {
		t.Fatalf(
			"lookup name = %q, want %q",
			entry.Name,
			"CHILD.TXT",
		)
	}

	if entry.IsDir {
		t.Fatal("lookup returned CHILD.TXT as a directory")
	}
}

// Ensure that .. is a supported path component
func TestWriteFileParentDirectoryPathComponent(t *testing.T) {
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

	if err := fs.Mkdir("PARENT"); err != nil {
		t.Fatal(err)
	}

	if err := fs.Mkdir("PARENT/CHILD"); err != nil {
		t.Fatal(err)
	}

	if err := fs.WriteFile(
		"PARENT/CHILD/../FROM_PARENT.TXT",
		[]byte("parent"),
	); err != nil {
		t.Fatalf(
			"WriteFile(PARENT/CHILD/../FROM_PARENT.TXT): %v",
			err,
		)
	}

	content, err := fs.ReadFile(
		"PARENT/FROM_PARENT.TXT",
	)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(content, []byte("parent")) {
		t.Fatalf(
			"FROM_PARENT.TXT contents = %q, want %q",
			content,
			"parent",
		)
	}
}

// Traversal through GRANDCHILD/../.. correctly returns to PARENT,
// rather than merely proving that a single .. works.
func TestWriteFileMultipleParentDirectoryPathComponents(t *testing.T) {
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

	if err := fs.Mkdir("PARENT"); err != nil {
		t.Fatal(err)
	}

	if err := fs.Mkdir("PARENT/CHILD"); err != nil {
		t.Fatal(err)
	}

	if err := fs.Mkdir("PARENT/CHILD/GRANDCHILD"); err != nil {
		t.Fatal(err)
	}

	if err := fs.WriteFile(
		"PARENT/CHILD/GRANDCHILD/../../ROOT.TXT",
		[]byte("parent"),
	); err != nil {
		t.Fatalf(
			"WriteFile with multiple .. components: %v",
			err,
		)
	}

	content, err := fs.ReadFile(
		"PARENT/ROOT.TXT",
	)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(content, []byte("parent")) {
		t.Fatalf(
			"ROOT.TXT contents = %q, want %q",
			content,
			"parent",
		)
	}
}

// The first write intentionally checks that a component named .... is treated
// as a normal directory name and therefore fails because it doesn't exist.
// The second verifies that PARENT/CHILD/../.. ultimately resolves to the
// root directory.
func TestWriteFileParentOfRootDirectory(t *testing.T) {
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

	if err := fs.Mkdir("PARENT"); err != nil {
		t.Fatal(err)
	}

	if err := fs.Mkdir("PARENT/CHILD"); err != nil {
		t.Fatal(err)
	}

	if err := fs.WriteFile(
		"PARENT/CHILD/../..../INVALID.TXT",
		[]byte("invalid"),
	); err == nil {
		t.Fatal("WriteFile unexpectedly accepted malformed parent path")
	}

	if err := fs.WriteFile(
		"PARENT/CHILD/../../ROOT.TXT",
		[]byte("root"),
	); err != nil {
		t.Fatalf(
			"WriteFile through root-level ..: %v",
			err,
		)
	}

	content, err := fs.ReadFile("ROOT.TXT")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(content, []byte("root")) {
		t.Fatalf(
			"ROOT.TXT contents = %q, want %q",
			content,
			"root",
		)
	}
}

// Traversal above root
func TestWriteFileParentTraversalAboveRootStaysAtRoot(t *testing.T) {
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

	if err := fs.WriteFile(
		"../../ROOT.TXT",
		[]byte("root"),
	); err != nil {
		t.Fatalf(
			"WriteFile through root traversal: %v",
			err,
		)
	}

	content, err := fs.ReadFile("ROOT.TXT")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(content, []byte("root")) {
		t.Fatalf(
			"ROOT.TXT contents = %q, want %q",
			content,
			"root",
		)
	}
}

// Test that a directory-record write failure after expansion rolls back the expansion.
func TestWriteFileDirectoryRecordFailureAfterExpansionRollsBack(t *testing.T) {
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

	// Fill the root directory except for the final end-of-directory entry.
	for offset := 0; offset < bytesPerCluster-32; offset += 32 {
		data[dataOffset+offset] = 0x41
	}

	data[dataOffset+bytesPerCluster-32] = 0x00

	// The first file-data cluster is 3. The expanded directory cluster is 4.
	// Fail only when the new file's directory entry is written.
	newDirectoryCluster := uint32(4)
	directoryWriteOffset := int64(
		dataOffset + int(newDirectoryCluster-2)*bytesPerCluster,
	)

	writer := &failOnWriteCountAt{
		data:       data,
		failOffset: directoryWriteOffset,
		failCount:  2,
	}

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: writer,
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      7,
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

	// The root directory must still be a single-cluster chain.
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

	// The expanded directory cluster must have been released.
	value, err := fs.readFATEntry(newDirectoryCluster)
	if err != nil {
		t.Fatal(err)
	}

	if value != 0 {
		t.Fatalf(
			"expanded directory cluster %d FAT entry = %#x, want free",
			newDirectoryCluster,
			value,
		)
	}

	// The file-data cluster allocated before directory creation must also be free.
	fileDataCluster := uint32(3)

	value, err = fs.readFATEntry(fileDataCluster)
	if err != nil {
		t.Fatal(err)
	}

	if value != 0 {
		t.Fatalf(
			"file data cluster %d FAT entry = %#x, want free",
			fileDataCluster,
			value,
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

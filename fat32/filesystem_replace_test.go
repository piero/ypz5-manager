package fat32

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestReplaceFileRoundTrip(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 30

		rootCluster = uint32(2)
		fileCluster = uint32(3)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(fileCluster)*4:],
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

	shortName, err := encodeShortName("TEST.TXT")
	if err != nil {
		t.Fatal(err)
	}

	shortEntry := encodeDirectoryEntry(
		shortName,
		0x20,
		fileCluster,
		3,
	)

	directoryOffset := int(dataOffset)

	copy(
		data[directoryOffset:directoryOffset+32],
		shortEntry[:],
	)

	data[directoryOffset+32] = 0x00

	oldDataOffset := directoryOffset +
		(int(fileCluster)-2)*int(layout.BytesPerCluster)

	copy(
		data[oldDataOffset:oldDataOffset+3],
		[]byte("old"),
	)

	buffer := writerAtBuffer{data: data}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	if err := fs.ReplaceFile(
		"TEST.TXT",
		[]byte("new contents"),
	); err != nil {
		t.Fatal(err)
	}

	record, err := fs.findDirectoryEntry(
		rootCluster,
		"TEST.TXT",
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("replacement file was not found")
	}

	if record.Entry.Size != uint32(len("new contents")) {
		t.Fatalf(
			"file size = %d, want %d",
			record.Entry.Size,
			len("new contents"),
		)
	}

	if record.Entry.StartCluster == fileCluster {
		t.Fatalf(
			"replacement reused old cluster %d",
			fileCluster,
		)
	}

	file := &File{
		entry: record.Entry,
		fs:    fs,
	}

	got, err := file.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("new contents")) {
		t.Fatalf(
			"replacement contents = %q, want %q",
			got,
			"new contents",
		)
	}

	free, err := fs.IsFreeCluster(fileCluster)
	if err != nil {
		t.Fatal(err)
	}

	if !free {
		t.Fatalf(
			"old cluster %d was not freed",
			fileCluster,
		)
	}
}

func TestReplaceFileInSubdirectory(t *testing.T) {
	// Replace a file using a path inside a subdirector
	// Make sure ReplaceFile("DIR/FILE.TXT", ...) updates the correct directory
	// and doesn't accidentally operate on the root.

	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 40

		rootCluster      = uint32(2)
		directoryCluster = uint32(3)
		fileCluster      = uint32(4)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(directoryCluster)*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(fileCluster)*4:],
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

	dirShortName, err := encodeShortName("MUSIC")
	if err != nil {
		t.Fatal(err)
	}

	dirEntry := encodeDirectoryEntry(
		dirShortName,
		0x10,
		directoryCluster,
		0,
	)

	rootOffset := int(dataOffset)

	copy(
		data[rootOffset:rootOffset+32],
		dirEntry[:],
	)

	data[rootOffset+32] = 0x00

	fileShortName, err := encodeShortName("TEST.TXT")
	if err != nil {
		t.Fatal(err)
	}

	fileEntry := encodeDirectoryEntry(
		fileShortName,
		0x20,
		fileCluster,
		3,
	)

	dirOffset := int(dataOffset) +
		(int(directoryCluster)-2)*int(layout.BytesPerCluster)

	copy(
		data[dirOffset:dirOffset+32],
		fileEntry[:],
	)

	data[dirOffset+32] = 0x00

	fileOffset := int(dataOffset) +
		(int(fileCluster)-2)*int(layout.BytesPerCluster)

	copy(
		data[fileOffset:fileOffset+3],
		[]byte("old"),
	)

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

	if err := fs.ReplaceFile(
		"MUSIC/TEST.TXT",
		[]byte("new"),
	); err != nil {
		t.Fatal(err)
	}

	record, err := fs.findDirectoryEntry(
		directoryCluster,
		"TEST.TXT",
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("replacement file not found in subdirectory")
	}

	if record.Entry.Size != 3 {
		t.Fatalf(
			"file size = %d, want 3",
			record.Entry.Size,
		)
	}

	file := &File{
		entry: record.Entry,
		fs:    fs,
	}

	got, err := file.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("new")) {
		t.Fatalf(
			"replacement contents = %q, want %q",
			got,
			"new",
		)
	}

	rootRecord, err := fs.findDirectoryEntry(
		rootCluster,
		"TEST.TXT",
	)
	if err != nil {
		t.Fatal(err)
	}

	if rootRecord != nil {
		t.Fatal("replacement unexpectedly created TEST.TXT in root")
	}
}

func TestReplaceFileDirectoryIsRejectedWithoutMutation(t *testing.T) {
	// Reject replacement of a directory without mutation.
	// ReplaceFile should protect against treating directories as files.
	// Here we exercis the record.Entry.IsDir check before any allocation or directory mutation.

	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 30

		rootCluster      = uint32(2)
		directoryCluster = uint32(3)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(directoryCluster)*4:],
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

	shortName, err := encodeShortName("MUSIC")
	if err != nil {
		t.Fatal(err)
	}

	directoryEntry := encodeDirectoryEntry(
		shortName,
		0x10,
		directoryCluster,
		0,
	)

	directoryOffset := int(dataOffset)

	copy(
		data[directoryOffset:directoryOffset+32],
		directoryEntry[:],
	)

	data[directoryOffset+32] = 0x00

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

	err = fs.ReplaceFile(
		"MUSIC",
		[]byte("new"),
	)
	if err == nil {
		t.Fatal("ReplaceFile succeeded, want directory rejection")
	}

	if !bytes.Equal(buffer.data, before) {
		t.Fatal("filesystem was modified when replacing a directory")
	}
}

// Replacing nonexistent file must not allocate anything.
// Verify that the existence check happens before new clusters are allocated.
func TestReplaceFileNonexistentFileDoesNotMutate(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 30

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

	data[int(dataOffset)+32] = 0x00

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

	err := fs.ReplaceFile(
		"MISSING.TXT",
		[]byte("new"),
	)
	if err == nil {
		t.Fatal("ReplaceFile succeeded, want nonexistent-file error")
	}

	if !bytes.Equal(buffer.data, before) {
		t.Fatal("filesystem was modified when replacing nonexistent file")
	}
}

func TestReplaceFileWithEmptyFile(t *testing.T) {
	// Test replacing a non-empty file with an empty file.
	// This exercises the StartCluster = 0 path and verifies
	// that the old chain is actually freed.

	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 30

		rootCluster = uint32(2)
		fileCluster = uint32(3)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(fileCluster)*4:],
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

	shortName, err := encodeShortName("TEST.TXT")
	if err != nil {
		t.Fatal(err)
	}

	shortEntry := encodeDirectoryEntry(
		shortName,
		0x20,
		fileCluster,
		3,
	)

	directoryOffset := int(dataOffset)

	copy(
		data[directoryOffset:directoryOffset+32],
		shortEntry[:],
	)

	data[directoryOffset+32] = 0x00

	buffer := writerAtBuffer{data: data}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	if err := fs.ReplaceFile("TEST.TXT", []byte{}); err != nil {
		t.Fatal(err)
	}

	record, err := fs.findDirectoryEntry(
		rootCluster,
		"TEST.TXT",
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("replacement file was not found")
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

	file := &File{
		entry: record.Entry,
		fs:    fs,
	}

	got, err := file.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 0 {
		t.Fatalf(
			"replacement contents = %q, want empty",
			got,
		)
	}

	free, err := fs.IsFreeCluster(fileCluster)
	if err != nil {
		t.Fatal(err)
	}

	if !free {
		t.Fatalf(
			"old cluster %d was not freed",
			fileCluster,
		)
	}
}

// Test replacing a zero-length file with non-empty data
// This specifically exercises the oldChain == nil → newly allocated chain transition.
func TestReplaceFileEmptyToNonEmpty(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 30

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

	shortName, err := encodeShortName("EMPTY.TXT")
	if err != nil {
		t.Fatal(err)
	}

	shortEntry := encodeDirectoryEntry(
		shortName,
		0x20,
		0,
		0,
	)

	directoryOffset := int(dataOffset)

	copy(
		data[directoryOffset:directoryOffset+32],
		shortEntry[:],
	)

	data[directoryOffset+32] = 0x00

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

	newContents := []byte("new contents")

	if err := fs.ReplaceFile(
		"EMPTY.TXT",
		newContents,
	); err != nil {
		t.Fatal(err)
	}

	record, err := fs.findDirectoryEntry(
		rootCluster,
		"EMPTY.TXT",
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("replacement file was removed")
	}

	if record.Entry.StartCluster < 2 {
		t.Fatalf(
			"replacement start cluster = %d, want allocated cluster",
			record.Entry.StartCluster,
		)
	}

	if record.Entry.Size != uint32(len(newContents)) {
		t.Fatalf(
			"file size = %d, want %d",
			record.Entry.Size,
			len(newContents),
		)
	}

	file := &File{
		entry: record.Entry,
		fs:    fs,
	}

	got, err := file.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, newContents) {
		t.Fatalf(
			"replacement contents = %q, want %q",
			got,
			newContents,
		)
	}
}

// Test replacement with data exactly one cluster.
// This verifies that exactly one cluster is allocated for data
// whose size is exactly the cluster size.
func TestReplaceFileExactClusterBoundary(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 30

		rootCluster = uint32(2)
		oldCluster  = uint32(3)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(oldCluster)*4:],
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

	shortName, err := encodeShortName("TEST.TXT")
	if err != nil {
		t.Fatal(err)
	}

	oldContents := []byte("old")

	shortEntry := encodeDirectoryEntry(
		shortName,
		0x20,
		oldCluster,
		uint32(len(oldContents)),
	)

	directoryOffset := int(dataOffset)

	copy(
		data[directoryOffset:directoryOffset+32],
		shortEntry[:],
	)

	data[directoryOffset+32] = 0x00

	oldDataOffset := directoryOffset +
		(int(oldCluster)-2)*int(layout.BytesPerCluster)

	copy(
		data[oldDataOffset:oldDataOffset+len(oldContents)],
		oldContents,
	)

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

	newContents := bytes.Repeat(
		[]byte("n"),
		int(sectorSize),
	)

	if err := fs.ReplaceFile(
		"TEST.TXT",
		newContents,
	); err != nil {
		t.Fatal(err)
	}

	record, err := fs.findDirectoryEntry(
		rootCluster,
		"TEST.TXT",
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("replacement file was removed")
	}

	chain, err := fs.ClusterChain(record.Entry.StartCluster)
	if err != nil {
		t.Fatal(err)
	}

	if len(chain) != 1 {
		t.Fatalf(
			"replacement cluster count = %d, want 1",
			len(chain),
		)
	}

	if record.Entry.Size != uint32(len(newContents)) {
		t.Fatalf(
			"file size = %d, want %d",
			record.Entry.Size,
			len(newContents),
		)
	}

	file := &File{
		entry: record.Entry,
		fs:    fs,
	}

	got, err := file.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, newContents) {
		t.Fatalf(
			"replacement contents do not match",
		)
	}

	free, err := fs.IsFreeCluster(oldCluster)
	if err != nil {
		t.Fatal(err)
	}

	if !free {
		t.Fatalf(
			"old cluster %d is still allocated",
			oldCluster,
		)
	}
}

// Test the one-byte-over-cluster boundary.
// Exactly one byte over a cluster must allocate two clusters.
func TestReplaceFileOneByteOverClusterBoundary(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 30

		rootCluster = uint32(2)
		oldCluster  = uint32(3)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(oldCluster)*4:],
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

	shortName, err := encodeShortName("TEST.TXT")
	if err != nil {
		t.Fatal(err)
	}

	oldContents := []byte("old")

	shortEntry := encodeDirectoryEntry(
		shortName,
		0x20,
		oldCluster,
		uint32(len(oldContents)),
	)

	directoryOffset := int(dataOffset)

	copy(
		data[directoryOffset:directoryOffset+32],
		shortEntry[:],
	)

	data[directoryOffset+32] = 0x00

	oldDataOffset := directoryOffset +
		(int(oldCluster)-2)*int(layout.BytesPerCluster)

	copy(
		data[oldDataOffset:oldDataOffset+len(oldContents)],
		oldContents,
	)

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

	newContents := bytes.Repeat(
		[]byte("n"),
		int(sectorSize)+1,
	)

	if err := fs.ReplaceFile(
		"TEST.TXT",
		newContents,
	); err != nil {
		t.Fatal(err)
	}

	record, err := fs.findDirectoryEntry(
		rootCluster,
		"TEST.TXT",
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("replacement file was removed")
	}

	chain, err := fs.ClusterChain(record.Entry.StartCluster)
	if err != nil {
		t.Fatal(err)
	}

	if len(chain) != 2 {
		t.Fatalf(
			"replacement cluster count = %d, want 2",
			len(chain),
		)
	}

	if record.Entry.Size != uint32(len(newContents)) {
		t.Fatalf(
			"file size = %d, want %d",
			record.Entry.Size,
			len(newContents),
		)
	}

	file := &File{
		entry: record.Entry,
		fs:    fs,
	}

	got, err := file.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, newContents) {
		t.Fatalf(
			"replacement contents do not match",
		)
	}

	free, err := fs.IsFreeCluster(oldCluster)
	if err != nil {
		t.Fatal(err)
	}

	if !free {
		t.Fatalf(
			"old cluster %d is still allocated",
			oldCluster,
		)
	}
}

// Replacement must preserve the old file if new-data allocation fails
// We inject a FAT allocation failure before the directory entry is changed,
// then verify the original file and its cluster remain intact.
func TestReplaceFilePreservesOldFileOnAllocationFailure(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 30

		rootCluster = uint32(2)
		fileCluster = uint32(3)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(fileCluster)*4:],
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

	shortName, err := encodeShortName("TEST.TXT")
	if err != nil {
		t.Fatal(err)
	}

	oldContents := []byte("old")

	shortEntry := encodeDirectoryEntry(
		shortName,
		0x20,
		fileCluster,
		uint32(len(oldContents)),
	)

	directoryOffset := int(dataOffset)

	copy(
		data[directoryOffset:directoryOffset+32],
		shortEntry[:],
	)

	data[directoryOffset+32] = 0x00

	oldDataOffset := directoryOffset +
		(int(fileCluster)-2)*int(layout.BytesPerCluster)

	copy(
		data[oldDataOffset:oldDataOffset+len(oldContents)],
		oldContents,
	)

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: fatOffset,
		failCount:  1,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	err = fs.ReplaceFile(
		"TEST.TXT",
		[]byte("new contents"),
	)
	if err == nil {
		t.Fatal("ReplaceFile succeeded, want allocation failure")
	}

	record, err := fs.findDirectoryEntry(
		rootCluster,
		"TEST.TXT",
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("original file was removed after replacement failure")
	}

	if record.Entry.StartCluster != fileCluster {
		t.Fatalf(
			"start cluster = %d, want original cluster %d",
			record.Entry.StartCluster,
			fileCluster,
		)
	}

	if record.Entry.Size != uint32(len(oldContents)) {
		t.Fatalf(
			"file size = %d, want %d",
			record.Entry.Size,
			len(oldContents),
		)
	}

	file := &File{
		entry: record.Entry,
		fs:    fs,
	}

	got, err := file.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, oldContents) {
		t.Fatalf(
			"original contents = %q, want %q",
			got,
			oldContents,
		)
	}
}

// Test failure updating the directory entry
// 1. Allocate/write the new contents.
// 2. Attempt to update the existing directory entry.
// 3. Directory write fails.
// 4. The newly allocated chain must be freed.
// 5. The original file must remain completely intact.
func TestReplaceFileRollsBackNewChainOnDirectoryWriteFailure(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 30

		rootCluster = uint32(2)
		fileCluster = uint32(3)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(fileCluster)*4:],
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

	shortName, err := encodeShortName("TEST.TXT")
	if err != nil {
		t.Fatal(err)
	}

	oldContents := []byte("old")

	shortEntry := encodeDirectoryEntry(
		shortName,
		0x20,
		fileCluster,
		uint32(len(oldContents)),
	)

	directoryOffset := int(dataOffset)

	copy(
		data[directoryOffset:directoryOffset+32],
		shortEntry[:],
	)

	data[directoryOffset+32] = 0x00

	oldDataOffset := directoryOffset +
		(int(fileCluster)-2)*int(layout.BytesPerCluster)

	copy(
		data[oldDataOffset:oldDataOffset+len(oldContents)],
		oldContents,
	)

	buffer := &failDirectoryAndFATRollbackBuffer{
		data:               data,
		directoryOffset:    dataOffset,
		failDirectoryWrite: true,
		fatOffset:          fatOffset,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	err = fs.ReplaceFile(
		"TEST.TXT",
		[]byte("new contents"),
	)
	if err == nil {
		t.Fatal("ReplaceFile succeeded, want directory write failure")
	}

	record, err := fs.findDirectoryEntry(
		rootCluster,
		"TEST.TXT",
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("original file was removed after replacement failure")
	}

	if record.Entry.StartCluster != fileCluster {
		t.Fatalf(
			"start cluster = %d, want original cluster %d",
			record.Entry.StartCluster,
			fileCluster,
		)
	}

	if record.Entry.Size != uint32(len(oldContents)) {
		t.Fatalf(
			"file size = %d, want %d",
			record.Entry.Size,
			len(oldContents),
		)
	}

	file := &File{
		entry: record.Entry,
		fs:    fs,
	}

	got, err := file.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, oldContents) {
		t.Fatalf(
			"original contents = %q, want %q",
			got,
			oldContents,
		)
	}

	free, err := fs.IsFreeCluster(4)
	if err != nil {
		t.Fatal(err)
	}

	if !free {
		t.Fatal("newly allocated replacement cluster 4 was not freed")
	}

	free, err = fs.IsFreeCluster(fileCluster)
	if err != nil {
		t.Fatal(err)
	}

	if free {
		t.Fatalf(
			"original file cluster %d was freed",
			fileCluster,
		)
	}
}

// Long-filename replacement
//
// This verifies that ReplaceFile:
//
// 1. preserves the existing LFN entries;
// 2. preserves the short name;
// 3. changes only the short entry's start cluster and size;
// 4. replaces the contents correctly;
// 5. frees the old cluster.
func TestReplaceFilePreservesLongFilename(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 30

		rootCluster = uint32(2)
		fileCluster = uint32(3)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(fileCluster)*4:],
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

	name := "A reasonably long filename.txt"

	shortName, err := encodeShortName("A~1.TXT")
	if err != nil {
		t.Fatal(err)
	}

	shortEntry := encodeDirectoryEntry(
		shortName,
		0x20,
		fileCluster,
		3,
	)

	checksum := shortNameChecksum(shortEntry[:])

	lfnEntries, err := encodeLFNEntries(name, checksum)
	if err != nil {
		t.Fatal(err)
	}

	if len(lfnEntries) < 2 {
		t.Fatalf("got %d LFN entries, want at least 2", len(lfnEntries))
	}

	directoryOffset := int(dataOffset)

	for i, entry := range lfnEntries {
		offset := directoryOffset + i*32
		copy(data[offset:offset+32], entry[:])
	}

	shortOffset := directoryOffset + len(lfnEntries)*32

	copy(
		data[shortOffset:shortOffset+32],
		shortEntry[:],
	)

	data[shortOffset+32] = 0x00

	oldDataOffset := directoryOffset +
		(int(fileCluster)-2)*int(layout.BytesPerCluster)

	copy(
		data[oldDataOffset:oldDataOffset+3],
		[]byte("old"),
	)

	buffer := writerAtBuffer{data: data}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	beforeLFN := append(
		[]byte(nil),
		data[directoryOffset:shortOffset]...,
	)

	if err := fs.ReplaceFile(
		name,
		[]byte("new contents"),
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
		t.Fatal("replacement file was not found")
	}

	if record.Entry.Name != name {
		t.Fatalf(
			"file name = %q, want %q",
			record.Entry.Name,
			name,
		)
	}

	if record.Entry.ShortName != "A~1.TXT" {
		t.Fatalf(
			"short name = %q, want %q",
			record.Entry.ShortName,
			"A~1.TXT",
		)
	}

	if record.Entry.Size != uint32(len("new contents")) {
		t.Fatalf(
			"file size = %d, want %d",
			record.Entry.Size,
			len("new contents"),
		)
	}

	if record.Entry.StartCluster == fileCluster {
		t.Fatalf(
			"replacement reused old cluster %d",
			fileCluster,
		)
	}

	afterLFN := data[directoryOffset:shortOffset]

	if !bytes.Equal(afterLFN, beforeLFN) {
		t.Fatal("LFN entries were modified during replacement")
	}

	file := &File{
		entry: record.Entry,
		fs:    fs,
	}

	got, err := file.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("new contents")) {
		t.Fatalf(
			"replacement contents = %q, want %q",
			got,
			"new contents",
		)
	}

	free, err := fs.IsFreeCluster(fileCluster)
	if err != nil {
		t.Fatal(err)
	}

	if !free {
		t.Fatalf(
			"old cluster %d was not freed",
			fileCluster,
		)
	}
}

// Verify replacing a long filename does not accidentally recreate
// or alter directory entries
func TestReplaceFileDoesNotDuplicateLongFilename(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 30

		rootCluster = uint32(2)
		fileCluster = uint32(3)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(fileCluster)*4:],
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

	name := "A reasonably long filename.txt"

	shortName, err := encodeShortName("A~1.TXT")
	if err != nil {
		t.Fatal(err)
	}

	checksum := shortNameChecksum(shortName[:])

	lfnEntries, err := encodeLFNEntries(name, checksum)
	if err != nil {
		t.Fatal(err)
	}

	shortEntry := encodeDirectoryEntry(
		shortName,
		0x20,
		fileCluster,
		3,
	)

	directoryOffset := int(dataOffset)

	for i, entry := range lfnEntries {
		start := directoryOffset + i*32
		end := start + 32

		copy(data[start:end], entry[:])
	}

	shortOffset := directoryOffset + len(lfnEntries)*32

	copy(
		data[shortOffset:shortOffset+32],
		shortEntry[:],
	)

	data[shortOffset+32] = 0x00

	fileDataOffset := directoryOffset +
		(int(fileCluster)-2)*int(layout.BytesPerCluster)

	copy(
		data[fileDataOffset:fileDataOffset+3],
		[]byte("old"),
	)

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

	records, err := fs.readDirectoryRecords(rootCluster)
	if err != nil {
		t.Fatal(err)
	}

	if len(records) != 1 {
		t.Fatalf(
			"initial directory record count = %d, want 1",
			len(records),
		)
	}

	if records[0].Entry.Name != name {
		t.Fatalf(
			"initial filename = %q, want %q",
			records[0].Entry.Name,
			name,
		)
	}

	if err := fs.ReplaceFile(
		name,
		[]byte("new"),
	); err != nil {
		t.Fatal(err)
	}

	records, err = fs.readDirectoryRecords(rootCluster)
	if err != nil {
		t.Fatal(err)
	}

	if len(records) != 1 {
		t.Fatalf(
			"directory record count after replacement = %d, want 1",
			len(records),
		)
	}

	if records[0].Entry.Name != name {
		t.Fatalf(
			"filename after replacement = %q, want %q",
			records[0].Entry.Name,
			name,
		)
	}

	if records[0].Entry.ShortName != "A~1.TXT" {
		t.Fatalf(
			"short filename after replacement = %q, want %q",
			records[0].Entry.ShortName,
			"A~1.TXT",
		)
	}

	file := &File{
		entry: records[0].Entry,
		fs:    fs,
	}

	got, err := file.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("new")) {
		t.Fatalf(
			"replacement contents = %q, want %q",
			got,
			"new",
		)
	}
}

func TestReplaceFileMultiCluster(t *testing.T) {
	// Verify that a replacement requiring multiple clusters gets a complete chain,
	// and that the old chain is entirely freed.

	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 30

		rootCluster = uint32(2)
		oldCluster1 = uint32(3)
		oldCluster2 = uint32(4)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	// Root directory cluster.
	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	// Existing file chain: 3 -> 4 -> EOF.
	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(oldCluster1)*4:],
		oldCluster2,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(oldCluster2)*4:],
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

	shortName, err := encodeShortName("TEST.BIN")
	if err != nil {
		t.Fatal(err)
	}

	oldContents := bytes.Repeat(
		[]byte("o"),
		int(sectorSize)*2,
	)

	shortEntry := encodeDirectoryEntry(
		shortName,
		0x20,
		oldCluster1,
		uint32(len(oldContents)),
	)

	directoryOffset := int(dataOffset)

	copy(
		data[directoryOffset:directoryOffset+32],
		shortEntry[:],
	)

	data[directoryOffset+32] = 0x00

	oldDataOffset1 := directoryOffset +
		(int(oldCluster1)-2)*int(layout.BytesPerCluster)

	oldDataOffset2 := directoryOffset +
		(int(oldCluster2)-2)*int(layout.BytesPerCluster)

	copy(
		data[oldDataOffset1:oldDataOffset1+int(sectorSize)],
		oldContents[:sectorSize],
	)

	copy(
		data[oldDataOffset2:oldDataOffset2+int(sectorSize)],
		oldContents[sectorSize:],
	)

	buffer := writerAtBuffer{data: data}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	newContents := bytes.Repeat(
		[]byte("n"),
		int(sectorSize)*3+1,
	)

	if err := fs.ReplaceFile("TEST.BIN", newContents); err != nil {
		t.Fatal(err)
	}

	record, err := fs.findDirectoryEntry(
		rootCluster,
		"TEST.BIN",
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("replacement file was not found")
	}

	if record.Entry.Size != uint32(len(newContents)) {
		t.Fatalf(
			"file size = %d, want %d",
			record.Entry.Size,
			len(newContents),
		)
	}

	if record.Entry.StartCluster == oldCluster1 ||
		record.Entry.StartCluster == oldCluster2 {
		t.Fatalf(
			"replacement reused old cluster chain, start=%d",
			record.Entry.StartCluster,
		)
	}

	newChain, err := fs.ClusterChain(record.Entry.StartCluster)
	if err != nil {
		t.Fatal(err)
	}

	if len(newChain) != 4 {
		t.Fatalf(
			"replacement chain length = %d, want 4",
			len(newChain),
		)
	}

	file := &File{
		entry: record.Entry,
		fs:    fs,
	}

	got, err := file.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, newContents) {
		t.Fatalf("replacement contents do not match")
	}

	free, err := fs.IsFreeCluster(oldCluster1)
	if err != nil {
		t.Fatal(err)
	}

	if !free {
		t.Fatalf(
			"old cluster %d was not freed",
			oldCluster1,
		)
	}

	free, err = fs.IsFreeCluster(oldCluster2)
	if err != nil {
		t.Fatal(err)
	}

	if !free {
		t.Fatalf(
			"old cluster %d was not freed",
			oldCluster2,
		)
	}
}

// Test replacement of a file whose old chain is fragmented
func TestReplaceFileFreesFragmentedOldChain(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 40

		rootCluster = uint32(2)
		oldCluster1 = uint32(3)
		oldCluster2 = uint32(7)
		oldCluster3 = uint32(9)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	// Fragmented existing file chain:
	// 3 -> 7 -> 9 -> EOF.
	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(oldCluster1)*4:],
		oldCluster2,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(oldCluster2)*4:],
		oldCluster3,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(oldCluster3)*4:],
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

	shortName, err := encodeShortName("TEST.TXT")
	if err != nil {
		t.Fatal(err)
	}

	oldContents := bytes.Repeat(
		[]byte("o"),
		int(sectorSize)*3,
	)

	shortEntry := encodeDirectoryEntry(
		shortName,
		0x20,
		oldCluster1,
		uint32(len(oldContents)),
	)

	directoryOffset := int(dataOffset)

	copy(
		data[directoryOffset:directoryOffset+32],
		shortEntry[:],
	)

	data[directoryOffset+32] = 0x00

	oldDataOffset1 := directoryOffset +
		(int(oldCluster1)-2)*int(layout.BytesPerCluster)

	oldDataOffset2 := directoryOffset +
		(int(oldCluster2)-2)*int(layout.BytesPerCluster)

	oldDataOffset3 := directoryOffset +
		(int(oldCluster3)-2)*int(layout.BytesPerCluster)

	copy(
		data[oldDataOffset1:oldDataOffset1+int(sectorSize)],
		oldContents[:sectorSize],
	)

	copy(
		data[oldDataOffset2:oldDataOffset2+int(sectorSize)],
		oldContents[sectorSize:2*int(sectorSize)],
	)

	copy(
		data[oldDataOffset3:oldDataOffset3+int(sectorSize)],
		oldContents[2*int(sectorSize):],
	)

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

	newContents := bytes.Repeat(
		[]byte("n"),
		int(sectorSize)*2,
	)

	if err := fs.ReplaceFile(
		"TEST.TXT",
		newContents,
	); err != nil {
		t.Fatal(err)
	}

	record, err := fs.findDirectoryEntry(
		rootCluster,
		"TEST.TXT",
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("replacement file was removed")
	}

	if record.Entry.Size != uint32(len(newContents)) {
		t.Fatalf(
			"file size = %d, want %d",
			record.Entry.Size,
			len(newContents),
		)
	}

	if record.Entry.StartCluster == oldCluster1 ||
		record.Entry.StartCluster == oldCluster2 ||
		record.Entry.StartCluster == oldCluster3 {
		t.Fatalf(
			"replacement file uses old cluster: %d",
			record.Entry.StartCluster,
		)
	}

	for _, cluster := range []uint32{
		oldCluster1,
		oldCluster2,
		oldCluster3,
	} {
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

	file := &File{
		entry: record.Entry,
		fs:    fs,
	}

	got, err := file.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, newContents) {
		t.Fatalf(
			"replacement contents = %q, want %q",
			got,
			newContents,
		)
	}
}

// Verify FAT high 4 bits are preserved during replacement.
// Since ReplaceFile performs multiple FAT updates, we should verify
// that freeing the old chain doesn't accidentally corrupt the reserved high nibble.
func TestReplaceFilePreservesFATHighFourBits(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 30

		rootCluster = uint32(2)
		oldCluster  = uint32(3)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	// Preserve a non-zero reserved high nibble on the old entry.
	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(oldCluster)*4:],
		0xA0000000|fat32EOF,
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

	shortName, err := encodeShortName("TEST.TXT")
	if err != nil {
		t.Fatal(err)
	}

	shortEntry := encodeDirectoryEntry(
		shortName,
		0x20,
		oldCluster,
		3,
	)

	directoryOffset := int(dataOffset)

	copy(
		data[directoryOffset:directoryOffset+32],
		shortEntry[:],
	)

	data[directoryOffset+32] = 0x00

	oldDataOffset := directoryOffset +
		(int(oldCluster)-2)*int(layout.BytesPerCluster)

	copy(
		data[oldDataOffset:oldDataOffset+3],
		[]byte("old"),
	)

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

	if err := fs.ReplaceFile(
		"TEST.TXT",
		[]byte("new"),
	); err != nil {
		t.Fatal(err)
	}

	raw := binary.LittleEndian.Uint32(
		buffer.data[int(fatOffset)+int(oldCluster)*4:],
	)

	if raw&0xF0000000 != 0xA0000000 {
		t.Fatalf(
			"old FAT entry high bits = 0x%08x, want 0xA0000000",
			raw&0xF0000000,
		)
	}

	if raw&0x0FFFFFFF != 0 {
		t.Fatalf(
			"old FAT entry low 28 bits = 0x%08x, want 0",
			raw&0x0FFFFFFF,
		)
	}
}

// Test data-write failure during replacement
func TestReplaceFilePreservesOldFileOnDataWriteFailure(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 30

		rootCluster = uint32(2)
		fileCluster = uint32(3)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(fileCluster)*4:],
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

	shortName, err := encodeShortName("TEST.TXT")
	if err != nil {
		t.Fatal(err)
	}

	oldContents := []byte("old")

	shortEntry := encodeDirectoryEntry(
		shortName,
		0x20,
		fileCluster,
		uint32(len(oldContents)),
	)

	directoryOffset := int(dataOffset)

	copy(
		data[directoryOffset:directoryOffset+32],
		shortEntry[:],
	)

	data[directoryOffset+32] = 0x00

	oldDataOffset := directoryOffset +
		(int(fileCluster)-2)*int(layout.BytesPerCluster)

	copy(
		data[oldDataOffset:oldDataOffset+len(oldContents)],
		oldContents,
	)

	// The first data write occurs after FAT allocation writes.
	// Fail on that data-cluster write.
	failOffset := dataOffset +
		(int64(4)-2)*layout.BytesPerCluster

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: failOffset,
		failCount:  1,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	err = fs.ReplaceFile(
		"TEST.TXT",
		[]byte("new contents"),
	)
	if err == nil {
		t.Fatal("ReplaceFile succeeded, want data write failure")
	}

	record, err := fs.findDirectoryEntry(
		rootCluster,
		"TEST.TXT",
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("original file was removed after replacement failure")
	}

	if record.Entry.StartCluster != fileCluster {
		t.Fatalf(
			"start cluster = %d, want original cluster %d",
			record.Entry.StartCluster,
			fileCluster,
		)
	}

	if record.Entry.Size != uint32(len(oldContents)) {
		t.Fatalf(
			"file size = %d, want %d",
			record.Entry.Size,
			len(oldContents),
		)
	}

	file := &File{
		entry: record.Entry,
		fs:    fs,
	}

	got, err := file.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, oldContents) {
		t.Fatalf(
			"original contents = %q, want %q",
			got,
			oldContents,
		)
	}

	free, err := fs.IsFreeCluster(4)
	if err != nil {
		t.Fatal(err)
	}

	if !free {
		t.Fatal("newly allocated replacement cluster 4 was not freed")
	}

	free, err = fs.IsFreeCluster(fileCluster)
	if err != nil {
		t.Fatal(err)
	}

	if free {
		t.Fatalf(
			"original file cluster %d was freed",
			fileCluster,
		)
	}
}

// Test failure while freeing the old chain
func TestReplaceFileCommitsNewFileWhenOldChainFreeFails(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 30

		rootCluster = uint32(2)
		oldCluster1 = uint32(3)
		oldCluster2 = uint32(4)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	// Existing file chain: 3 -> 4 -> EOF.
	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(oldCluster1)*4:],
		oldCluster2,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(oldCluster2)*4:],
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

	shortName, err := encodeShortName("TEST.TXT")
	if err != nil {
		t.Fatal(err)
	}

	oldContents := bytes.Repeat(
		[]byte("o"),
		int(sectorSize)*2,
	)

	shortEntry := encodeDirectoryEntry(
		shortName,
		0x20,
		oldCluster1,
		uint32(len(oldContents)),
	)

	directoryOffset := int(dataOffset)

	copy(
		data[directoryOffset:directoryOffset+32],
		shortEntry[:],
	)

	data[directoryOffset+32] = 0x00

	oldDataOffset1 := directoryOffset +
		(int(oldCluster1)-2)*int(layout.BytesPerCluster)

	oldDataOffset2 := directoryOffset +
		(int(oldCluster2)-2)*int(layout.BytesPerCluster)

	copy(
		data[oldDataOffset1:oldDataOffset1+int(sectorSize)],
		oldContents[:sectorSize],
	)

	copy(
		data[oldDataOffset2:oldDataOffset2+int(sectorSize)],
		oldContents[sectorSize:],
	)

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: fatOffset,
		// Write 1 allocates the new chain, write 2 frees the old one.
		failCount: 2,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	newContents := []byte("new contents")

	err = fs.ReplaceFile(
		"TEST.TXT",
		newContents,
	)
	if err == nil {
		t.Fatal("ReplaceFile succeeded, want old-chain free failure")
	}

	record, err := fs.findDirectoryEntry(
		rootCluster,
		"TEST.TXT",
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("replacement file was removed")
	}

	if record.Entry.StartCluster == oldCluster1 ||
		record.Entry.StartCluster == oldCluster2 {
		t.Fatalf(
			"directory entry still points to old chain: %d",
			record.Entry.StartCluster,
		)
	}

	if record.Entry.Size != uint32(len(newContents)) {
		t.Fatalf(
			"file size = %d, want %d",
			record.Entry.Size,
			len(newContents),
		)
	}

	file := &File{
		entry: record.Entry,
		fs:    fs,
	}

	got, err := file.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, newContents) {
		t.Fatalf(
			"replacement contents = %q, want %q",
			got,
			newContents,
		)
	}
}

// Test ReplaceFile inside a new directory
// Verify that ReplaceFile correctly resolves nested paths
// when the directory contains . and ...
func TestReplaceFileInNewDirectory(t *testing.T) {
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

	initial := []byte("initial content")

	if err := fs.WriteFile(
		"PARENT/CHILD/TEST.TXT",
		initial,
	); err != nil {
		t.Fatal(err)
	}

	replacement := []byte(
		"replacement content that is longer",
	)

	if err := fs.ReplaceFile(
		"PARENT/CHILD/TEST.TXT",
		replacement,
	); err != nil {
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

	file, err := fs.findDirectoryEntry(
		child.Entry.StartCluster,
		"TEST.TXT",
	)
	if err != nil {
		t.Fatal(err)
	}

	if file == nil {
		t.Fatal("file was not found after replacement")
	}

	content, err := fs.readFile(file.Entry)
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

	if file.Entry.Size != uint32(len(replacement)) {
		t.Fatalf(
			"file size = %d, want %d",
			file.Entry.Size,
			len(replacement),
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

// Verify ReplaceFile is case-insensitive
func TestReplaceFileIsCaseInsensitive(t *testing.T) {
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
		[]byte("before"),
	); err != nil {
		t.Fatal(err)
	}

	if err := fs.ReplaceFile(
		"example.txt",
		[]byte("after"),
	); err != nil {
		t.Fatalf(
			"ReplaceFile with different case: %v",
			err,
		)
	}

	content, err := fs.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(content, []byte("after")) {
		t.Fatalf(
			"file contents = %q, want %q",
			content,
			"after",
		)
	}
}

// Test ReplaceFile with ..
func TestReplaceFileParentDirectoryPathComponent(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 160

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

	if err := fs.Mkdir("Music"); err != nil {
		t.Fatal(err)
	}

	if err := fs.Mkdir("Music/Sub"); err != nil {
		t.Fatal(err)
	}

	if err := fs.WriteFile(
		"Music/Example.TXT",
		[]byte("before"),
	); err != nil {
		t.Fatal(err)
	}

	if err := fs.ReplaceFile(
		"Music/Sub/../Example.TXT",
		[]byte("after"),
	); err != nil {
		t.Fatalf(
			"ReplaceFile with parent directory component: %v",
			err,
		)
	}

	content, err := fs.ReadFile("Music/Example.TXT")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(content, []byte("after")) {
		t.Fatalf(
			"file contents = %q, want %q",
			content,
			"after",
		)
	}
}

// Multi-level .. for ReplaceFile
func TestReplaceFileMultipleParentDirectoryPathComponents(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 240

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

	if err := fs.Mkdir("A"); err != nil {
		t.Fatal(err)
	}

	if err := fs.Mkdir("A/B"); err != nil {
		t.Fatal(err)
	}

	if err := fs.Mkdir("A/B/C"); err != nil {
		t.Fatal(err)
	}

	if err := fs.WriteFile(
		"Example.TXT",
		[]byte("before"),
	); err != nil {
		t.Fatal(err)
	}

	if err := fs.ReplaceFile(
		"A/B/C/../../../../Example.TXT",
		[]byte("after"),
	); err != nil {
		t.Fatalf(
			"ReplaceFile with multiple parent directory components: %v",
			err,
		)
	}

	content, err := fs.ReadFile("Example.TXT")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(content, []byte("after")) {
		t.Fatalf(
			"file contents = %q, want %q",
			content,
			"after",
		)
	}
}

func TestReplaceFileEmptyPathComponentIsRejected(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 160

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

	if err := fs.Mkdir("Dir"); err != nil {
		t.Fatal(err)
	}

	if err := fs.WriteFile(
		"Dir/Example.TXT",
		[]byte("before"),
	); err != nil {
		t.Fatal(err)
	}

	if err := fs.ReplaceFile(
		"Dir//Example.TXT",
		[]byte("after"),
	); err == nil {
		t.Fatal("ReplaceFile accepted an empty path component")
	}

	content, err := fs.ReadFile("Dir/Example.TXT")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(content, []byte("before")) {
		t.Fatalf(
			"file contents = %q, want %q",
			content,
			"before",
		)
	}
}

package fat32

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
)

// Case-insensitive lookup
func TestReadFileIsCaseInsensitive(t *testing.T) {
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

	content, err := fs.ReadFile("example.txt")
	if err != nil {
		t.Fatalf(
			"ReadFile with different case: %v",
			err,
		)
	}

	if !bytes.Equal(content, []byte("hello")) {
		t.Fatalf(
			"file contents = %q, want %q",
			content,
			"hello",
		)
	}
}

// Case-insensitive nested path lookup
func TestReadFileNestedPathIsCaseInsensitive(t *testing.T) {
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

	if err := fs.WriteFile(
		"Music/Example.TXT",
		[]byte("hello"),
	); err != nil {
		t.Fatal(err)
	}

	content, err := fs.ReadFile("MUSIC/example.txt")
	if err != nil {
		t.Fatalf(
			"ReadFile with different case in nested path: %v",
			err,
		)
	}

	if !bytes.Equal(content, []byte("hello")) {
		t.Fatalf(
			"file contents = %q, want %q",
			content,
			"hello",
		)
	}
}

// Test .. in ReadFile
func TestReadFileParentDirectoryPathComponent(t *testing.T) {
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
		[]byte("hello"),
	); err != nil {
		t.Fatal(err)
	}

	content, err := fs.ReadFile(
		"Music/Sub/../Example.TXT",
	)
	if err != nil {
		t.Fatalf(
			"ReadFile with parent directory component: %v",
			err,
		)
	}

	if !bytes.Equal(content, []byte("hello")) {
		t.Fatalf(
			"file contents = %q, want %q",
			content,
			"hello",
		)
	}
}

// ReadFile traversal above root
func TestReadFileParentTraversalAboveRootStaysAtRoot(t *testing.T) {
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

	if err := fs.WriteFile(
		"Example.TXT",
		[]byte("hello"),
	); err != nil {
		t.Fatal(err)
	}

	content, err := fs.ReadFile(
		"../../Example.TXT",
	)
	if err != nil {
		t.Fatalf(
			"ReadFile with traversal above root: %v",
			err,
		)
	}

	if !bytes.Equal(content, []byte("hello")) {
		t.Fatalf(
			"file contents = %q, want %q",
			content,
			"hello",
		)
	}
}

// Malformed empty path components
func TestReadFileEmptyPathComponentIsRejected(t *testing.T) {
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
		[]byte("hello"),
	); err != nil {
		t.Fatal(err)
	}

	if _, err := fs.ReadFile(
		"Dir//Example.TXT",
	); err == nil {
		t.Fatal("ReadFile accepted an empty path component")
	}
}

func TestReadFileTrailingSeparatorIsRejected(t *testing.T) {
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
		[]byte("hello"),
	); err != nil {
		t.Fatal(err)
	}

	if _, err := fs.ReadFile(
		"Dir/Example.TXT/",
	); err == nil {
		t.Fatal("ReadFile accepted a trailing separator")
	}
}

func TestReadFileMultipleLeadingSeparatorsAreRejected(t *testing.T) {
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

	if err := fs.WriteFile(
		"Example.TXT",
		[]byte("hello"),
	); err != nil {
		t.Fatal(err)
	}

	if _, err := fs.ReadFile(
		"//Example.TXT",
	); err == nil {
		t.Fatal("ReadFile accepted multiple leading separators")
	}
}

func TestReadFileDirectoryIsRejected(t *testing.T) {
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

	if _, err := fs.ReadFile("Dir"); err == nil {
		t.Fatal("ReadFile accepted a directory")
	}
}

func TestReadFileNonexistentDoesNotMutate(t *testing.T) {
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

	before := append([]byte(nil), buffer.data...)

	if _, err := fs.ReadFile("does-not-exist.txt"); err == nil {
		t.Fatal("ReadFile succeeded for nonexistent file")
	}

	if !bytes.Equal(buffer.data, before) {
		t.Fatal("ReadFile modified the filesystem")
	}
}

func TestReadFileParentFileIsRejected(t *testing.T) {
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

	if err := fs.WriteFile("file.txt", []byte("hello")); err != nil {
		t.Fatal(err)
	}

	before := append([]byte(nil), buffer.data...)

	if _, err := fs.ReadFile("file.txt/child.txt"); err == nil {
		t.Fatal("ReadFile accepted a file as a parent directory")
	}

	if !bytes.Equal(buffer.data, before) {
		t.Fatal("ReadFile modified the filesystem")
	}
}

func TestReadFileRootDirectoryIsRejected(t *testing.T) {
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

	for _, path := range []string{".", "/"} {
		before := append([]byte(nil), buffer.data...)

		if _, err := fs.ReadFile(path); err == nil {
			t.Fatalf("ReadFile accepted root directory path %q", path)
		}

		if !bytes.Equal(buffer.data, before) {
			t.Fatalf(
				"ReadFile modified the filesystem for path %q",
				path,
			)
		}
	}
}

// Read a multi-cluster file with non-cluster-aligned size
func TestReadFileTrimsFinalClusterToFileSize(t *testing.T) {
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

	content := bytes.Repeat([]byte("x"), int(sectorSize)+17)

	if err := fs.WriteFile("test.txt", content); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile("test.txt")
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != len(content) {
		t.Fatalf(
			"ReadFile returned %d bytes, want %d",
			len(got),
			len(content),
		)
	}

	if !bytes.Equal(got, content) {
		t.Fatal("ReadFile returned incorrect data")
	}
}

func TestReadEmptyFile(t *testing.T) {
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

	if err := fs.WriteFile("empty.txt", nil); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile("empty.txt")
	if err != nil {
		t.Fatal(err)
	}

	if got == nil {
		t.Fatal("ReadFile returned nil for an empty file")
	}

	if len(got) != 0 {
		t.Fatalf(
			"ReadFile returned %d bytes, want 0",
			len(got),
		)
	}
}

// Corrupted FAT chain
func TestReadFileRejectsBadCluster(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 160

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
		fat32BadCluster,
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

	// Root directory contains a short entry for bad.txt.
	shortName, err := encodeShortName("bad.txt")
	if err != nil {
		t.Fatal(err)
	}

	entry := encodeDirectoryEntry(
		shortName,
		0x20,
		fileCluster,
		1,
	)

	copy(
		data[int(dataOffset):int(dataOffset)+32],
		entry[:],
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

	if _, err := fs.ReadFile("bad.txt"); err == nil {
		t.Fatal("ReadFile accepted a bad-cluster FAT entry")
	}
}

// Cyclic FAT chain
func TestReadFileRejectsCyclicClusterChain(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 160

		rootCluster   = uint32(2)
		firstCluster  = uint32(3)
		secondCluster = uint32(4)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(firstCluster)*4:],
		secondCluster,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(secondCluster)*4:],
		firstCluster,
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

	shortName, err := encodeShortName("cycle.txt")
	if err != nil {
		t.Fatal(err)
	}

	entry := encodeDirectoryEntry(
		shortName,
		0x20,
		firstCluster,
		uint32(sectorSize)+1,
	)

	copy(
		data[int(dataOffset):int(dataOffset)+32],
		entry[:],
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

	if _, err := fs.ReadFile("cycle.txt"); err == nil {
		t.Fatal("ReadFile accepted a cyclic cluster chain")
	}
}

// FAT chain ends before the declared file size
func TestReadFileRejectsTruncatedClusterChain(t *testing.T) {
	// Scenario: the directory entry declares 513 bytes,
	// while its chain contains only one 512-byte cluster
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 160

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

	shortName, err := encodeShortName("trunc.txt")
	if err != nil {
		t.Fatal(err)
	}

	entry := encodeDirectoryEntry(
		shortName,
		0x20,
		fileCluster,
		uint32(sectorSize)+1,
	)

	copy(
		data[int(dataOffset):int(dataOffset)+32],
		entry[:],
	)

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

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

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	if _, err := fs.ReadFile("trunc.txt"); err == nil {
		t.Fatal("ReadFile accepted a truncated cluster chain")
	}
}

// File size exactly matches the cluster chain
func TestReadFileExactClusterSize(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 160

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

	content := bytes.Repeat([]byte("x"), int(sectorSize))

	copy(
		data[int(dataOffset)+int(fileCluster-rootCluster)*int(sectorSize):],
		content,
	)

	shortName, err := encodeShortName("exact.txt")
	if err != nil {
		t.Fatal(err)
	}

	entry := encodeDirectoryEntry(
		shortName,
		0x20,
		fileCluster,
		uint32(len(content)),
	)

	copy(
		data[int(dataOffset):int(dataOffset)+32],
		entry[:],
	)

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

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

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	got, err := fs.ReadFile("exact.txt")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, content) {
		t.Fatal("ReadFile returned incorrect data")
	}
}

// Reading a file through multiple parent directories
func TestReadFileMultipleParentDirectories(t *testing.T) {
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

	if err := fs.Mkdir("one"); err != nil {
		t.Fatal(err)
	}

	if err := fs.Mkdir("one/two"); err != nil {
		t.Fatal(err)
	}

	content := []byte("nested read")

	if err := fs.WriteFile("one/two/test.txt", content); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile("one/two/test.txt")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, content) {
		t.Fatal("ReadFile returned incorrect nested file data")
	}
}

// Invalid starting cluster
func TestReadFileRejectsInvalidStartCluster(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 160

		rootCluster    = uint32(2)
		invalidCluster = uint32(1)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	shortName, err := encodeShortName("invalid.txt")
	if err != nil {
		t.Fatal(err)
	}

	entry := encodeDirectoryEntry(
		shortName,
		0x20,
		invalidCluster,
		1,
	)

	copy(
		data[int(dataOffset):int(dataOffset)+32],
		entry[:],
	)

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

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

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	if _, err := fs.ReadFile("invalid.txt"); err == nil {
		t.Fatal("ReadFile accepted an invalid starting cluster")
	}
}

// FAT chain points beyond the valid cluster range
func TestReadFileRejectsOutOfRangeCluster(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 160

		rootCluster = uint32(2)
		fileCluster = uint32(3)
		invalidNext = uint32(200)
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
		invalidNext,
	)

	shortName, err := encodeShortName("range.txt")
	if err != nil {
		t.Fatal(err)
	}

	entry := encodeDirectoryEntry(
		shortName,
		0x20,
		fileCluster,
		uint32(sectorSize)+1,
	)

	copy(
		data[int(dataOffset):int(dataOffset)+32],
		entry[:],
	)

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

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

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	if _, err := fs.ReadFile("range.txt"); err == nil {
		t.Fatal("ReadFile accepted an out-of-range cluster")
	}
}

// FAT chain terminates with a free-cluster value.
// A chain containing 0 in the middle is corrupt.
// readClusterChain should reject it rather than treating it as end-of-chain.
func TestReadFileRejectsFreeClusterInChain(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 160

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

	// Cluster 3 is allocated, but its FAT entry is zero,
	// meaning the chain points to a free cluster.
	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(fileCluster)*4:],
		0,
	)

	shortName, err := encodeShortName("free.txt")
	if err != nil {
		t.Fatal(err)
	}

	entry := encodeDirectoryEntry(
		shortName,
		0x20,
		fileCluster,
		1,
	)

	copy(
		data[int(dataOffset):int(dataOffset)+32],
		entry[:],
	)

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

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

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	if _, err := fs.ReadFile("free.txt"); err == nil {
		t.Fatal("ReadFile accepted a free cluster in the file chain")
	}
}

// Verify case-insensitive lookup with a long filename
func TestReadFileLongFilenameIsCaseInsensitive(t *testing.T) {
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

	const filename = "A Rather Long Filename.txt"
	content := []byte("long filename")

	if err := fs.WriteFile(filename, content); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile("a rather long filename.txt")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, content) {
		t.Fatal("ReadFile returned incorrect data")
	}
}

// Absolute path lookup
func TestReadFileAbsolutePath(t *testing.T) {
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

	content := []byte("absolute path")

	if err := fs.WriteFile("test.txt", content); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile("/test.txt")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, content) {
		t.Fatal("ReadFile returned incorrect data")
	}
}

// Verify that ReadFile can resolve a path that enters a directory
// and then walks back to its parent
func TestReadFileParentTraversal(t *testing.T) {
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

	content := []byte("parent traversal")

	if err := fs.WriteFile("test.txt", content); err != nil {
		t.Fatal(err)
	}

	if err := fs.Mkdir("dir"); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile("dir/../test.txt")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, content) {
		t.Fatal("ReadFile returned incorrect data")
	}
}

// Read from a directory whose cluster chain was expanded.
// Exercise directory traversal after the directory has crossed
// its original cluster boundary.
func TestReadFileFromExpandedDirectory(t *testing.T) {
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

	if err := fs.Mkdir("dir"); err != nil {
		t.Fatal(err)
	}

	// With one sector per cluster there are 16 directory entries
	// per cluster. The first two are "." and "..".
	for i := 0; i < 14; i++ {
		name := fmt.Sprintf("file%d.txt", i)

		if err := fs.WriteFile(
			"dir/"+name,
			[]byte(name),
		); err != nil {
			t.Fatal(err)
		}
	}

	content := []byte("expanded directory file")

	if err := fs.WriteFile(
		"dir/target.txt",
		content,
	); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile("dir/target.txt")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, content) {
		t.Fatal("ReadFile returned incorrect data")
	}
}

// Read a long filename spanning a directory-cluster boundary.
// Combines LFN resolution and directory-chain traversal.
func TestReadFileLongFilenameAcrossDirectoryClusterBoundary(t *testing.T) {
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

	if err := fs.Mkdir("dir"); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 14; i++ {
		name := fmt.Sprintf("file%d.txt", i)

		if err := fs.WriteFile(
			"dir/"+name,
			[]byte(name),
		); err != nil {
			t.Fatal(err)
		}
	}

	const filename = "This is a long filename.txt"
	content := []byte("long filename across boundary")

	if err := fs.WriteFile(
		"dir/"+filename,
		content,
	); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile(
		"dir/" + filename,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, content) {
		t.Fatal("ReadFile returned incorrect data")
	}
}

// Unicode LFN round-trip on read.
// Verify that ReadFile resolves a Unicode filename containing
// a supplementary-plane character after it has been written.
func TestReadFileUnicodeLongFilename(t *testing.T) {
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

	const filename = "Music 🎵 Collection.txt"
	content := []byte("unicode filename")

	if err := fs.WriteFile(filename, content); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, content) {
		t.Fatal("ReadFile returned incorrect data")
	}
}

// Verify ReadFile doesn't modify the filesystem
func TestReadFileDoesNotMutateFilesystem(t *testing.T) {
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

	content := []byte("read-only")

	if err := fs.WriteFile("test.txt", content); err != nil {
		t.Fatal(err)
	}

	before := append([]byte(nil), buffer.data...)

	got, err := fs.ReadFile("test.txt")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, content) {
		t.Fatal("ReadFile returned incorrect data")
	}

	if !bytes.Equal(buffer.data, before) {
		t.Fatal("ReadFile modified the filesystem")
	}
}

// Non-empty file with no starting cluster.
// The directory entry says the file has data, but its start cluster is 0.
func TestReadFileRejectsNonEmptyFileWithoutStartCluster(t *testing.T) {
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

	shortName, err := encodeShortName("orphan.txt")
	if err != nil {
		t.Fatal(err)
	}

	// Size is non-zero, but StartCluster is zero.
	entry := encodeDirectoryEntry(
		shortName,
		0x20,
		0,
		1,
	)

	copy(
		data[int(dataOffset):int(dataOffset)+32],
		entry[:],
	)

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

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

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	if _, err := fs.ReadFile("orphan.txt"); err == nil {
		t.Fatal("ReadFile accepted a non-empty file without a start cluster")
	}
}

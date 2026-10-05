package fat32

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
	"unicode/utf16"
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

// File.ReadAll() returns the same data as ReadFile
func TestFileReadAll(t *testing.T) {
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

	content := []byte("File.ReadAll test")

	if err := fs.WriteFile("test.txt", content); err != nil {
		t.Fatal(err)
	}

	file, err := fs.OpenFile("test.txt")
	if err != nil {
		t.Fatal(err)
	}

	got, err := file.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, content) {
		t.Fatal("File.ReadAll returned incorrect data")
	}
}

// OpenFile exposes the correct filename and size, including an LFN
func TestOpenFileMetadata(t *testing.T) {
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
	content := []byte("metadata")

	if err := fs.WriteFile(filename, content); err != nil {
		t.Fatal(err)
	}

	file, err := fs.OpenFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	if got := file.Name(); got != filename {
		t.Fatalf(
			"File.Name() = %q, want %q",
			got,
			filename,
		)
	}

	if got := file.Size(); got != uint32(len(content)) {
		t.Fatalf(
			"File.Size() = %d, want %d",
			got,
			len(content),
		)
	}
}

// OpenFile should reject directories
// Test it returns a *File only for regular files, not directories.
func TestOpenFileDirectoryIsRejected(t *testing.T) {
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

	if err := fs.Mkdir("testdir"); err != nil {
		t.Fatal(err)
	}

	if _, err := fs.OpenFile("testdir"); err == nil {
		t.Fatal("OpenFile should reject a directory")
	}
}

func TestOpenFileNonexistentDoesNotMutate(t *testing.T) {
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

	before := append([]byte(nil), data...)

	if _, err := fs.OpenFile("missing.txt"); err == nil {
		t.Fatal("OpenFile should reject a nonexistent file")
	}

	if !bytes.Equal(data, before) {
		t.Fatal("OpenFile mutated the filesystem")
	}
}

func TestOpenFileIsCaseInsensitive(t *testing.T) {
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

	const filename = "Test File With Long Name.txt"
	content := []byte("case insensitive OpenFile")

	if err := fs.WriteFile(filename, content); err != nil {
		t.Fatal(err)
	}

	file, err := fs.OpenFile("test file with long name.TXT")
	if err != nil {
		t.Fatal(err)
	}

	got, err := file.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, content) {
		t.Fatalf(
			"OpenFile returned incorrect content: %q",
			got,
		)
	}

	if file.Name() != filename {
		t.Fatalf(
			"File.Name() = %q, want %q",
			file.Name(),
			filename,
		)
	}
}

// Verify OpenFile with . and .. path components
func TestOpenFileParentTraversal(t *testing.T) {
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

	content := []byte("OpenFile parent traversal")

	if err := fs.WriteFile("dir/file.txt", content); err != nil {
		t.Fatal(err)
	}

	file, err := fs.OpenFile("dir/../dir/./file.txt")
	if err != nil {
		t.Fatal(err)
	}

	got, err := file.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, content) {
		t.Fatalf(
			"OpenFile returned incorrect content: %q",
			got,
		)
	}

	if file.Name() != "FILE.TXT" {
		t.Fatalf(
			"File.Name() = %q, want %q",
			file.Name(),
			"FILE.TXT",
		)
	}
}

// Reject malformed paths through OpenFile
func TestOpenFileInvalidPathDoesNotMutate(t *testing.T) {
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

	content := []byte("valid")

	if err := fs.WriteFile("valid.txt", content); err != nil {
		t.Fatal(err)
	}

	before := append([]byte(nil), data...)

	invalidPaths := []string{
		"missing//file.txt",
		"valid.txt/",
		"/valid.txt//",
	}

	for _, path := range invalidPaths {
		if _, err := fs.OpenFile(path); err == nil {
			t.Fatalf(
				"OpenFile(%q) should reject invalid path",
				path,
			)
		}
	}

	if !bytes.Equal(data, before) {
		t.Fatal("OpenFile mutated the filesystem")
	}
}

// Verify OpenFile on an empty file
func TestOpenFileEmptyFile(t *testing.T) {
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

	file, err := fs.OpenFile("empty.txt")
	if err != nil {
		t.Fatal(err)
	}

	if file.Name() != "EMPTY.TXT" {
		t.Fatalf(
			"File.Name() = %q, want %q",
			file.Name(),
			"EMPTY.TXT",
		)
	}

	if file.Size() != 0 {
		t.Fatalf(
			"File.Size() = %d, want 0",
			file.Size(),
		)
	}

	content, err := file.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if len(content) != 0 {
		t.Fatalf(
			"ReadAll returned %d bytes, want 0",
			len(content),
		)
	}
}

// ReadAll() should be repeatable
// Since File does not maintain a read cursor,
// calling it twice should return the same data.
func TestFileReadAllIsRepeatable(t *testing.T) {
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

	content := []byte("repeatable File.ReadAll content")

	if err := fs.WriteFile("repeat.txt", content); err != nil {
		t.Fatal(err)
	}

	file, err := fs.OpenFile("repeat.txt")
	if err != nil {
		t.Fatal(err)
	}

	first, err := file.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	second, err := file.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(first, content) {
		t.Fatalf(
			"first ReadAll returned incorrect data: %q",
			first,
		)
	}

	if !bytes.Equal(second, content) {
		t.Fatalf(
			"second ReadAll returned incorrect data: %q",
			second,
		)
	}

	if !bytes.Equal(first, second) {
		t.Fatal("repeated ReadAll calls returned different data")
	}
}

// Verify that the File abstraction correctly reads a multi-cluster chain
func TestFileReadAllMultiCluster(t *testing.T) {
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

	content := bytes.Repeat([]byte("multi-cluster "), 100)

	if len(content) <= int(sectorSize) {
		t.Fatal("test content must span multiple clusters")
	}

	if err := fs.WriteFile("multi.txt", content); err != nil {
		t.Fatal(err)
	}

	file, err := fs.OpenFile("multi.txt")
	if err != nil {
		t.Fatal(err)
	}

	if file.Size() != uint32(len(content)) {
		t.Fatalf(
			"File.Size() = %d, want %d",
			file.Size(),
			len(content),
		)
	}

	got, err := file.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, content) {
		t.Fatal("File.ReadAll returned incorrect multi-cluster data")
	}
}

// File.ReadAll() rejects a corrupted cluster chain
func TestFileReadAllRejectsBadCluster(t *testing.T) {
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

	// Mark the file cluster as bad.
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

	// Construct a file entry pointing at the bad cluster.
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

	file, err := fs.OpenFile("bad.txt")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := file.ReadAll(); err == nil {
		t.Fatal("ReadAll should reject a bad cluster")
	}
}

func TestFileReadAllRejectsCyclicClusterChain(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 160

		rootCluster = uint32(2)
		fileCluster = uint32(3)
		nextCluster = uint32(4)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	// 3 -> 4 -> 3
	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(fileCluster)*4:],
		nextCluster,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(nextCluster)*4:],
		fileCluster,
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

	shortName, err := encodeShortName("cycle.txt")
	if err != nil {
		t.Fatal(err)
	}

	entry := encodeDirectoryEntry(
		shortName,
		0x20,
		fileCluster,
		uint32(sectorSize*2),
	)

	copy(
		data[int(dataOffset):int(dataOffset)+32],
		entry[:],
	)

	file, err := fs.OpenFile("cycle.txt")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := file.ReadAll(); err == nil {
		t.Fatal("ReadAll should reject a cyclic cluster chain")
	}
}

// Reject a free cluster inside the file chain
func TestFileReadAllRejectsFreeClusterInChain(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 160

		rootCluster = uint32(2)
		fileCluster = uint32(3)
		freeCluster = uint32(4)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	// The file points to a second cluster that is marked free.
	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(fileCluster)*4:],
		freeCluster,
	)

	// Cluster 4 remains zero/free.

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

	shortName, err := encodeShortName("free.txt")
	if err != nil {
		t.Fatal(err)
	}

	entry := encodeDirectoryEntry(
		shortName,
		0x20,
		fileCluster,
		uint32(sectorSize*2),
	)

	copy(
		data[int(dataOffset):int(dataOffset)+32],
		entry[:],
	)

	file, err := fs.OpenFile("free.txt")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := file.ReadAll(); err == nil {
		t.Fatal("ReadAll should reject a free cluster in the file chain")
	}
}

// Exact file-size truncation through File.ReadAll
func TestFileReadAllTrimsFinalClusterToFileSize(t *testing.T) {
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

	content := []byte("exact file size")
	padding := bytes.Repeat([]byte{0xAA}, int(sectorSize)-len(content))

	clusterData := append(
		append([]byte(nil), content...),
		padding...,
	)

	// Manually construct a file using cluster 3 so that the
	// on-disk cluster contains data beyond the declared size.
	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+3*4:],
		fat32EOF,
	)

	copy(
		data[int(dataOffset)+int(sectorSize):],
		clusterData,
	)

	shortName, err := encodeShortName("trim.txt")
	if err != nil {
		t.Fatal(err)
	}

	entry := encodeDirectoryEntry(
		shortName,
		0x20,
		3,
		uint32(len(content)),
	)

	copy(
		data[int(dataOffset):int(dataOffset)+32],
		entry[:],
	)

	file, err := fs.OpenFile("trim.txt")
	if err != nil {
		t.Fatal(err)
	}

	got, err := file.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, content) {
		t.Fatalf(
			"ReadAll returned %d bytes instead of the declared %d bytes",
			len(got),
			len(content),
		)
	}
}

// Test that an LFN sequence without the last-entry flag is rejected.
func TestReadDirectoryRejectsLFNWithoutLastFlag(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
	)

	data := make([]byte, dataOffset+2*bytesPerCluster)

	binary.LittleEndian.PutUint32(
		data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	shortName, err := encodeShortName("FILE.TXT")
	if err != nil {
		t.Fatal(err)
	}

	// LFN entry with sequence 1 but without the 0x40 last-entry flag.
	data[dataOffset] = 0x01
	data[dataOffset+0x0B] = 0x0F
	data[dataOffset+0x0D] = shortNameChecksum(shortName[:])

	// Following short entry.
	entry := encodeDirectoryEntry(shortName, 0x20, 0, 0)
	copy(data[dataOffset+32:dataOffset+64], entry[:])

	fs := NewReadWrite(
		bytes.NewReader(data),
		&writerAtBuffer{data: data},
	)

	fs.boot = BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: 1,
		NumberOfFATs:      1,
		SectorsPerFAT:     1,
		TotalSectors:      3,
		RootCluster:       rootCluster,
	}

	fs.layout = Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: bytesPerCluster,
	}

	if _, err := fs.ReadRootDirectory(); err == nil {
		t.Fatal("ReadRootDirectory unexpectedly accepted LFN without last-entry flag")
	}
}

// Test that an LFN sequence with a missing sequence number is rejected.
func TestReadDirectoryRejectsLFNWithMissingSequence(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
	)

	data := make([]byte, dataOffset+2*bytesPerCluster)

	binary.LittleEndian.PutUint32(
		data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	shortName, err := encodeShortName("FILE.TXT")
	if err != nil {
		t.Fatal(err)
	}

	// Last LFN entry claims sequence 2, but sequence 1 is missing.
	data[dataOffset] = 0x42
	data[dataOffset+0x0B] = 0x0F
	data[dataOffset+0x0D] = shortNameChecksum(shortName[:])

	// Following short entry.
	entry := encodeDirectoryEntry(shortName, 0x20, 0, 0)
	copy(data[dataOffset+32:dataOffset+64], entry[:])

	fs := NewReadWrite(
		bytes.NewReader(data),
		&writerAtBuffer{data: data},
	)

	fs.boot = BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: 1,
		NumberOfFATs:      1,
		SectorsPerFAT:     1,
		TotalSectors:      3,
		RootCluster:       rootCluster,
	}

	fs.layout = Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: bytesPerCluster,
	}

	if _, err := fs.ReadRootDirectory(); err == nil {
		t.Fatal("ReadRootDirectory unexpectedly accepted LFN with missing sequence")
	}
}

// Test that duplicate LFN sequence numbers are rejected.
func TestReadDirectoryRejectsLFNWithDuplicateSequence(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
	)

	data := make([]byte, dataOffset+2*bytesPerCluster)

	binary.LittleEndian.PutUint32(
		data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	shortName, err := encodeShortName("FILE.TXT")
	if err != nil {
		t.Fatal(err)
	}

	checksum := shortNameChecksum(shortName[:])

	// Last LFN entry claims sequence 2.
	data[dataOffset] = 0x42
	data[dataOffset+0x0B] = 0x0F
	data[dataOffset+0x0D] = checksum

	// Second entry incorrectly repeats sequence 2.
	data[dataOffset+32] = 0x02
	data[dataOffset+32+0x0B] = 0x0F
	data[dataOffset+32+0x0D] = checksum

	// Following short entry.
	entry := encodeDirectoryEntry(shortName, 0x20, 0, 0)
	copy(data[dataOffset+64:dataOffset+96], entry[:])

	fs := NewReadWrite(
		bytes.NewReader(data),
		&writerAtBuffer{data: data},
	)

	fs.boot = BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: 1,
		NumberOfFATs:      1,
		SectorsPerFAT:     1,
		TotalSectors:      3,
		RootCluster:       rootCluster,
	}

	fs.layout = Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: bytesPerCluster,
	}

	if _, err := fs.ReadRootDirectory(); err == nil {
		t.Fatal("ReadRootDirectory unexpectedly accepted duplicate LFN sequence")
	}
}

// This test rejects an LFN entry with reserved sequence bit 5 set.
func TestReadDirectoryRejectsLFNWithReservedSequenceBit(t *testing.T) {
	var shortName [11]byte
	copy(shortName[:], []byte("TEST    TXT"))

	checksum := shortNameChecksum(shortName[:])

	lfn, err := encodeLFNEntryUnits(0x41, []uint16{'T'}, checksum)
	if err != nil {
		t.Fatalf("encodeLFNEntryUnits: %v", err)
	}

	lfn[0] |= 0x20

	entry := encodeDirectoryEntry(
		shortName,
		0x20,
		0,
		0,
	)

	// Cluster 2 is the root directory. The FAT entry for cluster 2
	// must point to EOF, and the directory data belongs at DataOffset.
	data := make([]byte, 1024+4096)

	binary.LittleEndian.PutUint32(data[512+2*4:512+2*4+4], fat32EOF)

	copy(data[1024:1024+32], lfn[:])
	copy(data[1024+32:1024+64], entry[:])

	reader := bytes.NewReader(data)
	writer := &writerAtBuffer{data: append([]byte(nil), data...)}

	fs := NewReadWrite(reader, writer)
	fs.boot = BootSector{
		BytesPerSector:    512,
		SectorsPerCluster: 8,
		ReservedSectors:   1,
		NumberOfFATs:      1,
		SectorsPerFAT:     1,
		TotalSectors:      9,
		RootCluster:       2,
	}
	fs.layout = Layout{
		FATOffset:       512,
		DataOffset:      1024,
		BytesPerCluster: 4096,
	}

	if _, err := fs.ReadRootDirectory(); err == nil {
		t.Fatal("ReadRootDirectory unexpectedly accepted LFN with reserved sequence bit")
	}
}

// This test rejects an LFN entry with reserved sequence bit 7 set.
func TestReadDirectoryRejectsLFNWithHighReservedSequenceBit(t *testing.T) {
	var shortName [11]byte
	copy(shortName[:], []byte("TEST    TXT"))

	checksum := shortNameChecksum(shortName[:])

	lfn, err := encodeLFNEntryUnits(0x41, []uint16{'T'}, checksum)
	if err != nil {
		t.Fatalf("encodeLFNEntryUnits: %v", err)
	}

	lfn[0] |= 0x80

	entry := encodeDirectoryEntry(
		shortName,
		0x20,
		0,
		0,
	)

	// Cluster 2 is the root directory. The FAT entry for cluster 2
	// must point to EOF, and the directory data belongs at DataOffset.
	data := make([]byte, 1024+4096)

	binary.LittleEndian.PutUint32(data[512+2*4:512+2*4+4], fat32EOF)

	copy(data[1024:1024+32], lfn[:])
	copy(data[1024+32:1024+64], entry[:])

	reader := bytes.NewReader(data)
	writer := &writerAtBuffer{data: append([]byte(nil), data...)}

	fs := NewReadWrite(reader, writer)
	fs.boot = BootSector{
		BytesPerSector:    512,
		SectorsPerCluster: 8,
		ReservedSectors:   1,
		NumberOfFATs:      1,
		SectorsPerFAT:     1,
		TotalSectors:      9,
		RootCluster:       2,
	}
	fs.layout = Layout{
		FATOffset:       512,
		DataOffset:      1024,
		BytesPerCluster: 4096,
	}

	if _, err := fs.ReadRootDirectory(); err == nil {
		t.Fatal("ReadRootDirectory unexpectedly accepted LFN with high reserved sequence bit")
	}
}

// This test rejects an LFN entry with sequence number zero.
func TestReadDirectoryRejectsLFNWithZeroSequence(t *testing.T) {
	var shortName [11]byte
	copy(shortName[:], []byte("TEST    TXT"))

	checksum := shortNameChecksum(shortName[:])

	lfn, err := encodeLFNEntryUnits(0x40, []uint16{'T'}, checksum)
	if err != nil {
		t.Fatalf("encodeLFNEntryUnits: %v", err)
	}

	lfn[0] = 0x40

	entry := encodeDirectoryEntry(
		shortName,
		0x20,
		0,
		0,
	)

	data := make([]byte, 1024+4096)

	binary.LittleEndian.PutUint32(
		data[512+2*4:512+2*4+4],
		fat32EOF,
	)

	copy(data[1024:1024+32], lfn[:])
	copy(data[1024+32:1024+64], entry[:])

	reader := bytes.NewReader(data)
	writer := &writerAtBuffer{data: append([]byte(nil), data...)}

	fs := NewReadWrite(reader, writer)
	fs.boot = BootSector{
		BytesPerSector:    512,
		SectorsPerCluster: 8,
		ReservedSectors:   1,
		NumberOfFATs:      1,
		SectorsPerFAT:     1,
		TotalSectors:      9,
		RootCluster:       2,
	}
	fs.layout = Layout{
		FATOffset:       512,
		DataOffset:      1024,
		BytesPerCluster: 4096,
	}

	if _, err := fs.ReadRootDirectory(); err == nil {
		t.Fatal("ReadRootDirectory unexpectedly accepted LFN with zero sequence")
	}
}

// This test rejects an LFN sequence with the last-entry flag on a later entry.
func TestReadDirectoryRejectsLFNWithUnexpectedLastFlag(t *testing.T) {
	var shortName [11]byte
	copy(shortName[:], []byte("TEST    TXT"))

	checksum := shortNameChecksum(shortName[:])

	first, err := encodeLFNEntryUnits(0x42, []uint16{'A'}, checksum)
	if err != nil {
		t.Fatalf("encodeLFNEntryUnits first: %v", err)
	}

	second, err := encodeLFNEntryUnits(0x41, []uint16{'B'}, checksum)
	if err != nil {
		t.Fatalf("encodeLFNEntryUnits second: %v", err)
	}

	// The first entry has the correct last-entry flag. The second
	// entry incorrectly has it as well.
	second[0] |= 0x40

	entry := encodeDirectoryEntry(
		shortName,
		0x20,
		0,
		0,
	)

	data := make([]byte, 1024+4096)

	binary.LittleEndian.PutUint32(
		data[512+2*4:512+2*4+4],
		fat32EOF,
	)

	copy(data[1024:1024+32], first[:])
	copy(data[1024+32:1024+64], second[:])
	copy(data[1024+64:1024+96], entry[:])

	reader := bytes.NewReader(data)
	writer := &writerAtBuffer{data: append([]byte(nil), data...)}

	fs := NewReadWrite(reader, writer)
	fs.boot = BootSector{
		BytesPerSector:    512,
		SectorsPerCluster: 8,
		ReservedSectors:   1,
		NumberOfFATs:      1,
		SectorsPerFAT:     1,
		TotalSectors:      9,
		RootCluster:       2,
	}
	fs.layout = Layout{
		FATOffset:       512,
		DataOffset:      1024,
		BytesPerCluster: 4096,
	}

	if _, err := fs.ReadRootDirectory(); err == nil {
		t.Fatal("ReadRootDirectory unexpectedly accepted LFN with unexpected last-entry flag")
	}
}

// This test verifies that a directory listing exposes "." and ".." entries.
func TestReadDirectoryIncludesDotEntries(t *testing.T) {
	// We'll use the same minimal FAT32 layout as the other synthetic
	// directory tests.
	data := make([]byte, 1024+4096)

	// Root directory cluster 2 terminates here.
	binary.LittleEndian.PutUint32(
		data[512+2*4:512+2*4+4],
		fat32EOF,
	)

	dot, err := encodeSpecialDirectoryEntry(".", 2)
	if err != nil {
		t.Fatalf("encodeSpecialDirectoryEntry(.): %v", err)
	}

	dotDot, err := encodeSpecialDirectoryEntry("..", 0)
	if err != nil {
		t.Fatalf("encodeSpecialDirectoryEntry(..): %v", err)
	}

	copy(data[1024:1024+32], dot[:])
	copy(data[1024+32:1024+64], dotDot[:])

	reader := bytes.NewReader(data)
	fs := New(reader)

	fs.boot = BootSector{
		BytesPerSector:    512,
		SectorsPerCluster: 8,
		ReservedSectors:   1,
		NumberOfFATs:      1,
		SectorsPerFAT:     1,
		TotalSectors:      9,
		RootCluster:       2,
	}
	fs.layout = Layout{
		FATOffset:       512,
		DataOffset:      1024,
		BytesPerCluster: 4096,
	}

	entries, err := fs.ReadRootDirectory()
	if err != nil {
		t.Fatalf("ReadRootDirectory: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	if entries[0].Name != "." {
		t.Fatalf("first entry name = %q, want %q", entries[0].Name, ".")
	}

	if !entries[0].IsDir {
		t.Fatal("'.' entry is not marked as a directory")
	}

	if entries[0].StartCluster != 2 {
		t.Fatalf(
			"'.' start cluster = %d, want 2",
			entries[0].StartCluster,
		)
	}

	if entries[1].Name != ".." {
		t.Fatalf("second entry name = %q, want %q", entries[1].Name, "..")
	}

	if !entries[1].IsDir {
		t.Fatal("'..' entry is not marked as a directory")
	}

	if entries[1].StartCluster != 0 {
		t.Fatalf(
			"'..' start cluster = %d, want 0",
			entries[1].StartCluster,
		)
	}
}

// This test verifies that ReadDir hides "." and ".." bookkeeping entries.
func TestMkdirReadDirHidesDotEntries(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	if err := fs.Mkdir("/TESTDIR"); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	entries, err := fs.ReadDir("/TESTDIR")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf("got %d entries, want 0", len(entries))
	}

	// Read the underlying directory records to verify that "." and ".."
	// actually exist and were only filtered by ReadDir.
	dir, err := fs.Find("/TESTDIR")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	entries, err = fs.readDirectory(dir.StartCluster)
	if err != nil {
		t.Fatalf("readDirectory: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("underlying directory has %d entries, want 2", len(entries))
	}

	if entries[0].Name != "." {
		t.Fatalf(
			"first underlying entry name = %q, want %q",
			entries[0].Name,
			".",
		)
	}

	if entries[1].Name != ".." {
		t.Fatalf(
			"second underlying entry name = %q, want %q",
			entries[1].Name,
			"..",
		)
	}
}

// This test verifies that "." and ".." do not prevent child-file lookup.
func TestMkdirDotEntriesDoNotInterfereWithChildFileLookup(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	if err := fs.Mkdir("/TESTDIR"); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	if err := fs.WriteFile("/TESTDIR/FILE.TXT", []byte("hello")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entry, err := fs.Lookup("/TESTDIR/FILE.TXT")
	if err != nil {
		t.Fatalf("Lookup child file: %v", err)
	}

	if entry.Name != "FILE.TXT" {
		t.Fatalf("entry name = %q, want %q", entry.Name, "FILE.TXT")
	}

	if entry.IsDir {
		t.Fatal("child file is marked as a directory")
	}
}

// This test verifies that a volume-label entry is not exposed as a regular file.
func TestReadDirectoryIgnoresVolumeLabel(t *testing.T) {
	var volumeLabel [11]byte
	copy(volumeLabel[:], []byte("YP-Z5      "))

	entry := encodeDirectoryEntry(
		volumeLabel,
		0x08,
		0,
		0,
	)

	data := make([]byte, 1024+4096)

	binary.LittleEndian.PutUint32(
		data[512+2*4:512+2*4+4],
		fat32EOF,
	)

	copy(data[1024:1024+32], entry[:])

	reader := bytes.NewReader(data)
	fs := New(reader)

	fs.boot = BootSector{
		BytesPerSector:    512,
		SectorsPerCluster: 8,
		ReservedSectors:   1,
		NumberOfFATs:      1,
		SectorsPerFAT:     1,
		TotalSectors:      9,
		RootCluster:       2,
	}

	fs.layout = Layout{
		FATOffset:       512,
		DataOffset:      1024,
		BytesPerCluster: 4096,
	}

	entries, err := fs.ReadRootDirectory()
	if err != nil {
		t.Fatalf("ReadRootDirectory: %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf(
			"ReadRootDirectory returned %d entries, want 0",
			len(entries),
		)
	}
}

// An LFN immediately followed by end-of-directory is ignored.
func TestReadDirectoryIgnoresOrphanedLFNAtEndOfDirectory(t *testing.T) {
	data := make([]byte, 2048)

	// FAT: cluster 2 -> EOF.
	binary.LittleEndian.PutUint32(data[512+2*4:], fat32EOF)

	root := data[1024:]

	lfn, err := encodeLFNEntryUnits(
		0x41,
		utf16.Encode([]rune("orphan.txt")),
		0,
	)
	if err != nil {
		t.Fatalf("encode LFN entry: %v", err)
	}

	copy(root[0:32], lfn[:])

	// Mark the next directory entry as end-of-directory.
	root[32] = 0x00

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector:    512,
			SectorsPerCluster: 2,
			ReservedSectors:   1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      4,
			RootCluster:       2,
		},
		layout: Layout{
			FATOffset:       512,
			DataOffset:      1024,
			BytesPerCluster: 1024,
		},
	}

	entries, err := fs.readDirectory(2)
	if err != nil {
		t.Fatalf("readDirectory returned error: %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf("readDirectory returned %d entries, want 0", len(entries))
	}
}

// An LFN followed by a deleted entry is discarded before the next valid entry.
func TestReadDirectoryIgnoresOrphanedLFNBeforeDeletedEntry(t *testing.T) {
	data := make([]byte, 2048)

	// FAT: cluster 2 -> EOF.
	binary.LittleEndian.PutUint32(data[512+2*4:], fat32EOF)

	root := data[1024:]

	lfn, err := encodeLFNEntryUnits(
		0x41,
		utf16.Encode([]rune("orphan.txt")),
		0,
	)
	if err != nil {
		t.Fatalf("encode LFN entry: %v", err)
	}

	copy(root[0:32], lfn[:])

	// A deleted entry must discard the pending LFN.
	root[32] = 0xE5

	shortName, err := encodeShortName("VALID.TXT")
	if err != nil {
		t.Fatalf("encode short name: %v", err)
	}

	short := encodeDirectoryEntry(
		shortName,
		0x20,
		0,
		0,
	)

	copy(root[64:96], short[:])

	// Mark the next entry as end-of-directory.
	root[96] = 0x00

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector:    512,
			SectorsPerCluster: 2,
			ReservedSectors:   1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      4,
			RootCluster:       2,
		},
		layout: Layout{
			FATOffset:       512,
			DataOffset:      1024,
			BytesPerCluster: 1024,
		},
	}

	entries, err := fs.readDirectory(2)
	if err != nil {
		t.Fatalf("readDirectory returned error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("readDirectory returned %d entries, want 1", len(entries))
	}

	if entries[0].Name != "VALID.TXT" {
		t.Fatalf(
			"entry name = %q, want %q",
			entries[0].Name,
			"VALID.TXT",
		)
	}
}

// A valid UTF-16 surrogate pair is decoded into the corresponding Unicode character.
func TestReadDirectoryDecodesLFNSurrogatePair(t *testing.T) {
	data := make([]byte, 2048)

	// FAT: cluster 2 -> EOF.
	binary.LittleEndian.PutUint32(data[512+2*4:], fat32EOF)

	root := data[1024:]

	shortName, err := encodeShortName("EMOJI.TXT")
	if err != nil {
		t.Fatalf("encode short name: %v", err)
	}

	name := "emoji 😀.txt"
	units := utf16.Encode([]rune(name))
	checksum := shortNameChecksum(shortName[:])

	lfn, err := encodeLFNEntryUnits(0x41, units, checksum)
	if err != nil {
		t.Fatalf("encode LFN entry: %v", err)
	}

	copy(root[0:32], lfn[:])

	short := encodeDirectoryEntry(
		shortName,
		0x20,
		0,
		0,
	)

	copy(root[32:64], short[:])

	// Mark the next directory entry as end-of-directory.
	root[64] = 0x00

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector:    512,
			SectorsPerCluster: 2,
			ReservedSectors:   1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      4,
			RootCluster:       2,
		},
		layout: Layout{
			FATOffset:       512,
			DataOffset:      1024,
			BytesPerCluster: 1024,
		},
	}

	entries, err := fs.readDirectory(2)
	if err != nil {
		t.Fatalf("readDirectory returned error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("readDirectory returned %d entries, want 1", len(entries))
	}

	if entries[0].Name != name {
		t.Fatalf(
			"entry name = %q, want %q",
			entries[0].Name,
			name,
		)
	}
}

// An unpaired UTF-16 surrogate is replaced with U+FFFD when decoding an LFN.
func TestReadDirectoryHandlesUnpairedLFNSurrogate(t *testing.T) {
	data := make([]byte, 2048)

	// FAT: cluster 2 -> EOF.
	binary.LittleEndian.PutUint32(data[512+2*4:], fat32EOF)

	root := data[1024:]

	shortName, err := encodeShortName("BAD.TXT")
	if err != nil {
		t.Fatalf("encode short name: %v", err)
	}

	// Deliberately construct an invalid UTF-16 sequence containing
	// a lone high surrogate.
	units := []uint16{
		'H',
		0xD83D,
		'e',
		'l',
		'l',
		'o',
	}

	checksum := shortNameChecksum(shortName[:])

	lfn, err := encodeLFNEntryUnits(0x41, units, checksum)
	if err != nil {
		t.Fatalf("encode LFN entry: %v", err)
	}

	copy(root[0:32], lfn[:])

	short := encodeDirectoryEntry(
		shortName,
		0x20,
		0,
		0,
	)

	copy(root[32:64], short[:])

	// Mark the next directory entry as end-of-directory.
	root[64] = 0x00

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector:    512,
			SectorsPerCluster: 2,
			ReservedSectors:   1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      4,
			RootCluster:       2,
		},
		layout: Layout{
			FATOffset:       512,
			DataOffset:      1024,
			BytesPerCluster: 1024,
		},
	}

	entries, err := fs.readDirectory(2)
	if err != nil {
		t.Fatalf("readDirectory returned error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("readDirectory returned %d entries, want 1", len(entries))
	}

	want := "H\uFFFDello"

	if entries[0].Name != want {
		t.Fatalf(
			"entry name = %q, want %q",
			entries[0].Name,
			want,
		)
	}
}

// An LFN terminator followed by 0xFFFF padding is decoded without the padding.
func TestReadDirectoryLFNTerminatorAndPadding(t *testing.T) {
	data := make([]byte, 2048)

	// FAT: cluster 2 -> EOF.
	binary.LittleEndian.PutUint32(data[512+2*4:], fat32EOF)

	root := data[1024:]

	shortName, err := encodeShortName("PAD.TXT")
	if err != nil {
		t.Fatalf("encode short name: %v", err)
	}

	name := "Padding.txt"
	units := utf16.Encode([]rune(name))
	checksum := shortNameChecksum(shortName[:])

	lfn, err := encodeLFNEntryUnits(0x41, units, checksum)
	if err != nil {
		t.Fatalf("encode LFN entry: %v", err)
	}

	copy(root[0:32], lfn[:])

	short := encodeDirectoryEntry(
		shortName,
		0x20,
		0,
		0,
	)

	copy(root[32:64], short[:])
	root[64] = 0x00

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector:    512,
			SectorsPerCluster: 2,
			ReservedSectors:   1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      4,
			RootCluster:       2,
		},
		layout: Layout{
			FATOffset:       512,
			DataOffset:      1024,
			BytesPerCluster: 1024,
		},
	}

	entries, err := fs.readDirectory(2)
	if err != nil {
		t.Fatalf("readDirectory returned error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("readDirectory returned %d entries, want 1", len(entries))
	}

	if entries[0].Name != name {
		t.Fatalf(
			"entry name = %q, want %q",
			entries[0].Name,
			name,
		)
	}
}

// An LFN using all 13 UTF-16 units without a terminator is decoded correctly.
func TestReadDirectoryLFNExactlyFillsEntry(t *testing.T) {
	data := make([]byte, 2048)

	// FAT: cluster 2 -> EOF.
	binary.LittleEndian.PutUint32(data[512+2*4:], fat32EOF)

	root := data[1024:]

	shortName, err := encodeShortName("THIRTEEN.TXT")
	if err != nil {
		t.Fatalf("encode short name: %v", err)
	}

	name := "1234567890123"
	units := utf16.Encode([]rune(name))

	if len(units) != 13 {
		t.Fatalf(
			"test name has %d UTF-16 units, want 13",
			len(units),
		)
	}

	checksum := shortNameChecksum(shortName[:])

	lfn, err := encodeLFNEntryUnits(0x41, units, checksum)
	if err != nil {
		t.Fatalf("encode LFN entry: %v", err)
	}

	copy(root[0:32], lfn[:])

	short := encodeDirectoryEntry(
		shortName,
		0x20,
		0,
		0,
	)

	copy(root[32:64], short[:])
	root[64] = 0x00

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector:    512,
			SectorsPerCluster: 2,
			ReservedSectors:   1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      4,
			RootCluster:       2,
		},
		layout: Layout{
			FATOffset:       512,
			DataOffset:      1024,
			BytesPerCluster: 1024,
		},
	}

	entries, err := fs.readDirectory(2)
	if err != nil {
		t.Fatalf("readDirectory returned error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("readDirectory returned %d entries, want 1", len(entries))
	}

	if entries[0].Name != name {
		t.Fatalf(
			"entry name = %q, want %q",
			entries[0].Name,
			name,
		)
	}
}

// Read-only and hidden attributes do not prevent a file from being listed.
func TestReadDirectoryIncludesReadOnlyHiddenFile(t *testing.T) {
	data := make([]byte, 2048)

	// FAT: cluster 2 -> EOF.
	binary.LittleEndian.PutUint32(data[512+2*4:], fat32EOF)

	root := data[1024:]

	shortName, err := encodeShortName("SECRET.TXT")
	if err != nil {
		t.Fatalf("encode short name: %v", err)
	}

	// Read-only + hidden + archive.
	short := encodeDirectoryEntry(
		shortName,
		0x01|0x02|0x20,
		0,
		123,
	)

	copy(root[0:32], short[:])
	root[32] = 0x00

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector:    512,
			SectorsPerCluster: 2,
			ReservedSectors:   1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      4,
			RootCluster:       2,
		},
		layout: Layout{
			FATOffset:       512,
			DataOffset:      1024,
			BytesPerCluster: 1024,
		},
	}

	entries, err := fs.readDirectory(2)
	if err != nil {
		t.Fatalf("readDirectory returned error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("readDirectory returned %d entries, want 1", len(entries))
	}

	if entries[0].Name != "SECRET.TXT" {
		t.Fatalf(
			"entry name = %q, want %q",
			entries[0].Name,
			"SECRET.TXT",
		)
	}

	if entries[0].Size != 123 {
		t.Fatalf(
			"entry size = %d, want 123",
			entries[0].Size,
		)
	}

	if entries[0].IsDir {
		t.Fatal("read-only hidden file reported as directory")
	}
}

// The system attribute does not prevent a file from being listed.
func TestReadDirectoryIncludesSystemFile(t *testing.T) {
	data := make([]byte, 2048)

	// FAT: cluster 2 -> EOF.
	binary.LittleEndian.PutUint32(data[512+2*4:], fat32EOF)

	root := data[1024:]

	shortName, err := encodeShortName("SYSTEM.BIN")
	if err != nil {
		t.Fatalf("encode short name: %v", err)
	}

	short := encodeDirectoryEntry(
		shortName,
		0x04,
		0,
		456,
	)

	copy(root[0:32], short[:])
	root[32] = 0x00

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector:    512,
			SectorsPerCluster: 2,
			ReservedSectors:   1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      4,
			RootCluster:       2,
		},
		layout: Layout{
			FATOffset:       512,
			DataOffset:      1024,
			BytesPerCluster: 1024,
		},
	}

	entries, err := fs.readDirectory(2)
	if err != nil {
		t.Fatalf("readDirectory returned error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("readDirectory returned %d entries, want 1", len(entries))
	}

	if entries[0].Name != "SYSTEM.BIN" {
		t.Fatalf(
			"entry name = %q, want %q",
			entries[0].Name,
			"SYSTEM.BIN",
		)
	}

	if entries[0].Size != 456 {
		t.Fatalf(
			"entry size = %d, want 456",
			entries[0].Size,
		)
	}

	if entries[0].IsDir {
		t.Fatal("system file reported as directory")
	}
}

// A non-empty file with no start cluster is rejected when its contents are read.
func TestReadFileRejectsNonEmptyFileWithZeroStartCluster(t *testing.T) {
	data := make([]byte, 2048)

	// FAT: cluster 2 -> EOF.
	binary.LittleEndian.PutUint32(data[512+2*4:], fat32EOF)

	root := data[1024:]

	shortName, err := encodeShortName("BROKEN.TXT")
	if err != nil {
		t.Fatalf("encode short name: %v", err)
	}

	short := encodeDirectoryEntry(
		shortName,
		0x20,
		0,
		1,
	)

	copy(root[0:32], short[:])
	root[32] = 0x00

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector:    512,
			SectorsPerCluster: 2,
			ReservedSectors:   1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      4,
			RootCluster:       2,
		},
		layout: Layout{
			FATOffset:       512,
			DataOffset:      1024,
			BytesPerCluster: 1024,
		},
	}

	_, err = fs.ReadFile("BROKEN.TXT")
	if err == nil {
		t.Fatal("ReadFile returned nil error for non-empty file with zero start cluster")
	}
}

// A file whose start cluster exceeds the filesystem range is rejected.
func TestReadFileRejectsStartClusterAboveMaxCluster(t *testing.T) {
	data := make([]byte, 2048)

	// FAT: cluster 2 -> EOF.
	binary.LittleEndian.PutUint32(data[512+2*4:], fat32EOF)

	root := data[1024:]

	shortName, err := encodeShortName("BROKEN.TXT")
	if err != nil {
		t.Fatalf("encode short name: %v", err)
	}

	// This minimal filesystem has MaxCluster == 3.
	invalidCluster := uint32(4)

	short := encodeDirectoryEntry(
		shortName,
		0x20,
		invalidCluster,
		1,
	)

	copy(root[0:32], short[:])
	root[32] = 0x00

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector:    512,
			SectorsPerCluster: 2,
			ReservedSectors:   1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      4,
			RootCluster:       2,
		},
		layout: Layout{
			FATOffset:       512,
			DataOffset:      1024,
			BytesPerCluster: 1024,
		},
	}

	_, err = fs.ReadFile("BROKEN.TXT")
	if err == nil {
		t.Fatal("ReadFile returned nil error for start cluster above MaxCluster")
	}
}

// A file whose size requires more clusters than its FAT chain provides is rejected.
func TestReadFileRejectsShortClusterChainForFileSize(t *testing.T) {
	data := make([]byte, 3072)

	// FAT:
	// cluster 2 -> EOF (root directory)
	// cluster 3 -> EOF (file has only one data cluster)
	binary.LittleEndian.PutUint32(data[512+2*4:], fat32EOF)
	binary.LittleEndian.PutUint32(data[512+3*4:], fat32EOF)

	root := data[1024:]

	shortName, err := encodeShortName("SHORT.TXT")
	if err != nil {
		t.Fatalf("encode short name: %v", err)
	}

	// Cluster size is 1024 bytes, but the file claims 1025 bytes.
	short := encodeDirectoryEntry(
		shortName,
		0x20,
		3,
		1025,
	)

	copy(root[0:32], short[:])
	root[32] = 0x00

	// Put one valid byte pattern into the only file cluster.
	fileCluster := data[2048:]
	for i := range fileCluster {
		fileCluster[i] = 0xAA
	}

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector:    512,
			SectorsPerCluster: 2,
			ReservedSectors:   1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      6,
			RootCluster:       2,
		},
		layout: Layout{
			FATOffset:       512,
			DataOffset:      1024,
			BytesPerCluster: 1024,
		},
	}

	_, err = fs.ReadFile("SHORT.TXT")
	if err == nil {
		t.Fatal(
			"ReadFile returned nil error for file size larger than cluster chain",
		)
	}
}

// A zero-length file requires no data clusters.
func TestClustersForSizeZero(t *testing.T) {
	fs := &Filesystem{
		layout: Layout{
			BytesPerCluster: 4096,
		},
	}

	if got := fs.clustersForSize(0); got != 0 {
		t.Fatalf("clustersForSize(0) = %d, want 0", got)
	}
}

// A size exactly divisible by the cluster size requires no extra cluster.
func TestClustersForSizeExactClusterBoundary(t *testing.T) {
	fs := &Filesystem{
		layout: Layout{
			BytesPerCluster: 4096,
		},
	}

	if got := fs.clustersForSize(8192); got != 2 {
		t.Fatalf("clustersForSize(8192) = %d, want 2", got)
	}
}

// The maximum uint32 file size rounds up without overflowing.
func TestClustersForSizeMaximumUint32(t *testing.T) {
	fs := &Filesystem{
		layout: Layout{
			BytesPerCluster: 4096,
		},
	}

	const maxUint32 = uint32(^uint32(0))

	got := fs.clustersForSize(int(maxUint32))

	want := 1048576

	if got != want {
		t.Fatalf(
			"clustersForSize(max uint32) = %d, want %d",
			got,
			want,
		)
	}
}

// A FAT32 reserved cluster value is rejected rather than followed as a cluster.
func TestClusterChainRejectsReservedFATValue(t *testing.T) {
	data := make([]byte, 3072)

	// FAT:
	// cluster 2 -> reserved value 0x0FFFFFF0
	binary.LittleEndian.PutUint32(
		data[512+2*4:],
		0x0FFFFFF0,
	)

	// Root directory cluster 2.
	root := data[1024:]
	root[0] = 0x00

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		boot: BootSector{
			BytesPerSector:    512,
			SectorsPerCluster: 2,
			ReservedSectors:   1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      6,
			RootCluster:       2,
		},
		layout: Layout{
			FATOffset:       512,
			DataOffset:      1024,
			BytesPerCluster: 1024,
		},
	}

	_, err := fs.ClusterChain(2)
	if err == nil {
		t.Fatal("ClusterChain returned nil error for reserved FAT value")
	}
}

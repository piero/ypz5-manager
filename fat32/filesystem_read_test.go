package fat32

import (
	"bytes"
	"encoding/binary"
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

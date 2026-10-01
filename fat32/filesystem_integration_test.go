package fat32

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
)

func TestFileLifecycle(t *testing.T) {
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

	const filename = "lifecycle.txt"

	firstContent := []byte("first version")
	secondContent := []byte("second version")
	thirdContent := []byte("third version")

	if err := fs.WriteFile(filename, firstContent); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, firstContent) {
		t.Fatalf(
			"initial ReadFile returned %q, want %q",
			got,
			firstContent,
		)
	}

	if err := fs.ReplaceFile(filename, secondContent); err != nil {
		t.Fatal(err)
	}

	got, err = fs.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, secondContent) {
		t.Fatalf(
			"ReadFile after ReplaceFile returned %q, want %q",
			got,
			secondContent,
		)
	}

	if err := fs.Delete(filename); err != nil {
		t.Fatal(err)
	}

	if _, err := fs.ReadFile(filename); err == nil {
		t.Fatal("ReadFile should fail after Delete")
	}

	if err := fs.WriteFile(filename, thirdContent); err != nil {
		t.Fatal(err)
	}

	got, err = fs.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, thirdContent) {
		t.Fatalf(
			"ReadFile after recreating file returned %q, want %q",
			got,
			thirdContent,
		)
	}
}

// Creates a directory, writes a file inside it, replaces the file, and reads it back.
func TestDirectoryFileLifecycle(t *testing.T) {
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

	if err := fs.Mkdir("documents"); err != nil {
		t.Fatal(err)
	}

	firstContent := []byte("first document")
	secondContent := []byte("updated document")

	if err := fs.WriteFile(
		"documents/report.txt",
		firstContent,
	); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile("documents/report.txt")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, firstContent) {
		t.Fatalf(
			"initial file content = %q, want %q",
			got,
			firstContent,
		)
	}

	if err := fs.ReplaceFile(
		"documents/report.txt",
		secondContent,
	); err != nil {
		t.Fatal(err)
	}

	got, err = fs.ReadFile("documents/report.txt")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, secondContent) {
		t.Fatalf(
			"replaced file content = %q, want %q",
			got,
			secondContent,
		)
	}
}

// Deletes and recreates a file inside an existing directory.
// Mkdir → WriteFile → Delete → WriteFile → ReadFile
func TestDirectoryFileDeleteLifecycle(t *testing.T) {
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

	if err := fs.Mkdir("documents"); err != nil {
		t.Fatal(err)
	}

	firstContent := []byte("first document")
	secondContent := []byte("recreated document")

	if err := fs.WriteFile(
		"documents/report.txt",
		firstContent,
	); err != nil {
		t.Fatal(err)
	}

	if err := fs.Delete("documents/report.txt"); err != nil {
		t.Fatal(err)
	}

	if _, err := fs.ReadFile("documents/report.txt"); err == nil {
		t.Fatal("deleted file should not be readable")
	}

	if err := fs.WriteFile(
		"documents/report.txt",
		secondContent,
	); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile("documents/report.txt")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, secondContent) {
		t.Fatalf(
			"recreated file content = %q, want %q",
			got,
			secondContent,
		)
	}
}

// Creates nested directories and verifies file operations through the complete path.
func TestNestedDirectoryFileLifecycle(t *testing.T) {
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

	if err := fs.Mkdir("a"); err != nil {
		t.Fatal(err)
	}

	if err := fs.Mkdir("a/b"); err != nil {
		t.Fatal(err)
	}

	if err := fs.Mkdir("a/b/c"); err != nil {
		t.Fatal(err)
	}

	content := []byte("deeply nested file")

	if err := fs.WriteFile(
		"a/b/c/file.txt",
		content,
	); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile("a/b/c/file.txt")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, content) {
		t.Fatalf(
			"nested file content = %q, want %q",
			got,
			content,
		)
	}

	got, err = fs.ReadFile("a/./b/../b/c/./file.txt")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, content) {
		t.Fatalf(
			"nested traversal content = %q, want %q",
			got,
			content,
		)
	}
}

// Replaces a file with data of different sizes while keeping the same entry.
func TestReplaceFileLifecycle(t *testing.T) {
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

	initial := bytes.Repeat([]byte("a"), 100)
	replacement := bytes.Repeat([]byte("b"), 1200)
	final := bytes.Repeat([]byte("c"), 20)

	if err := fs.WriteFile(
		"song.bin",
		initial,
	); err != nil {
		t.Fatal(err)
	}

	if err := fs.ReplaceFile(
		"song.bin",
		replacement,
	); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile("song.bin")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, replacement) {
		t.Fatalf(
			"replaced file content has length %d, want %d",
			len(got),
			len(replacement),
		)
	}

	if err := fs.ReplaceFile(
		"song.bin",
		final,
	); err != nil {
		t.Fatal(err)
	}

	got, err = fs.ReadFile("song.bin")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, final) {
		t.Fatalf(
			"second replacement content = %q, want %q",
			got,
			final,
		)
	}

	entry, err := fs.OpenFile("song.bin")
	if err != nil {
		t.Fatal(err)
	}

	if entry.Name() != "SONG.BIN" {
		t.Fatalf(
			"file name after replacement = %q, want %q",
			entry.Name(),
			"SONG.BIN",
		)
	}

	if entry.Size() != uint32(len(final)) {
		t.Fatalf(
			"file size after replacement = %d, want %d",
			entry.Size(),
			len(final),
		)
	}
}

// Reuses a deleted directory slot when creating another file.
func TestDeleteAndReuseDirectorySlot(t *testing.T) {
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
		"first.txt",
		[]byte("first"),
	); err != nil {
		t.Fatal(err)
	}

	if err := fs.Delete("first.txt"); err != nil {
		t.Fatal(err)
	}

	if err := fs.WriteFile(
		"second.txt",
		[]byte("second"),
	); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile("second.txt")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("second")) {
		t.Fatalf(
			"reused directory slot content = %q, want %q",
			got,
			"second",
		)
	}

	if _, err := fs.ReadFile("first.txt"); err == nil {
		t.Fatal("deleted file should remain absent after directory slot reuse")
	}
}

// Creates and replaces a long-named file inside nested directories.
func TestNestedLongFilenameLifecycle(t *testing.T) {
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

	if err := fs.Mkdir("music"); err != nil {
		t.Fatal(err)
	}

	if err := fs.Mkdir("music/Albums"); err != nil {
		t.Fatal(err)
	}

	const filename = "A Very Long Album Track Name.mp3"

	initial := []byte("initial track")
	replacement := []byte("replacement track")

	if err := fs.WriteFile(
		"music/Albums/"+filename,
		initial,
	); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile(
		"music/albums/" + filename,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, initial) {
		t.Fatalf(
			"initial content = %q, want %q",
			got,
			initial,
		)
	}

	if err := fs.ReplaceFile(
		"music/ALBUMS/"+filename,
		replacement,
	); err != nil {
		t.Fatal(err)
	}

	got, err = fs.ReadFile(
		"music/albums/" + filename,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, replacement) {
		t.Fatalf(
			"replacement content = %q, want %q",
			got,
			replacement,
		)
	}

	if err := fs.Delete(
		"music/albums/" + filename,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := fs.ReadFile(
		"music/albums/" + filename,
	); err == nil {
		t.Fatal("deleted long-named file should not be readable")
	}
}

// Preserves a Unicode long filename through creation, replacement, and deletion.
func TestNestedUnicodeFilenameLifecycle(t *testing.T) {
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

	if err := fs.Mkdir("music"); err != nil {
		t.Fatal(err)
	}

	if err := fs.Mkdir("music/日本語"); err != nil {
		t.Fatal(err)
	}

	const filename = "🎵 日本語の曲.mp3"

	content := []byte("unicode track")

	if err := fs.WriteFile(
		"music/日本語/"+filename,
		content,
	); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile(
		"music/日本語/" + filename,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, content) {
		t.Fatalf(
			"Unicode filename content = %q, want %q",
			got,
			content,
		)
	}

	file, err := fs.OpenFile(
		"music/日本語/" + filename,
	)
	if err != nil {
		t.Fatal(err)
	}

	if file.Name() != filename {
		t.Fatalf(
			"Unicode filename = %q, want %q",
			file.Name(),
			filename,
		)
	}

	if err := fs.Delete(
		"music/日本語/" + filename,
	); err != nil {
		t.Fatal(err)
	}
}

// Creates, reads, replaces, and deletes an empty file.
func TestEmptyFileLifecycle(t *testing.T) {
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

	if file.Size() != 0 {
		t.Fatalf(
			"empty file size = %d, want 0",
			file.Size(),
		)
	}

	got, err := file.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 0 {
		t.Fatalf(
			"empty file content length = %d, want 0",
			len(got),
		)
	}

	if err := fs.Delete("empty.txt"); err != nil {
		t.Fatal(err)
	}

	if _, err := fs.OpenFile("empty.txt"); err == nil {
		t.Fatal("deleted empty file should not exist")
	}
}

// Replaces a non-empty file with an empty file and then restores its content.
func TestReplaceWithEmptyFileLifecycle(t *testing.T) {
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

	initial := []byte("some initial content")
	final := []byte("content restored after empty replacement")

	if err := fs.WriteFile(
		"replace.txt",
		initial,
	); err != nil {
		t.Fatal(err)
	}

	if err := fs.ReplaceFile(
		"replace.txt",
		nil,
	); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile("replace.txt")
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 0 {
		t.Fatalf(
			"content after empty replacement has length %d, want 0",
			len(got),
		)
	}

	file, err := fs.OpenFile("replace.txt")
	if err != nil {
		t.Fatal(err)
	}

	if file.Size() != 0 {
		t.Fatalf(
			"size after empty replacement = %d, want 0",
			file.Size(),
		)
	}

	if err := fs.ReplaceFile(
		"replace.txt",
		final,
	); err != nil {
		t.Fatal(err)
	}

	got, err = fs.ReadFile("replace.txt")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, final) {
		t.Fatalf(
			"content after restoring file = %q, want %q",
			got,
			final,
		)
	}
}

// Expands the root directory when more entries are created than one cluster can hold.
func TestRootDirectoryExpansionLifecycle(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 2
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 160

		rootCluster = uint32(2)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize) * 2

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

	rootOffset := int(dataOffset)
	data[rootOffset] = 0x00

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

	// A 512-byte cluster contains 16 directory entries.
	const fileCount = 20

	for i := 0; i < fileCount; i++ {
		name := fmt.Sprintf("file%02d.txt", i)

		if err := fs.WriteFile(
			name,
			[]byte(name),
		); err != nil {
			t.Fatalf(
				"WriteFile(%q): %v",
				name,
				err,
			)
		}
	}

	for i := 0; i < fileCount; i++ {
		name := fmt.Sprintf("file%02d.txt", i)

		got, err := fs.ReadFile(name)
		if err != nil {
			t.Fatalf(
				"ReadFile(%q): %v",
				name,
				err,
			)
		}

		if !bytes.Equal(got, []byte(name)) {
			t.Fatalf(
				"ReadFile(%q) = %q, want %q",
				name,
				got,
				name,
			)
		}
	}

	chain, err := fs.ClusterChain(rootCluster)
	if err != nil {
		t.Fatal(err)
	}

	if len(chain) < 2 {
		t.Fatalf(
			"root directory chain length = %d, want at least 2",
			len(chain),
		)
	}
}

// Expands a nested directory when it runs out of directory-entry space.
func TestNestedDirectoryExpansionLifecycle(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 2
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 160

		rootCluster = uint32(2)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize) * 2

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

	if err := fs.Mkdir("documents"); err != nil {
		t.Fatal(err)
	}

	// A 512-byte cluster contains 16 directory entries, two of which
	// are occupied by "." and "..".
	const fileCount = 20

	for i := 0; i < fileCount; i++ {
		name := fmt.Sprintf("file%02d.txt", i)

		if err := fs.WriteFile(
			"documents/"+name,
			[]byte(name),
		); err != nil {
			t.Fatalf(
				"WriteFile(%q): %v",
				name,
				err,
			)
		}
	}

	for i := 0; i < fileCount; i++ {
		name := fmt.Sprintf("file%02d.txt", i)

		got, err := fs.ReadFile(
			"documents/" + name,
		)
		if err != nil {
			t.Fatalf(
				"ReadFile(%q): %v",
				name,
				err,
			)
		}

		if !bytes.Equal(got, []byte(name)) {
			t.Fatalf(
				"ReadFile(%q) = %q, want %q",
				name,
				got,
				name,
			)
		}
	}

	if err := fs.WriteFile(
		"documents/after-expansion.txt",
		[]byte("after expansion"),
	); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile(
		"documents/after-expansion.txt",
	)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("after expansion")) {
		t.Fatalf(
			"post-expansion file content = %q, want %q",
			got,
			"after expansion",
		)
	}

	if err := fs.Delete(
		"documents/file00.txt",
	); err != nil {
		t.Fatal(err)
	}

	if _, err := fs.ReadFile(
		"documents/file00.txt",
	); err == nil {
		t.Fatal("deleted file should not remain readable")
	}

	if err := fs.WriteFile(
		"documents/reused.txt",
		[]byte("reused"),
	); err != nil {
		t.Fatal(err)
	}

	got, err = fs.ReadFile("documents/reused.txt")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("reused")) {
		t.Fatalf(
			"reused directory content = %q, want %q",
			got,
			"reused",
		)
	}
}

// Expands a directory when a long filename requires multiple directory slots.
func TestLongFilenameDirectoryExpansionLifecycle(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 2
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 160

		rootCluster = uint32(2)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize) * 2

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

	// Fill most of the first directory cluster with short-name entries.
	const shortFileCount = 13

	for i := 0; i < shortFileCount; i++ {
		name := fmt.Sprintf("file%02d.txt", i)

		if err := fs.WriteFile(
			name,
			[]byte(name),
		); err != nil {
			t.Fatalf(
				"WriteFile(%q): %v",
				name,
				err,
			)
		}
	}

	longName := "This is a deliberately long filename that needs several LFN entries.txt"

	if err := fs.WriteFile(
		longName,
		[]byte("long filename"),
	); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile(longName)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("long filename")) {
		t.Fatalf(
			"long filename content = %q, want %q",
			got,
			"long filename",
		)
	}

	for i := 0; i < shortFileCount; i++ {
		name := fmt.Sprintf("file%02d.txt", i)

		got, err := fs.ReadFile(name)
		if err != nil {
			t.Fatalf(
				"ReadFile(%q): %v",
				name,
				err,
			)
		}

		if !bytes.Equal(got, []byte(name)) {
			t.Fatalf(
				"ReadFile(%q) = %q, want %q",
				name,
				got,
				name,
			)
		}
	}

	if err := fs.Delete(longName); err != nil {
		t.Fatal(err)
	}

	if _, err := fs.ReadFile(longName); err == nil {
		t.Fatal("deleted long filename should not remain readable")
	}
}

// Reuses the multiple directory slots freed by a deleted long filename.
func TestLongFilenameSlotReuseLifecycle(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 2
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 160

		rootCluster = uint32(2)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize) * 2

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

	longName := "A filename long enough to require multiple LFN directory entries.txt"

	if err := fs.WriteFile(
		longName,
		[]byte("original"),
	); err != nil {
		t.Fatal(err)
	}

	if err := fs.Delete(longName); err != nil {
		t.Fatal(err)
	}

	replacementName := "Another filename long enough to require multiple LFN directory entries.txt"

	if err := fs.WriteFile(
		replacementName,
		[]byte("replacement"),
	); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile(replacementName)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("replacement")) {
		t.Fatalf(
			"reused LFN slots content = %q, want %q",
			got,
			"replacement",
		)
	}

	if _, err := fs.ReadFile(longName); err == nil {
		t.Fatal("deleted long filename should remain absent")
	}
}

// Creates multiple long filenames that require distinct generated short names.
func TestLongFilenameShortNameCollisionLifecycle(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 2
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 160

		rootCluster = uint32(2)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize) * 2

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

	names := []string{
		"Collision Test One.txt",
		"Collision Test Two.txt",
		"Collision Test Three.txt",
	}

	for i, name := range names {
		if err := fs.WriteFile(
			name,
			[]byte(fmt.Sprintf("content-%d", i)),
		); err != nil {
			t.Fatalf(
				"WriteFile(%q): %v",
				name,
				err,
			)
		}
	}

	records, err := fs.readDirectoryRecords(rootCluster)
	if err != nil {
		t.Fatal(err)
	}

	shortNames := make(map[string]string)

	for _, record := range records {
		for _, name := range names {
			if strings.EqualFold(record.Entry.Name, name) {
				shortNames[name] = record.Entry.ShortName
			}
		}
	}

	if len(shortNames) != len(names) {
		t.Fatalf(
			"found %d generated long-name records, want %d",
			len(shortNames),
			len(names),
		)
	}

	seen := make(map[string]string)

	for _, name := range names {
		shortName := shortNames[name]

		if previous, exists := seen[shortName]; exists {
			t.Fatalf(
				"short name %q is shared by %q and %q",
				shortName,
				previous,
				name,
			)
		}

		seen[shortName] = name
	}

	for i, name := range names {
		got, err := fs.ReadFile(name)
		if err != nil {
			t.Fatalf(
				"ReadFile(%q): %v",
				name,
				err,
			)
		}

		want := []byte(fmt.Sprintf("content-%d", i))

		if !bytes.Equal(got, want) {
			t.Fatalf(
				"ReadFile(%q) = %q, want %q",
				name,
				got,
				want,
			)
		}
	}
}

// Reuses a generated short name when another long filename has the same prefix.
// A deleted generated 8.3 alias becomes available again,
// provided the new filename would generate the same alias.
func TestLongFilenameShortNameReuseLifecycle(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 2
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 160

		rootCluster = uint32(2)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize) * 2

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

	firstName := "Reusable Collision Name.txt"
	secondName := "Reusable Collision Again.txt"

	if err := fs.WriteFile(
		firstName,
		[]byte("first"),
	); err != nil {
		t.Fatal(err)
	}

	records, err := fs.readDirectoryRecords(rootCluster)
	if err != nil {
		t.Fatal(err)
	}

	var firstShortName string

	for _, record := range records {
		if strings.EqualFold(record.Entry.Name, firstName) {
			firstShortName = record.Entry.ShortName
			break
		}
	}

	if firstShortName == "" {
		t.Fatal("could not find first long filename")
	}

	if err := fs.Delete(firstName); err != nil {
		t.Fatal(err)
	}

	if err := fs.WriteFile(
		secondName,
		[]byte("second"),
	); err != nil {
		t.Fatal(err)
	}

	records, err = fs.readDirectoryRecords(rootCluster)
	if err != nil {
		t.Fatal(err)
	}

	var secondShortName string

	for _, record := range records {
		if strings.EqualFold(record.Entry.Name, secondName) {
			secondShortName = record.Entry.ShortName
			break
		}
	}

	if secondShortName == "" {
		t.Fatal("could not find second long filename")
	}

	if secondShortName != firstShortName {
		t.Fatalf(
			"reused short name = %q, want %q",
			secondShortName,
			firstShortName,
		)
	}

	got, err := fs.ReadFile(secondName)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("second")) {
		t.Fatalf(
			"second file content = %q, want %q",
			got,
			"second",
		)
	}
}

// Rejects creating a file whose name differs from an existing file only by case.
func TestCaseInsensitiveFileCollision(t *testing.T) {
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
		"Song.mp3",
		[]byte("original"),
	); err != nil {
		t.Fatal(err)
	}

	if err := fs.WriteFile(
		"song.mp3",
		[]byte("duplicate"),
	); err == nil {
		t.Fatal("case-insensitive duplicate filename should be rejected")
	}

	got, err := fs.ReadFile("SONG.MP3")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("original")) {
		t.Fatalf(
			"original file content = %q, want %q",
			got,
			"original",
		)
	}
}

// Rejects creating a directory whose name differs from an existing directory only by case.
func TestCaseInsensitiveDirectoryCollision(t *testing.T) {
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

	if err := fs.Mkdir("music"); err == nil {
		t.Fatal("case-insensitive duplicate directory should be rejected")
	}

	if err := fs.WriteFile(
		"MUSIC/song.mp3",
		[]byte("song"),
	); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile("music/SONG.MP3")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("song")) {
		t.Fatalf(
			"file inside case-insensitive directory = %q, want %q",
			got,
			"song",
		)
	}
}

// Rejects creating a file where a directory already exists and preserves the directory.
func TestFileDirectoryTypeConflict(t *testing.T) {
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

	if err := fs.Mkdir("documents"); err != nil {
		t.Fatal(err)
	}

	if err := fs.WriteFile(
		"documents",
		[]byte("not a directory"),
	); err == nil {
		t.Fatal("WriteFile should reject an existing directory")
	}

	if err := fs.WriteFile(
		"documents/report.txt",
		[]byte("report"),
	); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile("documents/report.txt")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("report")) {
		t.Fatalf(
			"report content = %q, want %q",
			got,
			"report",
		)
	}
}

// Rejects creating a directory where a file already exists and preserves the file.
func TestDirectoryFileTypeConflict(t *testing.T) {
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
		"documents",
		[]byte("document"),
	); err != nil {
		t.Fatal(err)
	}

	if err := fs.Mkdir("documents"); err == nil {
		t.Fatal("Mkdir should reject an existing file")
	}

	got, err := fs.ReadFile("documents")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("document")) {
		t.Fatalf(
			"file content = %q, want %q",
			got,
			"document",
		)
	}
}

// Uses "." and ".." through public file operations without escaping the root.
func TestPublicPathTraversalLifecycle(t *testing.T) {
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

	if err := fs.Mkdir("music"); err != nil {
		t.Fatal(err)
	}

	if err := fs.Mkdir("music/albums"); err != nil {
		t.Fatal(err)
	}

	if err := fs.WriteFile(
		"music/albums/song.mp3",
		[]byte("song"),
	); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile(
		"music/albums/../albums/./song.mp3",
	)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("song")) {
		t.Fatalf(
			"path traversal content = %q, want %q",
			got,
			"song",
		)
	}

	got, err = fs.ReadFile(
		"music/../music/albums/song.mp3",
	)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("song")) {
		t.Fatalf(
			"parent traversal content = %q, want %q",
			got,
			"song",
		)
	}

	if err := fs.WriteFile(
		"../root-song.mp3",
		[]byte("root song"),
	); err != nil {
		t.Fatal(err)
	}

	got, err = fs.ReadFile("root-song.mp3")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("root song")) {
		t.Fatalf(
			"root traversal content = %q, want %q",
			got,
			"root song",
		)
	}
}

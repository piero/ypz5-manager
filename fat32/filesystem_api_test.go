package fat32

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"
)

// A read-only filesystem supports the complete public file-reading API.
func TestReadOnlySupportsFileReads(t *testing.T) {
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

	entry := encodeDirectoryEntry(
		[11]byte{'T', 'E', 'S', 'T', ' ', ' ', ' ', ' ', 'T', 'X', 'T'},
		0x20,
		fileCluster,
		4,
	)

	copy(
		data[rootOffset:rootOffset+32],
		entry[:],
	)

	data[rootOffset+32] = 0x00

	copy(
		data[int(dataOffset)+int(fileCluster-rootCluster)*int(sectorSize):],
		[]byte("test"),
	)

	fs := New(bytes.NewReader(data))

	fs.boot = boot
	fs.layout = layout

	// ReadFile returns the complete file contents.
	got, err := fs.ReadFile("test.txt")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("test")) {
		t.Fatalf(
			"ReadFile returned %q, want %q",
			got,
			"test",
		)
	}

	// OpenFile returns the file metadata.
	file, err := fs.OpenFile("test.txt")
	if err != nil {
		t.Fatal(err)
	}

	if file.Name() != "TEST.TXT" {
		t.Fatalf(
			"file name = %q, want %q",
			file.Name(),
			"TEST.TXT",
		)
	}

	if file.Size() != 4 {
		t.Fatalf(
			"file size = %d, want %d",
			file.Size(),
			4,
		)
	}

	// File.ReadAll returns the same contents as ReadFile.
	got, err = file.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("test")) {
		t.Fatalf(
			"ReadAll returned %q, want %q",
			got,
			"test",
		)
	}
}

// Public cluster APIs report FAT allocation and chain state consistently.
func TestClusterInspectionAPI(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 160

		rootCluster = uint32(2)
		cluster3    = uint32(3)
		cluster4    = uint32(4)
		cluster5    = uint32(5)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	// Root cluster is allocated and terminates.
	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	// Cluster 3 -> 4 -> 5 -> EOF.
	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(cluster3)*4:],
		cluster4,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(cluster4)*4:],
		cluster5,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(cluster5)*4:],
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

	fs := New(bytes.NewReader(data))

	fs.boot = boot
	fs.layout = layout

	if got := fs.RootCluster(); got != rootCluster {
		t.Fatalf(
			"RootCluster() = %d, want %d",
			got,
			rootCluster,
		)
	}

	if free, err := fs.IsFreeCluster(rootCluster); err != nil {
		t.Fatal(err)
	} else if free {
		t.Fatal("root cluster should be allocated")
	}

	if free, err := fs.IsFreeCluster(cluster3); err != nil {
		t.Fatal(err)
	} else if free {
		t.Fatal("cluster 3 should be allocated")
	}

	if free, err := fs.IsFreeCluster(10); err != nil {
		t.Fatal(err)
	} else if !free {
		t.Fatal("cluster 10 should be free")
	}

	cluster, err := fs.FindFreeCluster(3)
	if err != nil {
		t.Fatal(err)
	}

	if cluster != 6 {
		t.Fatalf(
			"FindFreeCluster(3) = %d, want %d",
			cluster,
			6,
		)
	}

	chain, err := fs.ClusterChain(cluster3)
	if err != nil {
		t.Fatal(err)
	}

	want := []uint32{
		cluster3,
		cluster4,
		cluster5,
	}

	if !reflect.DeepEqual(chain, want) {
		t.Fatalf(
			"ClusterChain(3) = %v, want %v",
			chain,
			want,
		)
	}
}

// Public file creation allocates a cluster chain and deletion releases it.
func TestPublicAllocationLifecycle(t *testing.T) {
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

	writer := writerAtBuffer{
		data: data,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		&writer,
	)

	fs.boot = boot
	fs.layout = layout

	fileData := bytes.Repeat(
		[]byte("x"),
		int(sectorSize)*3,
	)

	if err := fs.WriteFile("TEST.BIN", fileData); err != nil {
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
		t.Fatal("TEST.BIN was not created")
	}

	if record.Entry.StartCluster < 2 {
		t.Fatalf(
			"file start cluster = %d, want >= 2",
			record.Entry.StartCluster,
		)
	}

	chain, err := fs.ClusterChain(record.Entry.StartCluster)
	if err != nil {
		t.Fatal(err)
	}

	if len(chain) != 3 {
		t.Fatalf(
			"file cluster chain length = %d, want %d",
			len(chain),
			3,
		)
	}

	for _, cluster := range chain {
		free, err := fs.IsFreeCluster(cluster)
		if err != nil {
			t.Fatal(err)
		}

		if free {
			t.Fatalf(
				"file cluster %d should be allocated",
				cluster,
			)
		}
	}

	if err := fs.Delete("TEST.BIN"); err != nil {
		t.Fatal(err)
	}

	for _, cluster := range chain {
		free, err := fs.IsFreeCluster(cluster)
		if err != nil {
			t.Fatal(err)
		}

		if !free {
			t.Fatalf(
				"deleted file cluster %d should be free",
				cluster,
			)
		}
	}
}

func newAPITestFilesystem() ([]byte, *Filesystem, *writerAtBuffer) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 160

		rootCluster = uint32(2)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	binary.LittleEndian.PutUint32(
		data[int(rootCluster)*4:],
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
		FATOffset:       0,
		DataOffset:      int64(sectorSize),
		BytesPerCluster: int64(sectorSize),
	}

	writer := &writerAtBuffer{
		data: data,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		writer,
	)

	fs.boot = boot
	fs.layout = layout

	return data, fs, writer
}

// WriteFile creates a readable file through the public API.
func TestWriteFileCreatesReadableFile(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	want := []byte("hello from WriteFile")

	if err := fs.WriteFile("HELLO.TXT", want); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile("HELLO.TXT")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, want) {
		t.Fatalf(
			"ReadFile returned %q, want %q",
			got,
			want,
		)
	}
}

// WriteFile rejects replacing an existing file and preserves its contents.
func TestWriteFileRejectsExistingFile(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	original := []byte("original")

	if err := fs.WriteFile("TEST.TXT", original); err != nil {
		t.Fatal(err)
	}

	if err := fs.WriteFile(
		"TEST.TXT",
		[]byte("replacement"),
	); err == nil {
		t.Fatal("WriteFile should reject an existing file")
	}

	got, err := fs.ReadFile("TEST.TXT")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, original) {
		t.Fatalf(
			"existing file changed after rejected WriteFile: got %q, want %q",
			got,
			original,
		)
	}
}

// WriteFile rejects using an existing directory name as a file.
func TestWriteFileRejectsExistingDirectory(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	if err := fs.Mkdir("documents"); err != nil {
		t.Fatal(err)
	}

	if err := fs.WriteFile(
		"documents",
		[]byte("not a directory"),
	); err == nil {
		t.Fatal("WriteFile should reject an existing directory")
	}

	if _, err := fs.OpenFile("documents"); err == nil {
		t.Fatal("documents should remain a directory")
	}
}

// ReplaceFile replaces the contents of an existing file.
func TestReplaceFileUpdatesContents(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	original := []byte("original contents")
	replacement := []byte("replacement contents")

	if err := fs.WriteFile("TEST.TXT", original); err != nil {
		t.Fatal(err)
	}

	if err := fs.ReplaceFile("TEST.TXT", replacement); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile("TEST.TXT")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, replacement) {
		t.Fatalf(
			"ReadFile returned %q, want %q",
			got,
			replacement,
		)
	}

	file, err := fs.OpenFile("TEST.TXT")
	if err != nil {
		t.Fatal(err)
	}

	if file.Size() != uint32(len(replacement)) {
		t.Fatalf(
			"file size = %d, want %d",
			file.Size(),
			len(replacement),
		)
	}
}

// ReplaceFile can replace a file with data requiring additional clusters.
func TestReplaceFileWithLargerData(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	original := []byte("small")

	replacement := bytes.Repeat(
		[]byte("x"),
		1025,
	)

	if err := fs.WriteFile("TEST.BIN", original); err != nil {
		t.Fatal(err)
	}

	if err := fs.ReplaceFile("TEST.BIN", replacement); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile("TEST.BIN")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, replacement) {
		t.Fatalf(
			"ReadFile returned %d bytes, want %d",
			len(got),
			len(replacement),
		)
	}

	file, err := fs.OpenFile("TEST.BIN")
	if err != nil {
		t.Fatal(err)
	}

	if file.Size() != uint32(len(replacement)) {
		t.Fatalf(
			"file size = %d, want %d",
			file.Size(),
			len(replacement),
		)
	}
}

// ReplaceFile can replace a non-empty file with an empty file.
func TestReplaceFileWithEmptyData(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	if err := fs.WriteFile(
		"EMPTY.TXT",
		[]byte("some contents"),
	); err != nil {
		t.Fatal(err)
	}

	if err := fs.ReplaceFile(
		"EMPTY.TXT",
		[]byte{},
	); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile("EMPTY.TXT")
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 0 {
		t.Fatalf(
			"ReadFile returned %d bytes, want 0",
			len(got),
		)
	}

	file, err := fs.OpenFile("EMPTY.TXT")
	if err != nil {
		t.Fatal(err)
	}

	if file.Size() != 0 {
		t.Fatalf(
			"file size = %d, want 0",
			file.Size(),
		)
	}
}

// ReplaceFile rejects a path that does not identify an existing file.
func TestReplaceFileRejectsMissingFile(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	err := fs.ReplaceFile(
		"missing.txt",
		[]byte("replacement"),
	)

	if err == nil {
		t.Fatal("ReplaceFile should reject a missing file")
	}
}

// ReplaceFile rejects replacing a directory.
func TestReplaceFileRejectsDirectory(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	if err := fs.Mkdir("documents"); err != nil {
		t.Fatal(err)
	}

	err := fs.ReplaceFile(
		"documents",
		[]byte("replacement"),
	)

	if err == nil {
		t.Fatal("ReplaceFile should reject a directory")
	}
}

// ReplaceFile rejects a path with a trailing separator.
func TestReplaceFileRejectsTrailingSeparator(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	if err := fs.WriteFile(
		"TEST.TXT",
		[]byte("original"),
	); err != nil {
		t.Fatal(err)
	}

	err := fs.ReplaceFile(
		"TEST.TXT/",
		[]byte("replacement"),
	)

	if err == nil {
		t.Fatal("ReplaceFile should reject a trailing separator")
	}

	got, err := fs.ReadFile("TEST.TXT")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("original")) {
		t.Fatalf(
			"file changed after rejected replacement: got %q",
			got,
		)
	}
}

// Delete removes an existing file from its directory.
func TestDeleteRemovesFile(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	if err := fs.WriteFile(
		"TEST.BIN",
		[]byte("contents"),
	); err != nil {
		t.Fatal(err)
	}

	if err := fs.Delete("TEST.BIN"); err != nil {
		t.Fatal(err)
	}

	if _, err := fs.OpenFile("TEST.BIN"); err == nil {
		t.Fatal("deleted file should not be openable")
	}
}

// Delete releases all clusters belonging to the deleted file.
func TestDeleteReleasesFileClusters(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	if err := fs.WriteFile(
		"TEST.BIN",
		bytes.Repeat([]byte("x"), 1025),
	); err != nil {
		t.Fatal(err)
	}

	record, err := fs.findDirectoryEntry(
		fs.RootCluster(),
		"TEST.BIN",
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("TEST.BIN was not created")
	}

	chain, err := fs.ClusterChain(record.Entry.StartCluster)
	if err != nil {
		t.Fatal(err)
	}

	if err := fs.Delete("TEST.BIN"); err != nil {
		t.Fatal(err)
	}

	for _, cluster := range chain {
		free, err := fs.IsFreeCluster(cluster)
		if err != nil {
			t.Fatal(err)
		}

		if !free {
			t.Fatalf(
				"cluster %d should be free after deletion",
				cluster,
			)
		}
	}
}

// Delete rejects a path that does not identify an existing entry.
func TestDeleteRejectsMissingFile(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	if err := fs.Delete("missing.txt"); err == nil {
		t.Fatal("Delete should reject a missing file")
	}
}

// Delete rejects directories without removing them.
func TestDeleteRejectsDirectory(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	if err := fs.Mkdir("documents"); err != nil {
		t.Fatal(err)
	}

	if err := fs.Delete("documents"); err == nil {
		t.Fatal("Delete should reject a directory")
	}

	file, err := fs.OpenFile("documents")
	if err == nil {
		t.Fatal("OpenFile should reject a directory as a file")
	}

	_ = file

	if err := fs.WriteFile(
		"documents/TEST.TXT",
		[]byte("still here"),
	); err != nil {
		t.Fatalf(
			"directory was damaged by rejected deletion: %v",
			err,
		)
	}
}

// Mkdir creates a directory that can subsequently contain files.
func TestMkdirCreatesDirectory(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	if err := fs.Mkdir("documents"); err != nil {
		t.Fatal(err)
	}

	if err := fs.WriteFile(
		"documents/TEST.TXT",
		[]byte("contents"),
	); err != nil {
		t.Fatalf(
			"created directory cannot contain a file: %v",
			err,
		)
	}

	got, err := fs.ReadFile("documents/TEST.TXT")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("contents")) {
		t.Fatalf(
			"ReadFile returned %q, want %q",
			got,
			"contents",
		)
	}
}

// Mkdir creates nested directories through the public path API.
func TestMkdirCreatesNestedDirectory(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	if err := fs.Mkdir("documents"); err != nil {
		t.Fatal(err)
	}

	if err := fs.Mkdir("documents/music"); err != nil {
		t.Fatal(err)
	}

	if err := fs.WriteFile(
		"documents/music/SONG.TXT",
		[]byte("song"),
	); err != nil {
		t.Fatalf(
			"nested directory cannot contain a file: %v",
			err,
		)
	}

	got, err := fs.ReadFile("documents/music/SONG.TXT")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("song")) {
		t.Fatalf(
			"ReadFile returned %q, want %q",
			got,
			"song",
		)
	}
}

// Mkdir rejects creating a directory with an existing name.
func TestMkdirRejectsExistingEntry(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	if err := fs.Mkdir("documents"); err != nil {
		t.Fatal(err)
	}

	if err := fs.Mkdir("documents"); err == nil {
		t.Fatal("Mkdir should reject an existing directory")
	}
}

// Mkdir rejects creating a directory where a file already exists.
func TestMkdirRejectsExistingFile(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	if err := fs.WriteFile(
		"documents",
		[]byte("file"),
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

	if !bytes.Equal(got, []byte("file")) {
		t.Fatalf(
			"existing file changed after rejected Mkdir: got %q",
			got,
		)
	}
}

// Public APIs accept absolute paths for files and directories.
func TestPublicAPIAcceptsAbsolutePaths(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	if err := fs.Mkdir("/documents"); err != nil {
		t.Fatal(err)
	}

	if err := fs.WriteFile(
		"/documents/TEST.TXT",
		[]byte("absolute"),
	); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile("/documents/TEST.TXT")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("absolute")) {
		t.Fatalf(
			"ReadFile returned %q, want %q",
			got,
			"absolute",
		)
	}
}

// Public APIs support parent traversal through existing directories.
func TestPublicAPISupportsParentTraversal(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	if err := fs.Mkdir("documents"); err != nil {
		t.Fatal(err)
	}

	if err := fs.Mkdir("documents/music"); err != nil {
		t.Fatal(err)
	}

	if err := fs.WriteFile(
		"documents/music/SONG.TXT",
		[]byte("song"),
	); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile(
		"documents/music/../music/SONG.TXT",
	)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, []byte("song")) {
		t.Fatalf(
			"ReadFile returned %q, want %q",
			got,
			"song",
		)
	}
}

// Public write APIs reject empty path components.
func TestPublicAPIsRejectEmptyPathComponents(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	if err := fs.WriteFile(
		"documents//TEST.TXT",
		[]byte("invalid"),
	); err == nil {
		t.Fatal("WriteFile should reject an empty path component")
	}

	if err := fs.ReplaceFile(
		"documents//TEST.TXT",
		[]byte("invalid"),
	); err == nil {
		t.Fatal("ReplaceFile should reject an empty path component")
	}

	if err := fs.Mkdir(
		"documents//subdir",
	); err == nil {
		t.Fatal("Mkdir should reject an empty path component")
	}
}

// Public write APIs reject paths ending in a separator.
func TestPublicAPIsRejectTrailingSeparators(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	if err := fs.WriteFile(
		"TEST.TXT/",
		[]byte("invalid"),
	); err == nil {
		t.Fatal("WriteFile should reject a trailing separator")
	}

	if err := fs.ReplaceFile(
		"TEST.TXT/",
		[]byte("invalid"),
	); err == nil {
		t.Fatal("ReplaceFile should reject a trailing separator")
	}

	if err := fs.Mkdir("documents/"); err == nil {
		t.Fatal("Mkdir should reject a trailing separator")
	}
}

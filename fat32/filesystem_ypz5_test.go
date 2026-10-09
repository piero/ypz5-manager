package fat32

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The YP-Z5 integration tests run against any FAT32 image named by
// YP_Z5_IMAGE: a whole disk with an MBR or a bare partition. They create
// the files and directories they need and only assume that the image is
// a consistent FAT32 filesystem, so other images work as well as the
// YP-Z5 dump they were first written for.

// imagePartition returns where the filesystem lies in the image file.
func imagePartition(t *testing.T, file *os.File) (offset, size int64) {
	t.Helper()

	offset, err := PartitionOffset(file)
	if err != nil {
		t.Fatalf("find FAT32 filesystem in image: %v", err)
	}

	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}

	return offset, info.Size() - offset
}

// testDir creates a directory at the root for a test's files.
func testDir(t *testing.T, fs *Filesystem, name string) string {
	t.Helper()

	path := "/" + name

	if err := fs.Mkdir(path); err != nil {
		t.Fatalf("Mkdir(%q): %v", path, err)
	}

	return path
}

// walkImage calls visit for every entry on the image, depth first.
func walkImage(t *testing.T, fs *Filesystem, visit func(path string, entry DirectoryEntry)) {
	t.Helper()

	var walk func(dir string)

	walk = func(dir string) {
		entries, err := fs.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir(%q): %v", dir, err)
		}

		for _, entry := range entries {
			path := strings.TrimSuffix(dir, "/") + "/" + entry.Name

			visit(path, entry)

			if entry.IsDir {
				walk(path)
			}
		}
	}

	walk("/")
}

// existingFiles returns up to limit regular, unprotected, non-empty files
// already on the image, skipping the test when there are none.
func existingFiles(t *testing.T, fs *Filesystem, limit int) []string {
	t.Helper()

	var paths []string

	walkImage(t, fs, func(path string, entry DirectoryEntry) {
		if len(paths) < limit && !entry.IsDir && !entry.Protected() && entry.Size > 0 {
			paths = append(paths, path)
		}
	})

	if len(paths) == 0 {
		t.Skip("the image contains no regular files")
	}

	return paths
}

type partitionWriter struct {
	file   *os.File
	offset int64
	size   int64
}

func (w *partitionWriter) WriteAt(
	p []byte,
	off int64,
) (int, error) {
	if off < 0 || off+int64(len(p)) > w.size {
		return 0, io.ErrShortBuffer
	}

	return w.file.WriteAt(p, w.offset+off)
}

// Returns the pristine YP-Z5 image path or skips the test when unavailable.
func ypz5ImagePath(t *testing.T) string {
	t.Helper()

	path := os.Getenv("YP_Z5_IMAGE")

	if path == "" {
		t.Skip("YP-Z5 integration test requires YP_Z5_IMAGE")
	}

	return path
}

// Opens the backed-up YP-Z5 FAT32 partition as a read-only filesystem.
func openYPZ5Image(t *testing.T) (*os.File, *Filesystem) {
	t.Helper()

	path := ypz5ImagePath(t)

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open YP-Z5 image %q: %v", path, err)
	}

	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Errorf("close YP-Z5 image: %v", err)
		}
	})

	offset, size := imagePartition(t, file)
	partition := io.NewSectionReader(file, offset, size)

	fs := New(partition)

	if err := fs.Open(); err != nil {
		t.Fatalf("open filesystem: %v", err)
	}

	return file, fs
}

// Opens a fresh writable copy of the pristine YP-Z5 image for each test.
//
// Thanks to this, each mutating test can use their own image:
//
// YP_Z5_IMAGE
// │
// ├── Test A → temporary image A
// ├── Test B → temporary image B
// ├── Test C → temporary image C
// └── ...
//
// so we can run tests like this:
//
// YP_Z5_IMAGE="/Users/piero/Desktop/YP-Z5 image/yp-z5a-full.img" \
// go test ./fat32
// Opens a fresh writable clone of the pristine YP-Z5 image for each test.
// Opens a fresh writable copy of the pristine YP-Z5 image for each test.
func openYPZ5WriteImage(t *testing.T) (*os.File, *Filesystem) {
	t.Helper()

	_, file, fs := openYPZ5WriteImageWithPath(t)

	return file, fs
}

// Opens a fresh writable copy and returns its path for independent validation.
func openYPZ5WriteImageWithPath(t *testing.T) (string, *os.File, *Filesystem) {
	t.Helper()

	sourcePath := ypz5ImagePath(t)

	testPath := filepath.Join(
		t.TempDir(),
		"yp-z5-write-test.img",
	)

	cloneYPZ5Image(t, sourcePath, testPath)

	destination, err := os.OpenFile(
		testPath,
		os.O_RDWR,
		0,
	)
	if err != nil {
		t.Fatalf("open test image: %v", err)
	}

	offset, size := imagePartition(t, destination)
	partition := io.NewSectionReader(destination, offset, size)

	writer := &partitionWriter{
		file:   destination,
		offset: offset,
		size:   size,
	}

	fs := NewReadWrite(partition, writer)

	if err := fs.Open(); err != nil {
		destination.Close()
		t.Fatalf("open filesystem: %v", err)
	}

	t.Cleanup(func() {
		_ = destination.Close()
	})

	// The consistency check after the test is only meaningful if the
	// image was consistent to begin with.
	if err := consistencyError(fs, false); err != nil {
		t.Skipf("test image is not consistent; repair it first (e.g. with fsck_msdos): %v", err)
	}

	// Every mutating test must leave a consistent filesystem. Cleanups
	// run in reverse order, so this runs before the image is closed.
	t.Cleanup(func() {
		if t.Failed() {
			return
		}

		// Some tests close the image themselves to reopen it.
		if _, err := destination.Stat(); errors.Is(err, os.ErrClosed) {
			return
		}

		checkConsistency(t, fs)
	})

	return testPath, destination, fs
}

// Clones the source image on macOS/APFS, falling back to a regular copy elsewhere.
func cloneYPZ5Image(t *testing.T, sourcePath, destinationPath string) {
	t.Helper()

	if runtime.GOOS == "darwin" {
		cmd := exec.Command(
			"cp",
			"-c",
			sourcePath,
			destinationPath,
		)

		if output, err := cmd.CombinedOutput(); err == nil {
			// The clone keeps the source's mode, and the pristine
			// image is expected to be read-only.
			if err := os.Chmod(destinationPath, 0600); err != nil {
				t.Fatalf("make cloned image writable: %v", err)
			}

			return
		} else {
			t.Logf(
				"APFS clone failed, falling back to regular copy: %v: %s",
				err,
				output,
			)
		}
	}

	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatalf("open source image: %v", err)
	}
	defer source.Close()

	destination, err := os.OpenFile(
		destinationPath,
		os.O_CREATE|os.O_EXCL|os.O_RDWR,
		0600,
	)
	if err != nil {
		t.Fatalf("create test image: %v", err)
	}

	if _, err := io.Copy(destination, source); err != nil {
		destination.Close()
		t.Fatalf("copy source image: %v", err)
	}

	if err := destination.Close(); err != nil {
		t.Fatalf("close copied image: %v", err)
	}
}

// Verifies that the boot sector describes a valid, self-consistent
// FAT32 volume and that the root directory can be read.
func TestYPZ5Filesystem(t *testing.T) {
	_, fs := openYPZ5Image(t)

	boot := fs.BootSector()

	if err := boot.Validate(); err != nil {
		t.Fatal(err)
	}

	switch boot.BytesPerSector {
	case 512, 1024, 2048, 4096:
	default:
		t.Fatalf("BytesPerSector = %d, want 512, 1024, 2048 or 4096", boot.BytesPerSector)
	}

	if spc := boot.SectorsPerCluster; spc&(spc-1) != 0 {
		t.Fatalf("SectorsPerCluster = %d, want a power of two", spc)
	}

	// FAT32 volumes have at least 65525 clusters, and each FAT copy
	// must have an entry for every one of them.
	if clusters := boot.ClusterCount(); clusters < 65525 {
		t.Fatalf("ClusterCount = %d, too few for FAT32", clusters)
	}

	fatEntries := int64(boot.SectorsPerFAT) * int64(boot.BytesPerSector) / 4

	if int64(boot.MaxCluster()) >= fatEntries {
		t.Fatalf("FAT has %d entries, volume needs %d", fatEntries, boot.MaxCluster()+1)
	}

	if _, err := fs.ReadDir("/"); err != nil {
		t.Fatalf("ReadDir(/): %v", err)
	}
}

// Verifies that the image passes the same consistency check that every
// mutating test runs afterwards.
func TestYPZ5ImageIsConsistent(t *testing.T) {
	_, fs := openYPZ5Image(t)

	if err := consistencyError(fs, false); err != nil {
		t.Fatalf("image is not consistent; repair it first (e.g. with fsck_msdos): %v", err)
	}
}

// Verifies that every directory on the image can be read, and that each
// entry resolves back to itself by path.
func TestYPZ5ReadsEveryDirectory(t *testing.T) {
	_, fs := openYPZ5Image(t)

	count := 0

	walkImage(t, fs, func(path string, entry DirectoryEntry) {
		count++

		got, err := fs.Lookup(path)
		if err != nil {
			t.Fatalf("Lookup(%q): %v", path, err)
		}

		if got.StartCluster != entry.StartCluster || got.IsDir != entry.IsDir {
			t.Fatalf("Lookup(%q) = %+v, listed as %+v", path, got, entry)
		}
	})

	t.Logf("checked %d entries", count)
}

// Verifies that existing files read back with the length and cluster
// chain their directory entries promise, and, when mtools is installed,
// with the same contents mtools reads.
func TestYPZ5ReadsExistingFiles(t *testing.T) {
	imageFile, fs := openYPZ5Image(t)

	paths := existingFiles(t, fs, 20)

	for _, path := range paths {
		entry, err := fs.Lookup(path)
		if err != nil {
			t.Fatal(err)
		}

		data, err := fs.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q): %v", path, err)
		}

		if uint32(len(data)) != entry.Size {
			t.Fatalf("ReadFile(%q) returned %d bytes, want %d", path, len(data), entry.Size)
		}

		chain, err := fs.ClusterChain(entry.StartCluster)
		if err != nil {
			t.Fatalf("ClusterChain(%q): %v", path, err)
		}

		if want := fs.clustersForSize(int(entry.Size)); len(chain) != want {
			t.Fatalf("%q: chain has %d clusters, size needs %d", path, len(chain), want)
		}
	}

	if _, err := exec.LookPath("mtype"); err != nil {
		t.Logf("mtools not installed, contents not cross-checked: %v", err)
		return
	}

	offset, _ := imagePartition(t, imageFile)
	imageSpec := imageFile.Name() + "@@" + fmt.Sprint(offset)

	for _, path := range paths[:min(len(paths), 5)] {
		want, err := exec.Command("mtype", "-i", imageSpec, "::"+path).Output()
		if err != nil {
			t.Fatalf("mtype %q: %v", path, err)
		}

		got, err := fs.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(got, want) {
			t.Fatalf("%q: contents differ from mtools", path)
		}
	}
}

// Creates, reads, deletes, and recreates a file in an existing YP-Z5 directory.
func TestYPZ5WriteFileLifecycle(t *testing.T) {
	_, fs := openYPZ5WriteImage(t)

	path := testDir(t, fs, "YPZ5 Tests") + "/YPZ5 test file.txt"

	content := []byte("YP-Z5 FAT32 write test")

	if err := fs.WriteFile(path, content); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := fs.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if !bytes.Equal(got, content) {
		t.Fatalf("ReadFile = %q, want %q", got, content)
	}

	if err := fs.Delete(path); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := fs.ReadFile(path); err == nil {
		t.Fatal("ReadFile succeeded after Delete")
	}

	if err := fs.WriteFile(path, content); err != nil {
		t.Fatalf("WriteFile after Delete: %v", err)
	}

	got, err = fs.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after recreation: %v", err)
	}

	if !bytes.Equal(got, content) {
		t.Fatalf(
			"ReadFile after recreation = %q, want %q",
			got,
			content,
		)
	}
}

// Creates and reads a long Unicode filename on the YP-Z5 filesystem.
func TestYPZ5UnicodeLongFilename(t *testing.T) {
	_, fs := openYPZ5WriteImage(t)

	path := testDir(t, fs, "YPZ5 Tests") + "/Prova Unicode 🚀 日本語.txt"

	content := []byte("unicode filename test")

	if err := fs.WriteFile(path, content); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entry, err := fs.Lookup(path)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	if entry.Name != "Prova Unicode 🚀 日本語.txt" {
		t.Fatalf(
			"entry name = %q, want %q",
			entry.Name,
			"Prova Unicode 🚀 日本語.txt",
		)
	}

	if entry.IsDir {
		t.Fatal("Unicode test file is unexpectedly a directory")
	}

	got, err := fs.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if !bytes.Equal(got, content) {
		t.Fatalf("ReadFile = %q, want %q", got, content)
	}
}

// Replaces a file already on the image and verifies that its old data
// clusters are released.
func TestYPZ5ReplaceFile(t *testing.T) {
	_, fs := openYPZ5WriteImage(t)

	path := existingFiles(t, fs, 1)[0]

	before, err := fs.Lookup(path)
	if err != nil {
		t.Fatalf("Lookup before replacement: %v", err)
	}

	if before.IsDir {
		t.Fatalf("%q is unexpectedly a directory", path)
	}

	oldChain, err := fs.ClusterChain(before.StartCluster)
	if err != nil {
		t.Fatalf("ClusterChain before replacement: %v", err)
	}

	content := []byte("YP-Z5 replacement test\n")

	if err := fs.ReplaceFile(path, content); err != nil {
		t.Fatalf("ReplaceFile: %v", err)
	}

	got, err := fs.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after replacement: %v", err)
	}

	if !bytes.Equal(got, content) {
		t.Fatalf(
			"ReadFile after replacement = %q, want %q",
			got,
			content,
		)
	}

	after, err := fs.Lookup(path)
	if err != nil {
		t.Fatalf("Lookup after replacement: %v", err)
	}

	for _, cluster := range oldChain {
		free, err := fs.IsFreeCluster(cluster)
		if err != nil {
			t.Fatalf(
				"IsFreeCluster(%d): %v",
				cluster,
				err,
			)
		}

		if !free {
			t.Fatalf(
				"old cluster %d is still allocated",
				cluster,
			)
		}
	}

	if after.StartCluster != 0 {
		newChain, err := fs.ClusterChain(after.StartCluster)
		if err != nil {
			t.Fatalf("ClusterChain after replacement: %v", err)
		}

		if len(newChain) == 0 {
			t.Fatal("replacement file has an empty cluster chain")
		}
	}
}

// Creates a directory and verifies that it can contain a nested file.
func TestYPZ5DirectoryLifecycle(t *testing.T) {
	_, fs := openYPZ5WriteImage(t)

	const directory = "/YPZ5 Test Directory"

	if err := fs.Mkdir(directory); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	entry, err := fs.Lookup(directory)
	if err != nil {
		t.Fatalf("Lookup directory: %v", err)
	}

	if !entry.IsDir {
		t.Fatal("created entry is not a directory")
	}

	entries, err := fs.ReadDir(directory)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf(
			"new directory contains %d visible entries, want 0",
			len(entries),
		)
	}

	const file = directory + "/test.txt"

	content := []byte("nested YP-Z5 test")

	if err := fs.WriteFile(file, content); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := fs.ReadFile(file)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if !bytes.Equal(got, content) {
		t.Fatalf("ReadFile = %q, want %q", got, content)
	}
}

// Creates enough files to force a real directory to expand across clusters.
func TestYPZ5DirectoryExpansion(t *testing.T) {
	_, fs := openYPZ5WriteImage(t)

	const directory = "/YPZ5 Expansion Test"

	if err := fs.Mkdir(directory); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	const fileCount = 200

	content := []byte("directory expansion test")

	for i := 0; i < fileCount; i++ {
		path := fmt.Sprintf(
			"%s/new-%03d.txt",
			directory,
			i,
		)

		if err := fs.WriteFile(path, content); err != nil {
			t.Fatalf("WriteFile(%q): %v", path, err)
		}
	}

	entries, err := fs.ReadDir(directory)
	if err != nil {
		t.Fatalf("ReadDir after expansion: %v", err)
	}

	if len(entries) != fileCount {
		t.Fatalf(
			"directory contains %d entries, want %d",
			len(entries),
			fileCount,
		)
	}

	for _, index := range []int{0, 99, 199} {
		path := fmt.Sprintf(
			"%s/new-%03d.txt",
			directory,
			index,
		)

		got, err := fs.ReadFile(path)
		if err != nil {
			t.Fatalf(
				"ReadFile(%q): %v",
				path,
				err,
			)
		}

		if !bytes.Equal(got, content) {
			t.Fatalf(
				"ReadFile(%q) = %q, want %q",
				path,
				got,
				content,
			)
		}
	}
}

// Verifies that directory expansion creates a longer physical cluster chain.
func TestYPZ5DirectoryExpansionChain(t *testing.T) {
	_, fs := openYPZ5WriteImage(t)

	const directory = "/YPZ5 Expansion Chain"

	if err := fs.Mkdir(directory); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	const fileCount = 200
	content := []byte("directory chain test")

	for i := 0; i < fileCount; i++ {
		path := fmt.Sprintf(
			"%s/file-%03d.txt",
			directory,
			i,
		)

		if err := fs.WriteFile(path, content); err != nil {
			t.Fatalf("WriteFile(%q): %v", path, err)
		}
	}

	entry, err := fs.Lookup(directory)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	chain, err := fs.ClusterChain(entry.StartCluster)
	if err != nil {
		t.Fatalf("ClusterChain: %v", err)
	}

	if len(chain) < 2 {
		t.Fatalf(
			"directory cluster chain length = %d, want at least 2",
			len(chain),
		)
	}
}

// Deletes files created during directory expansion and verifies their slots can be reused.
func TestYPZ5DirectoryExpansionReuse(t *testing.T) {
	_, fs := openYPZ5WriteImage(t)

	const directory = "/YPZ5 Expansion Reuse"

	if err := fs.Mkdir(directory); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	const fileCount = 200
	content := []byte("directory reuse test")

	for i := 0; i < fileCount; i++ {
		path := fmt.Sprintf(
			"%s/new-%03d.txt",
			directory,
			i,
		)

		if err := fs.WriteFile(path, content); err != nil {
			t.Fatalf("WriteFile(%q): %v", path, err)
		}
	}

	// Delete every second file to create reusable single directory slots.
	for i := 0; i < fileCount; i += 2 {
		path := fmt.Sprintf(
			"%s/new-%03d.txt",
			directory,
			i,
		)

		if err := fs.Delete(path); err != nil {
			t.Fatalf("Delete(%q): %v", path, err)
		}
	}

	// Reuse the deleted slots with short 8.3 filenames.
	for i := 0; i < fileCount; i += 2 {
		path := fmt.Sprintf(
			"%s/r%03d.txt",
			directory,
			i,
		)

		if err := fs.WriteFile(path, content); err != nil {
			t.Fatalf("WriteFile(%q): %v", path, err)
		}
	}

	entries, err := fs.ReadDir(directory)
	if err != nil {
		t.Fatalf("ReadDir after reuse: %v", err)
	}

	if len(entries) != fileCount {
		t.Fatalf(
			"directory contains %d entries after reuse, want %d",
			len(entries),
			fileCount,
		)
	}

	for _, index := range []int{0, 36, 198, 199} {
		var path string

		if index%2 == 0 {
			path = fmt.Sprintf(
				"%s/r%03d.txt",
				directory,
				index,
			)
		} else {
			path = fmt.Sprintf(
				"%s/new-%03d.txt",
				directory,
				index,
			)
		}

		got, err := fs.ReadFile(path)
		if err != nil {
			t.Fatalf(
				"ReadFile(%q): %v",
				path,
				err,
			)
		}

		if !bytes.Equal(got, content) {
			t.Fatalf(
				"ReadFile(%q) = %q, want %q",
				path,
				got,
				content,
			)
		}
	}
}

// Reports the physical directory cluster geometry after creating 200 files.
func TestYPZ5DirectoryExpansionGeometry(t *testing.T) {
	_, fs := openYPZ5WriteImage(t)

	const directory = "/YPZ5 Expansion Geometry"

	if err := fs.Mkdir(directory); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	const fileCount = 200
	content := []byte("geometry test")

	for i := 0; i < fileCount; i++ {
		path := fmt.Sprintf(
			"%s/file-%03d.txt",
			directory,
			i,
		)

		if err := fs.WriteFile(path, content); err != nil {
			t.Fatalf("WriteFile(%q): %v", path, err)
		}
	}

	entry, err := fs.Lookup(directory)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	chain, err := fs.ClusterChain(entry.StartCluster)
	if err != nil {
		t.Fatalf("ClusterChain: %v", err)
	}

	t.Logf("directory start cluster: %d", entry.StartCluster)
	t.Logf("directory chain: %v", chain)
	t.Logf("bytes per cluster: %d", fs.BootSector().Layout().BytesPerCluster)
	t.Logf("entries per cluster: %d",
		fs.BootSector().Layout().BytesPerCluster/32,
	)

	entries, err := fs.ReadDir(directory)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	t.Logf("visible entries: %d", len(entries))
}

// Exercises a complete write/replace/delete/reopen lifecycle and validates the final image with mtools.
func TestYPZ5EndToEndLifecycleWithMtools(t *testing.T) {
	imagePath, file, fs := openYPZ5WriteImageWithPath(t)

	const (
		directory   = "/YPZ5 End-to-End"
		asciiFile   = directory + "/hello.txt"
		unicodeFile = directory + "/Caffè 🚀 日本語.txt"
	)

	original := []byte("original contents")
	replacement := []byte("replacement contents with more data")
	unicodeContent := []byte("unicode end-to-end test")

	if err := fs.Mkdir(directory); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	if err := fs.WriteFile(asciiFile, original); err != nil {
		t.Fatalf("WriteFile(%q): %v", asciiFile, err)
	}

	if err := fs.WriteFile(unicodeFile, unicodeContent); err != nil {
		t.Fatalf("WriteFile(%q): %v", unicodeFile, err)
	}

	if err := fs.ReplaceFile(asciiFile, replacement); err != nil {
		t.Fatalf("ReplaceFile(%q): %v", asciiFile, err)
	}

	if err := fs.Delete(unicodeFile); err != nil {
		t.Fatalf("Delete(%q): %v", unicodeFile, err)
	}

	got, err := fs.ReadFile(asciiFile)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", asciiFile, err)
	}

	if !bytes.Equal(got, replacement) {
		t.Fatalf(
			"ReadFile(%q) = %q, want %q",
			asciiFile,
			got,
			replacement,
		)
	}

	if _, err := fs.Lookup(unicodeFile); err == nil {
		t.Fatalf("Lookup(%q) succeeded after Delete", unicodeFile)
	}

	// Close and reopen the image to verify that all changes were persisted.
	if err := file.Close(); err != nil {
		t.Fatalf("close writable image: %v", err)
	}

	reopened, err := os.OpenFile(imagePath, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("reopen test image: %v", err)
	}

	reopenedOffset, reopenedSize := imagePartition(t, reopened)
	partition := io.NewSectionReader(reopened, reopenedOffset, reopenedSize)

	reopenedFS := New(partition)

	if err := reopenedFS.Open(); err != nil {
		reopened.Close()
		t.Fatalf("reopen filesystem: %v", err)
	}

	got, err = reopenedFS.ReadFile(asciiFile)
	if err != nil {
		reopened.Close()
		t.Fatalf("ReadFile after reopen: %v", err)
	}

	if !bytes.Equal(got, replacement) {
		reopened.Close()
		t.Fatalf(
			"ReadFile after reopen = %q, want %q",
			got,
			replacement,
		)
	}

	if _, err := reopenedFS.Lookup(unicodeFile); err == nil {
		reopened.Close()
		t.Fatalf("deleted Unicode file exists after reopen")
	}

	if err := reopened.Close(); err != nil {
		t.Fatalf("close reopened image: %v", err)
	}

	// Validate the persisted filesystem independently with mtools.
	if _, err := exec.LookPath("mdir"); err != nil {
		t.Skipf("mtools not installed: %v", err)
	}

	if _, err := exec.LookPath("mtype"); err != nil {
		t.Skipf("mtools mtype not installed: %v", err)
	}

	imageSpec := imagePath + "@@" + fmt.Sprint(reopenedOffset)

	cmd := exec.Command(
		"mdir",
		"-i",
		imageSpec,
		"::"+directory,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf(
			"mtools mdir failed: %v\n%s",
			err,
			output,
		)
	}

	// mtools displays the 8.3 name, lower-cased according to the NT
	// case flags.
	if !bytes.Contains(bytes.ToUpper(output), []byte("HELLO    TXT")) {
		t.Fatalf(
			"mtools directory listing does not contain HELLO.TXT:\n%s",
			output,
		)
	}

	// Verify independently that the deleted Unicode file is no longer accessible.
	cmd = exec.Command(
		"mtype",
		"-i",
		imageSpec,
		"::"+unicodeFile,
	)

	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf(
			"mtools can still read deleted Unicode file:\n%s",
			output,
		)
	}

	// Read the replaced file through mtools and verify its persisted contents.
	cmd = exec.Command(
		"mtype",
		"-i",
		imageSpec,
		"::"+asciiFile,
	)

	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf(
			"mtools mtype failed: %v\n%s",
			err,
			output,
		)
	}

	if !bytes.Equal(output, replacement) {
		t.Fatalf(
			"mtools mtype = %q, want %q",
			output,
			replacement,
		)
	}
}

func TestYPZ5FATCopiesStayInSync(t *testing.T) {
	_, fs := openYPZ5WriteImage(t)

	path := testDir(t, fs, "YPZ5 Tests") + "/YPZ5 FAT copy test.txt"

	content := []byte("FAT copy synchronization test")

	if err := fs.WriteFile(path, content); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entry, err := fs.Lookup(path)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	chain, err := fs.ClusterChain(entry.StartCluster)
	if err != nil {
		t.Fatalf("ClusterChain: %v", err)
	}

	// Verify that every cluster in the new file has identical entries in both FATs.
	for _, cluster := range chain {
		values := make([]uint32, fs.boot.NumberOfFATs)

		for fat := uint8(0); fat < fs.boot.NumberOfFATs; fat++ {
			offset := fs.layout.FATOffset +
				int64(fat)*int64(fs.boot.SectorsPerFAT)*int64(fs.boot.BytesPerSector) +
				int64(cluster)*4

			var buf [4]byte

			if _, err := fs.reader.ReadAt(buf[:], offset); err != nil {
				t.Fatalf(
					"read FAT %d entry for cluster %d: %v",
					fat,
					cluster,
					err,
				)
			}

			values[fat] = binary.LittleEndian.Uint32(buf[:]) &
				0x0FFFFFFF
		}

		for fat := 1; fat < len(values); fat++ {
			if values[fat] != values[0] {
				t.Fatalf(
					"cluster %d: FAT %d entry = %#x, FAT 0 entry = %#x",
					cluster,
					fat,
					values[fat],
					values[0],
				)
			}
		}
	}

	if err := fs.Delete(path); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify that deleting the file clears its old clusters in every FAT.
	for _, cluster := range chain {
		for fat := uint8(0); fat < fs.boot.NumberOfFATs; fat++ {
			offset := fs.layout.FATOffset +
				int64(fat)*int64(fs.boot.SectorsPerFAT)*int64(fs.boot.BytesPerSector) +
				int64(cluster)*4

			var buf [4]byte

			if _, err := fs.reader.ReadAt(buf[:], offset); err != nil {
				t.Fatalf(
					"read FAT %d entry after delete for cluster %d: %v",
					fat,
					cluster,
					err,
				)
			}

			value := binary.LittleEndian.Uint32(buf[:]) &
				0x0FFFFFFF

			if value != 0 {
				t.Fatalf(
					"cluster %d: FAT %d entry after delete = %#x, want 0",
					cluster,
					fat,
					value,
				)
			}
		}
	}
}

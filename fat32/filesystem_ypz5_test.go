package fat32

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

const (
	ypz5PartitionOffset int64 = 16384
	ypz5PartitionSize   int64 = 2028355584
)

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

	partition := io.NewSectionReader(
		file,
		ypz5PartitionOffset,
		ypz5PartitionSize,
	)

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

	partition := io.NewSectionReader(
		destination,
		ypz5PartitionOffset,
		ypz5PartitionSize,
	)

	writer := &partitionWriter{
		file:   destination,
		offset: ypz5PartitionOffset,
		size:   ypz5PartitionSize,
	}

	fs := NewReadWrite(partition, writer)

	if err := fs.Open(); err != nil {
		destination.Close()
		t.Fatalf("open filesystem: %v", err)
	}

	t.Cleanup(func() {
		_ = destination.Close()
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

// Verifies the FAT32 geometry and root directory of the YP-Z5 image.
func TestYPZ5Filesystem(t *testing.T) {
	_, fs := openYPZ5Image(t)

	if got := fs.BootSector().BytesPerSector; got != 512 {
		t.Fatalf("BytesPerSector = %d, want 512", got)
	}

	if got := fs.BootSector().SectorsPerCluster; got != 8 {
		t.Fatalf("SectorsPerCluster = %d, want 8", got)
	}

	if got := fs.BootSector().NumberOfFATs; got != 2 {
		t.Fatalf("NumberOfFATs = %d, want 2", got)
	}

	if got := fs.BootSector().SectorsPerFAT; got != 3862 {
		t.Fatalf("SectorsPerFAT = %d, want 3862", got)
	}

	if got := fs.RootCluster(); got != 2 {
		t.Fatalf("RootCluster = %d, want 2", got)
	}

	entries, err := fs.ReadDir("/")
	if err != nil {
		t.Fatalf("ReadDir(/): %v", err)
	}

	if len(entries) == 0 {
		t.Fatal("root directory is unexpectedly empty")
	}
}

// Verifies that the existing Music directory can be resolved and listed.
func TestYPZ5MusicDirectory(t *testing.T) {
	_, fs := openYPZ5Image(t)

	entries, err := fs.ReadDir("/Music")
	if err != nil {
		t.Fatalf("ReadDir(/Music): %v", err)
	}

	if len(entries) == 0 {
		t.Fatal("/Music directory is unexpectedly empty")
	}

	var foundCartoni bool

	for _, entry := range entries {
		if entry.Name == "Cartoni animati" {
			foundCartoni = true

			if !entry.IsDir {
				t.Fatal(`"Cartoni animati" is not a directory`)
			}
		}
	}

	if !foundCartoni {
		t.Fatal(`"/Music" does not contain "Cartoni animati"`)
	}
}

// Verifies that the existing long filename and its contents can be read.
func TestYPZ5PollonFile(t *testing.T) {
	_, fs := openYPZ5Image(t)

	const path = "/Music/Cartoni animati/Pollon.mp3"

	entry, err := fs.Lookup(path)
	if err != nil {
		t.Fatalf("Lookup(%q): %v", path, err)
	}

	if entry.IsDir {
		t.Fatalf("%q is unexpectedly a directory", path)
	}

	const expectedSize uint32 = 2801048

	if entry.Size != expectedSize {
		t.Fatalf(
			"%q size = %d, want %d",
			path,
			entry.Size,
			expectedSize,
		)
	}

	data, err := fs.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}

	if uint32(len(data)) != expectedSize {
		t.Fatalf(
			"ReadFile(%q) returned %d bytes, want %d",
			path,
			len(data),
			expectedSize,
		)
	}

	if len(data) < 4 {
		t.Fatal("Pollon.mp3 is unexpectedly short")
	}

	if bytes.Equal(data[:4], []byte{0, 0, 0, 0}) {
		t.Fatal("Pollon.mp3 begins with four zero bytes")
	}
}

// Verifies that the known Pollon cluster chain is read correctly.
func TestYPZ5PollonClusterChain(t *testing.T) {
	_, fs := openYPZ5Image(t)

	entry, err := fs.Lookup("/Music/Cartoni animati/Pollon.mp3")
	if err != nil {
		t.Fatal(err)
	}

	chain, err := fs.ClusterChain(entry.StartCluster)
	if err != nil {
		t.Fatalf("ClusterChain: %v", err)
	}

	if len(chain) != 684 {
		t.Fatalf(
			"Pollon cluster chain length = %d, want 684",
			len(chain),
		)
	}

	if chain[0] != 188294 {
		t.Fatalf(
			"Pollon first cluster = %d, want 188294",
			chain[0],
		)
	}

	if chain[len(chain)-1] != 188977 {
		t.Fatalf(
			"Pollon last cluster = %d, want 188977",
			chain[len(chain)-1],
		)
	}
}

// Creates, reads, deletes, and recreates a file in an existing YP-Z5 directory.
func TestYPZ5WriteFileLifecycle(t *testing.T) {
	_, fs := openYPZ5WriteImage(t)

	const path = "/Music/Cartoni animati/YPZ5 test file.txt"

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

	const path = "/Music/Cartoni animati/Prova Unicode 🚀 日本語.txt"

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

// Replaces an existing YP-Z5 file and verifies that its old data clusters are released.
func TestYPZ5ReplaceFile(t *testing.T) {
	_, fs := openYPZ5WriteImage(t)

	const path = "/BOOTEX.LOG"

	before, err := fs.Lookup(path)
	if err != nil {
		t.Fatalf("Lookup before replacement: %v", err)
	}

	if before.IsDir {
		t.Fatal("BOOTEX.LOG is unexpectedly a directory")
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
				"old BOOTEX.LOG cluster %d is still allocated",
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

	const directory = "/Music/YPZ5 Test Directory"

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

	const directory = "/Music/YPZ5 Expansion Test"

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

	const directory = "/Music/YPZ5 Expansion Chain"

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

	const directory = "/Music/YPZ5 Expansion Reuse"

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

	const directory = "/Music/YPZ5 Expansion Geometry"

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
		directory   = "/Music/YPZ5 End-to-End"
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

	partition := io.NewSectionReader(
		reopened,
		ypz5PartitionOffset,
		ypz5PartitionSize,
	)

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

	imageSpec := imagePath + "@@" + fmt.Sprint(ypz5PartitionOffset)

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

	// mtools displays the generated 8.3 alias rather than the long filename.
	if !bytes.Contains(output, []byte("HELLO    TXT")) {
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

	const path = "/Music/Cartoni animati/YPZ5 FAT copy test.txt"

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

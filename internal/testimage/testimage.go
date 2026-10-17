// Package testimage gives tests a writable copy of the FAT32 disk image
// named by the YP_Z5_IMAGE environment variable: a YP-Z5 dump or any
// other FAT32 image. Tests using it are skipped when the variable is not
// set.
package testimage

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/piero/ypz5-manager/fat32"
	"github.com/piero/ypz5-manager/internal/storage"
)

// Open opens a writable clone of the image through the same
// storage layer the TUI uses. The original image is never written.
func Open(t *testing.T) *fat32.Filesystem {
	t.Helper()

	source := os.Getenv("YP_Z5_IMAGE")
	if source == "" {
		t.Skip("requires YP_Z5_IMAGE")
	}

	clone := filepath.Join(t.TempDir(), "image.img")

	if output, err := exec.Command("cp", "-c", source, clone).CombinedOutput(); err != nil {
		t.Fatalf("clone image: %v: %s", err, output)
	}

	// The clone keeps the source's mode, and the pristine image is
	// expected to be read-only.
	if err := os.Chmod(clone, 0o600); err != nil {
		t.Fatal(err)
	}

	st, err := storage.Open(clone, true, false)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { st.Close() })

	fs := fat32.NewReadWrite(st, st)

	if err := fs.Open(); err != nil {
		t.Fatal(err)
	}

	return fs
}

// WriteTree creates files (path → contents) under root.
func WriteTree(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for name, contents := range files {
		path := filepath.Join(root, name)

		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

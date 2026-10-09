package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/piero/ypz5/fat32"
)

// maxFAT32FileSize is the largest file FAT32 can store.
const maxFAT32FileSize = 1<<32 - 1

// copyOp is one step of copying local files to the device: create a
// directory or write one file.
type copyOp struct {
	source string
	dest   string
	isDir  bool
	size   int64
}

// copyPlan lists everything a copy will do, computed before anything is
// written so free space can be checked up front.
type copyPlan struct {
	ops      []copyOp
	skipped  []string
	files    int
	bytes    int64
	clusters int64
}

// isJunkFile reports whether name is macOS metadata that must never be
// copied to the device: Finder's .DS_Store and AppleDouble "._" files,
// which the player would otherwise try to index as tracks.
func isJunkFile(name string) bool {
	return name == ".DS_Store" || strings.HasPrefix(name, "._")
}

// planCopy walks the local sources and plans copying them into destDir
// on the device. clusterSize is used to estimate the space needed.
func planCopy(sources []string, destDir string, clusterSize int64) (copyPlan, error) {
	var plan copyPlan

	clustersFor := func(size int64) int64 {
		return (size + clusterSize - 1) / clusterSize
	}

	// The destination directory may need to grow to hold new entries.
	plan.clusters++

	for _, source := range sources {
		root := filepath.Dir(source)

		err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			name := entry.Name()

			if isJunkFile(name) {
				plan.skipped = append(plan.skipped, path+" (macOS metadata)")

				if entry.IsDir() {
					return filepath.SkipDir
				}

				return nil
			}

			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}

			dest := joinPath(destDir, filepath.ToSlash(relative))

			switch {
			case entry.IsDir():
				plan.ops = append(plan.ops, copyOp{
					source: path,
					dest:   dest,
					isDir:  true,
				})

				// The directory's first cluster, plus room to grow.
				plan.clusters += 2

			case entry.Type().IsRegular():
				info, err := entry.Info()
				if err != nil {
					return err
				}

				if info.Size() > maxFAT32FileSize {
					plan.skipped = append(plan.skipped, path+" (larger than 4 GiB)")
					return nil
				}

				plan.ops = append(plan.ops, copyOp{
					source: path,
					dest:   dest,
					size:   info.Size(),
				})

				plan.files++
				plan.bytes += info.Size()
				plan.clusters += clustersFor(info.Size())

			default:
				plan.skipped = append(plan.skipped, path+" (not a regular file)")
			}

			return nil
		})
		if err != nil {
			return copyPlan{}, err
		}
	}

	return plan, nil
}

// checkSpace fails if the plan needs more clusters than are free.
func checkSpace(fs *fat32.Filesystem, plan copyPlan) error {
	free, ok := fs.FreeClusters()
	if !ok {
		return nil
	}

	if plan.clusters > int64(free) {
		clusterSize := fs.BytesPerCluster()

		return fmt.Errorf(
			"not enough space: need %s, %s free",
			formatSize(plan.clusters*clusterSize),
			formatSize(int64(free)*clusterSize),
		)
	}

	return nil
}

// runCopyOp performs one step. Existing directories are merged into;
// existing files are skipped, never overwritten. skipped is non-empty
// when the step was skipped.
func runCopyOp(fs *fat32.Filesystem, op copyOp) (skipped string, err error) {
	existing, err := fs.Lookup(op.dest)

	switch {
	case err == nil:
		if op.isDir && existing.IsDir {
			return "", nil
		}

		return op.dest + " (already exists)", nil

	case !errors.Is(err, fat32.ErrNotFound):
		return "", err
	}

	if op.isDir {
		return "", fs.Mkdir(op.dest)
	}

	data, err := os.ReadFile(op.source)
	if err != nil {
		return "", err
	}

	return "", fs.WriteFile(op.dest, data)
}

func formatSize(bytes int64) string {
	const unit = 1024

	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	value := float64(bytes)
	suffixes := []string{"KiB", "MiB", "GiB"}

	for i, suffix := range suffixes {
		value /= unit

		if value < unit || i == len(suffixes)-1 {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}

	return ""
}

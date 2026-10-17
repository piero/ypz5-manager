// Package transfer copies files and folders from the local filesystem to
// a FAT32 filesystem. A copy is planned first, so free space can be
// checked before anything is written, and then run one step at a time.
package transfer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/piero/ypz5-manager/fat32"
)

// maxFAT32FileSize is the largest file FAT32 can store.
const maxFAT32FileSize = 1<<32 - 1

// Op is one step of a copy: create a directory or write one file.
type Op struct {
	Source string // local path
	Dest   string // path on the FAT32 filesystem
	IsDir  bool
	Size   int64
}

// Plan lists everything a copy will do, computed before anything is
// written so free space can be checked up front.
type Plan struct {
	Ops      []Op
	Skipped  []string // local paths not copied, with the reason
	Files    int
	Bytes    int64
	Clusters int64 // estimated clusters needed, including directories
}

// IsJunk reports whether name is macOS metadata that must never be
// copied to the device: Finder's .DS_Store and AppleDouble "._" files,
// which the player would otherwise try to index as tracks.
func IsJunk(name string) bool {
	return name == ".DS_Store" || strings.HasPrefix(name, "._")
}

// PlanCopy walks the local sources and plans copying them into destDir
// on the FAT32 filesystem. clusterSize is used to estimate the space
// needed.
func PlanCopy(sources []string, destDir string, clusterSize int64) (Plan, error) {
	var plan Plan

	clustersFor := func(size int64) int64 {
		return (size + clusterSize - 1) / clusterSize
	}

	// The destination directory may need to grow to hold new entries.
	plan.Clusters++

	for _, source := range sources {
		root := filepath.Dir(source)

		err := filepath.WalkDir(source, func(localPath string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			name := entry.Name()

			if IsJunk(name) {
				plan.Skipped = append(plan.Skipped, localPath+" (macOS metadata)")

				if entry.IsDir() {
					return filepath.SkipDir
				}

				return nil
			}

			relative, err := filepath.Rel(root, localPath)
			if err != nil {
				return err
			}

			dest := path.Join(destDir, filepath.ToSlash(relative))

			switch {
			case entry.IsDir():
				plan.Ops = append(plan.Ops, Op{
					Source: localPath,
					Dest:   dest,
					IsDir:  true,
				})

				// The directory's first cluster, plus room to grow.
				plan.Clusters += 2

			case entry.Type().IsRegular():
				info, err := entry.Info()
				if err != nil {
					return err
				}

				if info.Size() > maxFAT32FileSize {
					plan.Skipped = append(plan.Skipped, localPath+" (larger than 4 GiB)")
					return nil
				}

				plan.Ops = append(plan.Ops, Op{
					Source: localPath,
					Dest:   dest,
					Size:   info.Size(),
				})

				plan.Files++
				plan.Bytes += info.Size()
				plan.Clusters += clustersFor(info.Size())

			default:
				plan.Skipped = append(plan.Skipped, localPath+" (not a regular file)")
			}

			return nil
		})
		if err != nil {
			return Plan{}, err
		}
	}

	return plan, nil
}

// CheckSpace fails if the plan needs more clusters than are free.
func CheckSpace(fs *fat32.Filesystem, plan Plan) error {
	free, ok := fs.FreeClusters()
	if !ok {
		return nil
	}

	if plan.Clusters > int64(free) {
		clusterSize := fs.BytesPerCluster()

		return fmt.Errorf(
			"not enough space: need %s, %s free",
			FormatSize(plan.Clusters*clusterSize),
			FormatSize(int64(free)*clusterSize),
		)
	}

	return nil
}

// Run performs one step. Existing directories are merged into;
// existing files are skipped, never overwritten. skipped is non-empty
// when the step was skipped.
func Run(fs *fat32.Filesystem, op Op) (skipped string, err error) {
	existing, err := fs.Lookup(op.Dest)

	switch {
	case err == nil:
		if op.IsDir && existing.IsDir {
			return "", nil
		}

		return op.Dest + " (already exists)", nil

	case !errors.Is(err, fat32.ErrNotFound):
		return "", err
	}

	if op.IsDir {
		return "", fs.Mkdir(op.Dest)
	}

	data, err := os.ReadFile(op.Source)
	if err != nil {
		return "", err
	}

	return "", fs.WriteFile(op.Dest, data)
}

// FormatSize formats a byte count for people, e.g. "4.0 MiB".
func FormatSize(bytes int64) string {
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

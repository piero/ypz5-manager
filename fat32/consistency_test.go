package fat32

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
)

// checkConsistency fails the test unless the filesystem passes
// consistencyError.
func checkConsistency(t *testing.T, fs *Filesystem) {
	t.Helper()

	if err := consistencyError(fs, true); err != nil {
		t.Fatal(err)
	}
}

// consistencyError checks the whole filesystem the way fsck would: FAT
// copies are identical, every chain is valid, no cluster belongs to two
// owners, no allocated cluster is unreferenced and file sizes match their
// chains. With checkFSInfo, the FSInfo free count must also be exact; it
// is only a hint, so an image we did not write may have a stale one.
func consistencyError(fs *Filesystem, checkFSInfo bool) error {
	maxCluster := fs.boot.MaxCluster()
	fatBytes := int64(fs.boot.SectorsPerFAT) * int64(fs.boot.BytesPerSector)

	fats := make([][]byte, fs.boot.NumberOfFATs)

	for i := range fats {
		fats[i] = make([]byte, fatBytes)

		if _, err := fs.reader.ReadAt(
			fats[i],
			fs.layout.FATOffset+int64(i)*fatBytes,
		); err != nil {
			return fmt.Errorf("read FAT %d: %w", i, err)
		}

		if i > 0 && !bytes.Equal(fats[0], fats[i]) {
			return fmt.Errorf("FAT %d differs from FAT 0", i)
		}
	}

	owners := make(map[uint32]string)

	claim := func(start uint32, owner string) (int, error) {
		chain, err := fs.readClusterChain(start)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", owner, err)
		}

		for _, cluster := range chain {
			if cluster > maxCluster {
				return 0, fmt.Errorf("%s: cluster %d beyond end of volume", owner, cluster)
			}

			if previous, exists := owners[cluster]; exists {
				return 0, fmt.Errorf(
					"cluster %d is shared by %s and %s",
					cluster,
					previous,
					owner,
				)
			}

			owners[cluster] = owner
		}

		return len(chain), nil
	}

	var walk func(cluster uint32, path string) error

	walk = func(cluster uint32, path string) error {
		if _, err := claim(cluster, path+"/"); err != nil {
			return err
		}

		entries, err := fs.readDirectory(cluster)
		if err != nil {
			return fmt.Errorf("read directory %s/: %w", path, err)
		}

		for _, child := range entries {
			if child.Name == "." || child.Name == ".." {
				continue
			}

			childPath := path + "/" + child.Name

			if child.IsDir {
				if err := walk(child.StartCluster, childPath); err != nil {
					return err
				}

				continue
			}

			if child.StartCluster == 0 {
				if child.Size != 0 {
					return fmt.Errorf("%s: size %d without clusters", childPath, child.Size)
				}

				continue
			}

			got, err := claim(child.StartCluster, childPath)
			if err != nil {
				return err
			}

			if want := fs.clustersForSize(int(child.Size)); got != want {
				return fmt.Errorf(
					"%s: chain has %d clusters, size needs %d",
					childPath,
					got,
					want,
				)
			}
		}

		return nil
	}

	if err := walk(fs.boot.RootCluster, ""); err != nil {
		return err
	}

	var free uint32

	for cluster := uint32(2); cluster <= maxCluster; cluster++ {
		value := binary.LittleEndian.Uint32(fats[0][cluster*4:]) & 0x0FFFFFFF

		if value == 0 {
			free++
			continue
		}

		if value == fat32BadCluster {
			continue
		}

		if _, ok := owners[cluster]; !ok {
			return fmt.Errorf("cluster %d is allocated but unreferenced", cluster)
		}
	}

	if !checkFSInfo || !fs.info.present {
		return nil
	}

	buf := make([]byte, sectorSize)

	if _, err := fs.reader.ReadAt(buf, fs.info.offset); err != nil {
		return fmt.Errorf("read FSInfo: %w", err)
	}

	// A freshly opened writable filesystem has corrected the count in
	// memory but not written it yet.
	if fs.info.dirty {
		if fs.info.freeCount != free {
			return fmt.Errorf("FSInfo free count = %d, actual free clusters = %d", fs.info.freeCount, free)
		}

		return nil
	}

	if got := binary.LittleEndian.Uint32(buf[fsInfoFreeCountOffset:]); got != free {
		return fmt.Errorf("FSInfo free count = %d, actual free clusters = %d", got, free)
	}

	return nil
}

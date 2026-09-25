package fat32

import (
	"encoding/binary"
	"fmt"
)

const (
	fat32BadCluster = 0x0FFFFFF7
	fat32EOF        = 0x0FFFFFF8
)

func (fs *Filesystem) readFATEntry(cluster uint32) (uint32, error) {
	offset := fs.layout.FATOffset + int64(cluster)*4

	sectorSize := int64(fs.boot.BytesPerSector)
	sectorOffset := (offset / sectorSize) * sectorSize
	entryOffset := offset - sectorOffset

	buf := make([]byte, sectorSize)

	if _, err := fs.reader.ReadAt(buf, sectorOffset); err != nil {
		return 0, fmt.Errorf(
			"read FAT sector for cluster %d: %w",
			cluster,
			err,
		)
	}

	value := binary.LittleEndian.Uint32(
		buf[entryOffset : entryOffset+4],
	)

	return value & 0x0FFFFFFF, nil
}

func (fs *Filesystem) readClusterChain(start uint32) ([]uint32, error) {
	/*
		If start = 2

		we do:

			2
			│
			├── FAT[2] → 3
			│
			3
			│
			├── FAT[3] → 4
			│
			4
			│
			├── FAT[4] → 5
			│
			5
			│
			└── FAT[5] → EOF

		and return []uint32{2, 3, 4, 5}

	*/
	if start < 2 {
		return nil, fmt.Errorf("invalid start cluster: %d", start)
	}

	var chain []uint32
	visited := make(map[uint32]bool)

	cluster := start

	for {
		if visited[cluster] {
			return nil, fmt.Errorf(
				"cycle detected at cluster %d",
				cluster,
			)
		}

		visited[cluster] = true
		chain = append(chain, cluster)

		next, err := fs.readFATEntry(cluster)
		if err != nil {
			return nil, err
		}

		switch {
		case next >= fat32EOF:
			return chain, nil

		case next == fat32BadCluster:
			return nil, fmt.Errorf(
				"bad cluster in chain after cluster %d",
				cluster,
			)

		case next < 2:
			return nil, fmt.Errorf(
				"invalid cluster %d after cluster %d",
				next,
				cluster,
			)

		default:
			cluster = next
		}
	}
}

// FAT entry = 0          → free
// FAT entry = 2..        → allocated / part of a chain
// FAT entry >= 0x0FFFFFF8 → end of chain
// 0x0FFFFFF7             → bad cluster
func (fs *Filesystem) IsFreeCluster(cluster uint32) (bool, error) {
	value, err := fs.readFATEntry(cluster)
	if err != nil {
		return false, err
	}

	// We've already masked the FAT entry to 28 bits in readFATEntry()
	// so this method can simply check for zero.
	return value == 0, nil
}

func (fs *Filesystem) FindFreeCluster(start uint32) (uint32, error) {
	if start < 2 {
		start = 2
	}

	for cluster := start; cluster <= fs.boot.MaxCluster(); cluster++ {
		free, err := fs.IsFreeCluster(cluster)
		if err != nil {
			return 0, err
		}

		if free {
			return cluster, nil
		}
	}

	return 0, fmt.Errorf("no free clusters")
}

func (fs *Filesystem) ClusterChain(start uint32) ([]uint32, error) {
	return fs.readClusterChain(start)
}

func (fs *Filesystem) writeFATEntry(cluster uint32, value uint32) error {
	if err := fs.writable(); err != nil {
		return err
	}

	if cluster > fs.boot.MaxCluster() {
		return fmt.Errorf("invalid cluster: %d", cluster)
	}

	value &= 0x0FFFFFFF

	sectorSize := int64(fs.boot.BytesPerSector)

	type fatSector struct {
		offset int64
		old    []byte
		new    []byte
	}

	sectors := make([]fatSector, 0, fs.boot.NumberOfFATs)

	// Prepare all FAT sectors before writing anything.
	for fat := uint8(0); fat < fs.boot.NumberOfFATs; fat++ {
		offset := fs.layout.FATOffset +
			int64(fat)*int64(fs.boot.SectorsPerFAT)*sectorSize +
			int64(cluster)*4

		sectorOffset := (offset / sectorSize) * sectorSize
		entryOffset := offset - sectorOffset

		buf := make([]byte, sectorSize)

		if _, err := fs.reader.ReadAt(buf, sectorOffset); err != nil {
			return fmt.Errorf(
				"read FAT sector for cluster %d: %w",
				cluster,
				err,
			)
		}

		old := append([]byte(nil), buf...)

		oldValue := binary.LittleEndian.Uint32(
			buf[entryOffset : entryOffset+4],
		)

		newValue := (oldValue & 0xF0000000) | value

		binary.LittleEndian.PutUint32(
			buf[entryOffset:entryOffset+4],
			newValue,
		)

		sectors = append(sectors, fatSector{
			offset: sectorOffset,
			old:    old,
			new:    buf,
		})
	}

	// Write each FAT copy. If a later write fails, restore the
	// copies that were successfully written.
	for i, sector := range sectors {
		n, err := fs.writer.WriteAt(sector.new, sector.offset)
		if err != nil || n != len(sector.new) {
			rollbackErr := error(nil)

			for j := i - 1; j >= 0; j-- {
				previous := sectors[j]

				rn, rerr := fs.writer.WriteAt(
					previous.old,
					previous.offset,
				)

				if rerr != nil {
					rollbackErr = fmt.Errorf(
						"rollback FAT sector at offset %d: %w",
						previous.offset,
						rerr,
					)
					break
				}

				if rn != len(previous.old) {
					rollbackErr = fmt.Errorf(
						"rollback FAT sector at offset %d: short write: wrote %d of %d bytes",
						previous.offset,
						rn,
						len(previous.old),
					)
					break
				}
			}

			if err != nil {
				if rollbackErr != nil {
					return fmt.Errorf(
						"write FAT sector for cluster %d: %w; rollback failed: %v",
						cluster,
						err,
						rollbackErr,
					)
				}

				return fmt.Errorf(
					"write FAT sector for cluster %d: %w",
					cluster,
					err,
				)
			}

			if rollbackErr != nil {
				return fmt.Errorf(
					"short write of FAT sector for cluster %d: wrote %d of %d bytes; rollback failed: %v",
					cluster,
					n,
					len(sector.new),
					rollbackErr,
				)
			}

			return fmt.Errorf(
				"short write of FAT sector for cluster %d: wrote %d of %d bytes",
				cluster,
				n,
				len(sector.new),
			)
		}
	}

	return nil
}

func (fs *Filesystem) freeClusterChain(start uint32) error {
	if err := fs.writable(); err != nil {
		return err
	}

	chain, err := fs.readClusterChain(start)
	if err != nil {
		return err
	}

	for _, cluster := range chain {
		if err := fs.writeFATEntry(cluster, 0); err != nil {
			return fmt.Errorf(
				"free cluster %d: %w",
				cluster,
				err,
			)
		}
	}

	return nil
}

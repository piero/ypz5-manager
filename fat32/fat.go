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
	// FAT entry offset = FAT start + cluster number × 4
	offset := fs.layout.FATOffset + int64(cluster)*4

	var buf [4]byte

	if _, err := fs.reader.ReadAt(buf[:], offset); err != nil {
		return 0, fmt.Errorf("read FAT entry for cluster %d: %w", cluster, err)
	}

	// FAT32 entries use only the lower 28 bits:
	//
	// 31             28 27                         0
	// +----------------+----------------------------+
	// |    reserved    |       cluster number       |
	// +----------------+----------------------------+
	//
	// so we discard the 4 reserved bits.
	return binary.LittleEndian.Uint32(buf[:]) & 0x0FFFFFFF, nil
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

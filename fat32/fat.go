package fat32

import (
	"encoding/binary"
	"fmt"
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

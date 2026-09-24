package fat32

import (
	"encoding/binary"
	"fmt"
	"io"
)

const sectorSize = 512

type BootSector struct {
	BytesPerSector    uint16
	SectorsPerCluster uint8
	ReservedSectors   uint16
	NumberOfFATs      uint8
	SectorsPerFAT     uint32
	RootCluster       uint32
}

func readBootSector(r io.ReaderAt) (BootSector, error) {
	var buf [sectorSize]byte

	if _, err := r.ReadAt(buf[:], 0); err != nil {
		return BootSector{}, fmt.Errorf("read boot sector: %w", err)
	}

	return BootSector{
		BytesPerSector:    binary.LittleEndian.Uint16(buf[0x0B:0x0D]),
		SectorsPerCluster: buf[0x0D],
		ReservedSectors:   binary.LittleEndian.Uint16(buf[0x0E:0x10]),
		NumberOfFATs:      buf[0x10],
		SectorsPerFAT:     binary.LittleEndian.Uint32(buf[0x24:0x28]),
		RootCluster:       binary.LittleEndian.Uint32(buf[0x2C:0x30]),
	}, nil
}

func (b BootSector) Validate() error {
	if b.BytesPerSector == 0 {
		return fmt.Errorf("invalid bytes per sector")
	}

	if b.SectorsPerCluster == 0 {
		return fmt.Errorf("invalid sectors per cluster")
	}

	if b.NumberOfFATs == 0 {
		return fmt.Errorf("invalid number of FATs")
	}

	if b.SectorsPerFAT == 0 {
		return fmt.Errorf("invalid sectors per FAT")
	}

	if b.RootCluster < 2 {
		return fmt.Errorf("invalid root cluster: %d", b.RootCluster)
	}

	return nil
}

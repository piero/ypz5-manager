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
	TotalSectors      uint32
	RootCluster       uint32
	ExtFlags          uint16
	FSInfoSector      uint16
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
		TotalSectors:      binary.LittleEndian.Uint32(buf[0x20:0x24]),
		RootCluster:       binary.LittleEndian.Uint32(buf[0x2C:0x30]),
		ExtFlags:          binary.LittleEndian.Uint16(buf[0x28:0x2A]),
		FSInfoSector:      binary.LittleEndian.Uint16(buf[0x30:0x32]),
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

	if b.TotalSectors == 0 {
		return fmt.Errorf("invalid total sectors: %d", b.TotalSectors)
	}

	if b.RootCluster < 2 {
		return fmt.Errorf("invalid root cluster: %d", b.RootCluster)
	}

	return nil
}

// FATMirroringDisabled reports whether only one FAT is active
// (ExtFlags bit 7). This package always writes every FAT copy, which is
// only correct when mirroring is enabled.
func (b BootSector) FATMirroringDisabled() bool {
	return b.ExtFlags&0x80 != 0
}

// MaxCluster finds the highest valid FAT cluster number
func (b BootSector) MaxCluster() uint32 {
	return b.ClusterCount() + 1
}

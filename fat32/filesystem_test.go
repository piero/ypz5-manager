package fat32

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestNew(t *testing.T) {
	data := []byte("test filesystem")
	reader := bytes.NewReader(data)

	fs := New(reader)

	if fs.reader != reader {
		t.Fatalf("reader = %v, want %v", fs.reader, reader)
	}
}

func TestBootSector(t *testing.T) {
	var boot [512]byte

	binary.LittleEndian.PutUint16(boot[0x0B:0x0D], 512)
	boot[0x0D] = 8
	binary.LittleEndian.PutUint16(boot[0x0E:0x10], 32)
	boot[0x10] = 2
	binary.LittleEndian.PutUint32(boot[0x24:0x28], 1000)
	binary.LittleEndian.PutUint32(boot[0x2C:0x30], 2)

	got, err := readBootSector(bytes.NewReader(boot[:]))
	if err != nil {
		t.Fatal(err)
	}

	if got.BytesPerSector != 512 {
		t.Errorf("BytesPerSector = %d, want 512", got.BytesPerSector)
	}

	if got.SectorsPerCluster != 8 {
		t.Errorf("SectorsPerCluster = %d, want 8", got.SectorsPerCluster)
	}

	if got.ReservedSectors != 32 {
		t.Errorf("ReservedSectors = %d, want 32", got.ReservedSectors)
	}

	if got.NumberOfFATs != 2 {
		t.Errorf("NumberOfFATs = %d, want 2", got.NumberOfFATs)
	}

	if got.SectorsPerFAT != 1000 {
		t.Errorf("SectorsPerFAT = %d, want 1000", got.SectorsPerFAT)
	}

	if got.RootCluster != 2 {
		t.Errorf("RootCluster = %d, want 2", got.RootCluster)
	}
}

func TestBootSectorLayout(t *testing.T) {
	boot := BootSector{
		BytesPerSector:    512,
		SectorsPerCluster: 8,
		ReservedSectors:   36,
		NumberOfFATs:      2,
		SectorsPerFAT:     3862,
		RootCluster:       2,
	}

	layout := boot.Layout()

	if layout.FATOffset != 18432 {
		t.Errorf("FATOffset = %d, want 18432", layout.FATOffset)
	}

	if layout.DataOffset != 3973120 {
		t.Errorf("DataOffset = %d, want 3973120", layout.DataOffset)
	}

	if layout.BytesPerCluster != 4096 {
		t.Errorf("BytesPerCluster = %d, want 4096", layout.BytesPerCluster)
	}
}
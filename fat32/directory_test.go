package fat32

import (
	"testing"
)

func TestParseDirectoryEntry(t *testing.T) {
	data := []byte{
		0x50, 0x49, 0x43, 0x54, 0x55, 0x52, 0x45, 0x53,
		0x20, 0x20, 0x20,
		0x10,
		0x00,
		0xbc,
		0x58, 0x55,
		0x22, 0x36,
		0x22, 0x36,
		0x00, 0x00,
		0x59, 0x55,
		0x22, 0x36,
		0x03, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}

	got, err := parseDirectoryEntry(data)
	if err != nil {
		t.Fatal(err)
	}

	if got.ShortName != "PICTURES" {
		t.Errorf("ShortName = %q, want %q", got.ShortName, "PICTURES")
	}

	if !got.IsDir {
		t.Error("IsDir = false, want true")
	}

	if got.StartCluster != 3 {
		t.Errorf("StartCluster = %d, want 3", got.StartCluster)
	}

	if got.Size != 0 {
		t.Errorf("Size = %d, want 0", got.Size)
	}
}
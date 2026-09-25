package fat32

import "testing"

func TestMaxCluster(t *testing.T) {
	boot := BootSector{
		SectorsPerCluster: 8,
		ReservedSectors:   36,
		NumberOfFATs:      2,
		SectorsPerFAT:     3862,
		TotalSectors:      10000,
	}

	if got := boot.ClusterCount(); got != 280 {
		t.Fatalf("ClusterCount() = %d, want 280", got)
	}

	if got := boot.MaxCluster(); got != 281 {
		t.Fatalf("MaxCluster() = %d, want 281", got)
	}
}

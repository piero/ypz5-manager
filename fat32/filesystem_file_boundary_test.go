package fat32

import (
	"bytes"
	"fmt"
	"testing"
)

func TestFileSizeBoundaries(t *testing.T) {
	tests := []struct {
		name string
		size int
	}{
		// Tests an empty file with no allocated data clusters.
		{name: "empty", size: 0},

		// Tests a file occupying a single byte.
		{name: "one-byte", size: 1},

		// Tests the largest file that fits in one cluster.
		{name: "one-cluster-minus-one", size: 4095},

		// Tests a file occupying exactly one cluster.
		{name: "one-cluster", size: 4096},

		// Tests the first file size requiring two clusters.
		{name: "one-cluster-plus-one", size: 4097},

		// Tests an exact two-cluster file.
		{name: "two-clusters", size: 8192},

		// Tests the first file size requiring three clusters.
		{name: "two-clusters-plus-one", size: 8193},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, fs, _ := newAPITestFilesystem()

			data := make([]byte, test.size)
			for i := range data {
				data[i] = byte(i % 251)
			}

			path := fmt.Sprintf("/boundary-%s.bin", test.name)

			if err := fs.WriteFile(path, data); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}

			got, err := fs.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}

			if !bytes.Equal(got, data) {
				t.Fatalf("file contents differ")
			}

			entry, err := fs.Lookup(path)
			if err != nil {
				t.Fatalf("Lookup: %v", err)
			}

			if int(entry.Size) != test.size {
				t.Fatalf(
					"file size = %d, want %d",
					entry.Size,
					test.size,
				)
			}

			expectedClusters := fs.clustersForSize(test.size)

			if expectedClusters == 0 {
				if entry.StartCluster != 0 {
					t.Fatalf(
						"empty file start cluster = %d, want 0",
						entry.StartCluster,
					)
				}

				return
			}

			chain, err := fs.ClusterChain(entry.StartCluster)
			if err != nil {
				t.Fatalf("ClusterChain: %v", err)
			}

			if len(chain) != expectedClusters {
				t.Fatalf(
					"cluster chain length = %d, want %d",
					len(chain),
					expectedClusters,
				)
			}
		})
	}
}

// Tests replacing a one-cluster file with a file requiring two clusters.
func TestReplaceFileAcrossClusterBoundary(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	clusterSize := int(fs.layout.BytesPerCluster)

	original := bytes.Repeat([]byte{0x11}, clusterSize)
	replacement := bytes.Repeat([]byte{0x22}, clusterSize+1)

	if err := fs.WriteFile("/boundary.bin", original); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := fs.ReplaceFile("/boundary.bin", replacement); err != nil {
		t.Fatalf("ReplaceFile: %v", err)
	}

	got, err := fs.ReadFile("/boundary.bin")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if !bytes.Equal(got, replacement) {
		t.Fatalf("replacement contents differ")
	}

	entry, err := fs.Lookup("/boundary.bin")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	if entry.Size != uint32(len(replacement)) {
		t.Fatalf(
			"file size = %d, want %d",
			entry.Size,
			len(replacement),
		)
	}

	chain, err := fs.ClusterChain(entry.StartCluster)
	if err != nil {
		t.Fatalf("ClusterChain: %v", err)
	}

	if len(chain) != 2 {
		t.Fatalf(
			"cluster chain length = %d, want 2",
			len(chain),
		)
	}
}

// Tests replacing a two-cluster file with an empty file.
func TestReplaceFileWithEmptyAcrossClusterBoundary(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	clusterSize := int(fs.layout.BytesPerCluster)
	original := bytes.Repeat([]byte{0x33}, clusterSize+1)

	if err := fs.WriteFile("/boundary.bin", original); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entry, err := fs.Lookup("/boundary.bin")
	if err != nil {
		t.Fatalf("Lookup before replacement: %v", err)
	}

	oldChain, err := fs.ClusterChain(entry.StartCluster)
	if err != nil {
		t.Fatalf("ClusterChain before replacement: %v", err)
	}

	if len(oldChain) != 2 {
		t.Fatalf(
			"original cluster chain length = %d, want 2",
			len(oldChain),
		)
	}

	if err := fs.ReplaceFile("/boundary.bin", nil); err != nil {
		t.Fatalf("ReplaceFile: %v", err)
	}

	got, err := fs.ReadFile("/boundary.bin")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if len(got) != 0 {
		t.Fatalf("replacement file length = %d, want 0", len(got))
	}

	entry, err = fs.Lookup("/boundary.bin")
	if err != nil {
		t.Fatalf("Lookup after replacement: %v", err)
	}

	if entry.Size != 0 {
		t.Fatalf("file size = %d, want 0", entry.Size)
	}

	if entry.StartCluster != 0 {
		t.Fatalf(
			"empty replacement start cluster = %d, want 0",
			entry.StartCluster,
		)
	}

	for _, cluster := range oldChain {
		free, err := fs.IsFreeCluster(cluster)
		if err != nil {
			t.Fatalf(
				"IsFreeCluster(%d): %v",
				cluster,
				err,
			)
		}

		if !free {
			t.Fatalf(
				"old cluster %d is still allocated",
				cluster,
			)
		}
	}
}

// Tests that unused bytes in the final data cluster are zero-filled.
func TestWriteFileZeroFillsFinalCluster(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	clusterSize := int(fs.layout.BytesPerCluster)
	size := clusterSize + 17

	data := bytes.Repeat([]byte{0xAA}, size)

	if err := fs.WriteFile("/zero-fill.bin", data); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entry, err := fs.Lookup("/zero-fill.bin")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	chain, err := fs.ClusterChain(entry.StartCluster)
	if err != nil {
		t.Fatalf("ClusterChain: %v", err)
	}

	if len(chain) != 2 {
		t.Fatalf(
			"cluster chain length = %d, want 2",
			len(chain),
		)
	}

	finalCluster, err := fs.readCluster(chain[len(chain)-1])
	if err != nil {
		t.Fatalf("readCluster: %v", err)
	}

	for i := size - clusterSize; i < clusterSize; i++ {
		if finalCluster[i] != 0 {
			t.Fatalf(
				"final cluster byte %d = %#02x, want 0",
				i,
				finalCluster[i],
			)
		}
	}
}

// Tests that a failed file-data write frees all clusters allocated for the file.
func TestWriteFileRollbackFreesAllocatedClusters(t *testing.T) {
	data, fs, baseWriter := newAPITestFilesystem()

	clusterSize := int(fs.layout.BytesPerCluster)
	fileData := bytes.Repeat([]byte{0x5A}, clusterSize+1)

	firstFree, err := fs.FindFreeCluster(2)
	if err != nil {
		t.Fatalf("FindFreeCluster before write: %v", err)
	}

	dataOffset := fs.layout.DataOffset +
		int64(firstFree-2)*int64(fs.layout.BytesPerCluster)

	writer := &failOnWriteCountAt{
		data:       baseWriter.data,
		failOffset: dataOffset,
		failCount:  1,
	}

	fs.writer = writer

	err = fs.WriteFile("/rollback.bin", fileData)
	if err == nil {
		t.Fatal("WriteFile succeeded, want error")
	}

	if _, err := fs.Lookup("/rollback.bin"); err == nil {
		t.Fatal("rollback.bin exists after failed WriteFile")
	}

	afterFree, err := fs.FindFreeCluster(2)
	if err != nil {
		t.Fatalf("FindFreeCluster after rollback: %v", err)
	}

	if afterFree != firstFree {
		t.Fatalf(
			"first free cluster after rollback = %d, want %d",
			afterFree,
			firstFree,
		)
	}

	_ = data
}

// Tests that a reused data cluster is zero-filled beyond the new file size.
func TestWriteFileZeroFillsReusedCluster(t *testing.T) {
	_, fs, _ := newAPITestFilesystem()

	clusterSize := int(fs.layout.BytesPerCluster)

	original := bytes.Repeat([]byte{0xAA}, clusterSize)
	if err := fs.WriteFile("/first.bin", original); err != nil {
		t.Fatalf("WriteFile first: %v", err)
	}

	entry, err := fs.Lookup("/first.bin")
	if err != nil {
		t.Fatalf("Lookup first: %v", err)
	}

	oldCluster := entry.StartCluster

	if err := fs.Delete("/first.bin"); err != nil {
		t.Fatalf("Delete first: %v", err)
	}

	free, err := fs.IsFreeCluster(oldCluster)
	if err != nil {
		t.Fatalf("IsFreeCluster: %v", err)
	}

	if !free {
		t.Fatalf("cluster %d was not freed", oldCluster)
	}

	replacementSize := clusterSize / 2
	replacement := bytes.Repeat([]byte{0x55}, replacementSize)

	if err := fs.WriteFile("/second.bin", replacement); err != nil {
		t.Fatalf("WriteFile second: %v", err)
	}

	entry, err = fs.Lookup("/second.bin")
	if err != nil {
		t.Fatalf("Lookup second: %v", err)
	}

	if entry.StartCluster != oldCluster {
		t.Fatalf(
			"reused cluster = %d, want %d",
			entry.StartCluster,
			oldCluster,
		)
	}

	cluster, err := fs.readCluster(entry.StartCluster)
	if err != nil {
		t.Fatalf("readCluster: %v", err)
	}

	for i := replacementSize; i < clusterSize; i++ {
		if cluster[i] != 0 {
			t.Fatalf(
				"reused cluster byte %d = %#02x, want 0",
				i,
				cluster[i],
			)
		}
	}
}

package fat32

import "fmt"

func (fs *Filesystem) readCluster(cluster uint32) ([]byte, error) {
	if cluster < 2 {
		return nil, fmt.Errorf("invalid cluster: %d", cluster)
	}

	// FAT32's data region starts at cluster 2:
	//   cluster 2 → first data cluster
	//   cluster 3 → second data cluster
	//   cluster 4 → third data cluster
	//   ...
	//
	// cluster offset = DataOffset + (cluster - 2) × BytesPerCluster
	size := fs.layout.BytesPerCluster
	offset := fs.layout.DataOffset + int64(cluster-2)*size

	data := make([]byte, size)

	if _, err := fs.reader.ReadAt(data, offset); err != nil {
		return nil, fmt.Errorf(
			"read cluster %d: %w",
			cluster,
			err,
		)
	}

	return data, nil
}

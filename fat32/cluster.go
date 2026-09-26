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

func (fs *Filesystem) ReadRootCluster() ([]byte, error) {
	return fs.readCluster(fs.boot.RootCluster)
}

func (fs *Filesystem) writeCluster(cluster uint32, data []byte) error {
	if err := fs.writable(); err != nil {
		return err
	}

	if cluster < 2 || cluster > fs.boot.MaxCluster() {
		return fmt.Errorf("invalid cluster: %d", cluster)
	}

	if int64(len(data)) != fs.layout.BytesPerCluster {
		return fmt.Errorf(
			"invalid cluster data size: got %d, want %d",
			len(data),
			fs.layout.BytesPerCluster,
		)
	}

	offset := fs.layout.DataOffset +
		int64(cluster-2)*fs.layout.BytesPerCluster

	n, err := fs.writer.WriteAt(data, offset)
	if err != nil {
		return fmt.Errorf(
			"write cluster %d: %w",
			cluster,
			err,
		)
	}

	if n != len(data) {
		return fmt.Errorf(
			"short write of cluster %d: wrote %d of %d bytes",
			cluster,
			n,
			len(data),
		)
	}

	return nil
}

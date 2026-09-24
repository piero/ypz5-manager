package fat32

import "fmt"

func (fs *Filesystem) readFile(entry DirectoryEntry) ([]byte, error) {
	if entry.IsDir {
		return nil, fmt.Errorf("%q is a directory", entry.Name)
	}

	if entry.Size == 0 {
		return []byte{}, nil
	}

	clusters, err := fs.readClusterChain(entry.StartCluster)
	if err != nil {
		return nil, err
	}

	data := make([]byte, 0, int(entry.Size))

	for _, cluster := range clusters {
		clusterData, err := fs.readCluster(cluster)
		if err != nil {
			return nil, err
		}

		data = append(data, clusterData...)
	}

	if len(data) < int(entry.Size) {
		return nil, fmt.Errorf(
			"file %q: cluster chain contains %d bytes, want %d",
			entry.Name,
			len(data),
			entry.Size,
		)
	}

	return data[:entry.Size], nil
}

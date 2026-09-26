package fat32

import "fmt"

type File struct {
	entry DirectoryEntry
	fs    *Filesystem
}

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

func (fs *Filesystem) ReadFile(path string) ([]byte, error) {
	entry, err := fs.findEntry(path)
	if err != nil {
		return nil, err
	}

	return fs.readFile(entry)
}

func (fs *Filesystem) OpenFile(path string) (*File, error) {
	entry, err := fs.findEntry(path)
	if err != nil {
		return nil, err
	}

	if entry.IsDir {
		return nil, fmt.Errorf("%q is a directory", path)
	}

	return &File{
		entry: entry,
		fs:    fs,
	}, nil
}

func (f *File) Name() string {
	return f.entry.Name
}

func (f *File) Size() uint32 {
	return f.entry.Size
}

func (f *File) ReadAll() ([]byte, error) {
	return f.fs.readFile(f.entry)
}

func (fs *Filesystem) writeFileData(
	chain []uint32,
	data []byte,
) error {
	clusterSize := int(fs.layout.BytesPerCluster)

	if len(data) > len(chain)*clusterSize {
		return fmt.Errorf(
			"file data is too large: %d bytes for %d clusters",
			len(data),
			len(chain),
		)
	}

	for i, cluster := range chain {
		start := i * clusterSize
		end := start + clusterSize

		clusterData := make([]byte, clusterSize)

		if start < len(data) {
			if end > len(data) {
				end = len(data)
			}

			copy(clusterData, data[start:end])
		}

		if err := fs.writeCluster(cluster, clusterData); err != nil {
			return fmt.Errorf(
				"write file data to cluster %d: %w",
				cluster,
				err,
			)
		}
	}

	return nil
}

func (fs *Filesystem) clustersForSize(size int) int {
	if size <= 0 {
		return 0
	}

	clusterSize := int(fs.layout.BytesPerCluster)

	return (size + clusterSize - 1) / clusterSize
}

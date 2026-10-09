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

	// Consecutive clusters are written with a single write, which is
	// much faster on USB devices than one write per cluster.
	maxRun := maxWriteSize / clusterSize
	if maxRun < 1 {
		maxRun = 1
	}

	for i := 0; i < len(chain); {
		run := 1

		for i+run < len(chain) &&
			run < maxRun &&
			chain[i+run] == chain[i+run-1]+1 {
			run++
		}

		start := i * clusterSize
		runData := make([]byte, run*clusterSize)

		if start < len(data) {
			copy(runData, data[start:])
		}

		if err := fs.writeClusters(chain[i], runData); err != nil {
			return fmt.Errorf(
				"write file data to clusters %d-%d: %w",
				chain[i],
				chain[i+run-1],
				err,
			)
		}

		i += run
	}

	return nil
}

// maxWriteSize caps the size of a single data write.
const maxWriteSize = 1 << 20

func (fs *Filesystem) clustersForSize(size int) int {
	if size <= 0 {
		return 0
	}

	clusterSize := int(fs.layout.BytesPerCluster)

	return (size + clusterSize - 1) / clusterSize
}

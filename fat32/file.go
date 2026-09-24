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

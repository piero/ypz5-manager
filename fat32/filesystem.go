package fat32

import (
	"errors"
	"fmt"
	"io"
	"time"
)

type Entry struct {
	Name    string
	IsDir   bool
	Size    uint32
	ModTime time.Time
}

type Filesystem struct {
	reader io.ReaderAt
	writer io.WriterAt
	boot   BootSector
	layout Layout
}

// read-only
func New(reader io.ReaderAt) *Filesystem {
	fs := &Filesystem{
		reader: reader,
	}

	if writer, ok := reader.(io.WriterAt); ok {
		fs.writer = writer
	}

	return fs
}

// read-write
func NewReadWrite(reader io.ReaderAt, writer io.WriterAt) *Filesystem {
	return &Filesystem{
		reader: reader,
		writer: writer,
	}
}

func (fs *Filesystem) writable() error {
	if fs.writer == nil {
		return fmt.Errorf("filesystem is read-only")
	}

	return nil
}

func (fs *Filesystem) Open() error {
	boot, err := readBootSector(fs.reader)
	if err != nil {
		return err
	}

	if err := boot.Validate(); err != nil {
		return fmt.Errorf("invalid FAT32 boot sector: %w", err)
	}

	fs.boot = boot
	fs.layout = boot.Layout()

	return nil
}

func (fs *Filesystem) RootCluster() uint32 {
	return fs.boot.RootCluster
}

func (fs *Filesystem) BootSector() BootSector {
	return fs.boot
}

func (fs *Filesystem) Delete(path string) error {
	if err := fs.writable(); err != nil {
		return err
	}

	record, err := fs.findDirectoryRecord(path)
	if err != nil {
		return err
	}

	if record.Entry.IsDir {
		return fmt.Errorf("%q is a directory", path)
	}

	// Read the complete chain before making any modifications.
	// A zero-length file has no allocated data clusters.
	var chain []uint32
	if record.Entry.StartCluster != 0 {
		chain, err = fs.readClusterChain(record.Entry.StartCluster)
		if err != nil {
			return err
		}
	}

	// Mark the directory entries as deleted first.
	// If this fails, the file remains intact and its clusters remain allocated.
	if err := fs.deleteDirectoryRecord(record); err != nil {
		return err
	}

	// Release the file's clusters.
	for _, cluster := range chain {
		if err := fs.writeFATEntry(cluster, 0); err != nil {
			return fmt.Errorf(
				"free cluster %d: %w",
				cluster,
				err,
			)
		}
	}

	return nil
}

func (fs *Filesystem) writeNewFileData(data []byte) ([]uint32, error) {
	if err := fs.writable(); err != nil {
		return nil, err
	}

	clusterCount := fs.clustersForSize(len(data))
	if clusterCount == 0 {
		return nil, nil
	}

	chain, err := fs.allocateClusterChain(clusterCount)
	if err != nil {
		return nil, err
	}

	if err := fs.writeFileData(chain, data); err != nil {
		if rollbackErr := fs.freeClusterChain(chain[0]); rollbackErr != nil {
			return nil, errors.Join(err, rollbackErr)
		}

		return nil, err
	}

	return chain, nil
}

// WriteFile writes a file into the given path.
//
// 1. validate path
// 2. resolve parent
// 3. check duplicate
// 4. allocate + write file data
// 5. create directory entry
// 6. if directory creation fails → free allocated chain
func (fs *Filesystem) WriteFile(path string, data []byte) error {
	if err := fs.writable(); err != nil {
		return err
	}

	parentPath, name, err := splitFilePath(path)
	if err != nil {
		return err
	}

	parentCluster, err := fs.findDirectoryCluster(parentPath)
	if err != nil {
		return err
	}

	existing, err := fs.findDirectoryEntry(parentCluster, name)
	if err != nil {
		return err
	}

	if existing != nil {
		return fmt.Errorf("file %q already exists", path)
	}

	chain, err := fs.writeNewFileData(data)
	if err != nil {
		return err
	}

	var startCluster uint32
	if len(chain) > 0 {
		startCluster = chain[0]
	}

	if err := fs.createDirectoryRecord(
		parentCluster,
		name,
		startCluster,
		uint32(len(data)),
	); err != nil {
		if len(chain) > 0 {
			if rollbackErr := fs.freeClusterChain(chain[0]); rollbackErr != nil {
				return errors.Join(err, rollbackErr)
			}
		}

		return err
	}

	return nil
}

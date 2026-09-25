package fat32

import (
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
	chain, err := fs.readClusterChain(record.Entry.StartCluster)
	if err != nil {
		return err
	}

	// FAT freed → directory deletion failure → file still appears, but its clusters are free

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

	// Mark the directory entries as deleted.
	if err := fs.deleteDirectoryRecord(record); err != nil {
		return err
	}

	return nil
}

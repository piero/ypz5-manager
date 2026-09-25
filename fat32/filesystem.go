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

func New(reader io.ReaderAt) *Filesystem {
	fs := &Filesystem{
		reader: reader,
	}

	if writer, ok := reader.(io.WriterAt); ok {
		fs.writer = writer
	}

	return fs
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

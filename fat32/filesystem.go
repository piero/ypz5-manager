package fat32

import (
	"errors"
	"fmt"
	"io"
	"time"
)

type Filesystem struct {
	reader io.ReaderAt
	writer io.WriterAt
	boot   BootSector
	layout Layout
	info   fsInfo
	fat    fatCache

	// clock returns the time used for directory entry timestamps;
	// nil means time.Now. Tests replace it.
	clock func() time.Time
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
	fs.fat = fatCache{}

	if fs.writer != nil && boot.FATMirroringDisabled() {
		return fmt.Errorf(
			"FAT mirroring is disabled; writing is not supported",
		)
	}

	if err := fs.loadFSInfo(); err != nil {
		return err
	}

	return nil
}

func (fs *Filesystem) RootCluster() uint32 {
	return fs.boot.RootCluster
}

func (fs *Filesystem) BootSector() BootSector {
	return fs.boot
}

// BytesPerCluster returns the allocation unit size.
func (fs *Filesystem) BytesPerCluster() int64 {
	return fs.layout.BytesPerCluster
}

// syncer is implemented by writers that can flush to stable storage,
// such as *os.File.
type syncer interface {
	Sync() error
}

// commit finishes a top-level mutation: it writes FSInfo and flushes
// the writer to stable storage. It runs even when the mutation failed,
// because a failed operation may still have changed the disk. Any error
// is joined into *errp.
func (fs *Filesystem) commit(errp *error) {
	var commitErr error

	if err := fs.writeFSInfo(); err != nil {
		commitErr = errors.Join(commitErr, err)
	}

	if s, ok := fs.writer.(syncer); ok {
		if err := s.Sync(); err != nil {
			commitErr = errors.Join(commitErr, fmt.Errorf("sync: %w", err))
		}
	}

	if commitErr != nil {
		*errp = errors.Join(*errp, commitErr)
	}
}

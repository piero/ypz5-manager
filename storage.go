package main

import (
	"fmt"
	"io"
	"os"
)

type storage struct {
	file   *os.File
	offset int64
}

func openStorage(path string, offset int64, writable bool) (*storage, error) {
	if offset < 0 {
		return nil, fmt.Errorf("invalid storage offset: %d", offset)
	}

	flags := os.O_RDONLY
	if writable {
		flags = os.O_RDWR
	}

	f, err := os.OpenFile(path, flags, 0)
	if err != nil {
		return nil, err
	}

	return &storage{
		file:   f,
		offset: offset,
	}, nil
}

func (s *storage) Close() error {
	return s.file.Close()
}

func (s *storage) ReadAt(p []byte, off int64) (int, error) {
	return s.file.ReadAt(p, s.offset+off)
}

func (s *storage) WriteAt(p []byte, off int64) (int, error) {
	return s.file.WriteAt(p, s.offset+off)
}

var (
	_ io.ReaderAt = (*storage)(nil)
	_ io.WriterAt = (*storage)(nil)
)

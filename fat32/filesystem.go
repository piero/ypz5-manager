package fat32

import "time"

type Entry struct {
	Name    string
	IsDir   bool
	Size    uint32
	ModTime time.Time
}

type Filesystem struct {
	device string
}

func New(device string) *Filesystem {
	return &Filesystem{
		device: device,
	}
}

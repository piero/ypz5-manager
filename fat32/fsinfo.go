package fat32

import (
	"encoding/binary"
	"fmt"
)

const (
	fsInfoLeadSignature   = 0x41615252
	fsInfoStructSignature = 0x61417272
	fsInfoTrailSignature  = 0xAA550000

	fsInfoFreeCountOffset = 0x1E8
	fsInfoNextFreeOffset  = 0x1EC

	fsInfoUnknown = 0xFFFFFFFF
)

// fsInfo mirrors the FSInfo sector, which caches the number of free
// clusters and a hint for where to start looking for one. Devices and
// operating systems trust these values, so they must be kept in step
// with the FAT.
type fsInfo struct {
	present   bool
	offset    int64
	freeCount uint32
	nextFree  uint32
	dirty     bool
}

// loadFSInfo reads the FSInfo sector. On a writable filesystem the free
// count is recomputed from the FAT, so a stale value is corrected on the
// next commit instead of being carried forward.
func (fs *Filesystem) loadFSInfo() error {
	fs.info = fsInfo{}

	sector := int64(fs.boot.FSInfoSector)

	if sector == 0 || sector >= int64(fs.boot.ReservedSectors) {
		return nil
	}

	offset := sector * int64(fs.boot.BytesPerSector)
	buf := make([]byte, sectorSize)

	if _, err := fs.reader.ReadAt(buf, offset); err != nil {
		return fmt.Errorf("read FSInfo sector: %w", err)
	}

	if binary.LittleEndian.Uint32(buf[0x000:]) != fsInfoLeadSignature ||
		binary.LittleEndian.Uint32(buf[0x1E4:]) != fsInfoStructSignature ||
		binary.LittleEndian.Uint32(buf[0x1FC:]) != fsInfoTrailSignature {
		return nil
	}

	fs.info = fsInfo{
		present:   true,
		offset:    offset,
		freeCount: binary.LittleEndian.Uint32(buf[fsInfoFreeCountOffset:]),
		nextFree:  binary.LittleEndian.Uint32(buf[fsInfoNextFreeOffset:]),
	}

	if fs.writer == nil {
		return nil
	}

	// Loading the FAT recomputes the free count.
	return fs.loadFAT()
}

// countFreeClusters counts free clusters in the loaded FAT.
func (fs *Filesystem) countFreeClusters() uint32 {
	var free uint32

	for _, value := range fs.fat.entries[2:] {
		if value == 0 {
			free++
		}
	}

	return free
}

// noteFATChange keeps the FSInfo free count in step with a FAT entry
// that changed from oldValue to newValue.
func (fs *Filesystem) noteFATChange(cluster, oldValue, newValue uint32) {
	if !fs.info.present || fs.info.freeCount == fsInfoUnknown {
		return
	}

	switch {
	case oldValue == 0 && newValue != 0:
		if fs.info.freeCount > 0 {
			fs.info.freeCount--
		}
		fs.info.nextFree = cluster
		fs.info.dirty = true

	case oldValue != 0 && newValue == 0:
		fs.info.freeCount++
		fs.info.dirty = true
	}
}

// FreeClusters returns the number of free clusters recorded in FSInfo.
// ok is false when the filesystem has no FSInfo or the count is unknown.
func (fs *Filesystem) FreeClusters() (count uint32, ok bool) {
	if !fs.info.present || fs.info.freeCount == fsInfoUnknown {
		return 0, false
	}

	return fs.info.freeCount, true
}

func (fs *Filesystem) writeFSInfo() error {
	if !fs.info.present || !fs.info.dirty {
		return nil
	}

	buf := make([]byte, sectorSize)

	if _, err := fs.reader.ReadAt(buf, fs.info.offset); err != nil {
		return fmt.Errorf("read FSInfo sector: %w", err)
	}

	binary.LittleEndian.PutUint32(buf[fsInfoFreeCountOffset:], fs.info.freeCount)
	binary.LittleEndian.PutUint32(buf[fsInfoNextFreeOffset:], fs.info.nextFree)

	n, err := fs.writer.WriteAt(buf, fs.info.offset)
	if err != nil {
		return fmt.Errorf("write FSInfo sector: %w", err)
	}

	if n != len(buf) {
		return fmt.Errorf(
			"short write of FSInfo sector: wrote %d of %d bytes",
			n,
			len(buf),
		)
	}

	fs.info.dirty = false

	return nil
}

package fat32

import (
	"encoding/binary"
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

var errDirectoryFull = errors.New("directory is full")

var errDirectoryNoContiguousSlots = errors.New(
	"directory has no contiguous free slots",
)

var errDirectoryEndReached = errors.New(
	"directory end reached before enough contiguous slots were found",
)

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
		false,
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

func (fs *Filesystem) ReplaceFile(path string, data []byte) error {
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

	record, err := fs.findDirectoryEntry(parentCluster, name)
	if err != nil {
		return err
	}

	if record == nil {
		return fmt.Errorf("file %q does not exist", path)
	}

	if record.Entry.IsDir {
		return fmt.Errorf("%q is a directory", path)
	}

	var oldChain []uint32

	if record.Entry.StartCluster != 0 {
		oldChain, err = fs.readClusterChain(record.Entry.StartCluster)
		if err != nil {
			return err
		}
	}

	newChain, err := fs.writeNewFileData(data)
	if err != nil {
		return err
	}

	var newStartCluster uint32

	if len(newChain) > 0 {
		newStartCluster = newChain[0]
	}

	shortClusterData, err := fs.readCluster(record.Short.Cluster)
	if err != nil {
		if len(newChain) > 0 {
			rollbackErr := fs.freeClusterChain(newChain[0])
			return errors.Join(err, rollbackErr)
		}

		return err
	}

	shortOffset := int(record.Short.Offset)

	if shortOffset < 0 ||
		shortOffset+32 > len(shortClusterData) {
		if len(newChain) > 0 {
			rollbackErr := fs.freeClusterChain(newChain[0])
			return errors.Join(
				fmt.Errorf(
					"directory entry offset %d is outside cluster %d",
					record.Short.Offset,
					record.Short.Cluster,
				),
				rollbackErr,
			)
		}

		return fmt.Errorf(
			"directory entry offset %d is outside cluster %d",
			record.Short.Offset,
			record.Short.Cluster,
		)
	}

	var shortEntry [32]byte

	copy(
		shortEntry[:],
		shortClusterData[shortOffset:shortOffset+32],
	)

	binary.LittleEndian.PutUint16(
		shortEntry[0x14:0x16],
		uint16(newStartCluster>>16),
	)

	binary.LittleEndian.PutUint16(
		shortEntry[0x1A:0x1C],
		uint16(newStartCluster),
	)

	binary.LittleEndian.PutUint32(
		shortEntry[0x1C:0x20],
		uint32(len(data)),
	)

	if err := fs.writeDirectoryEntry(
		record.Short,
		shortEntry,
	); err != nil {
		if len(newChain) > 0 {
			rollbackErr := fs.freeClusterChain(newChain[0])
			return errors.Join(err, rollbackErr)
		}

		return err
	}

	for _, cluster := range oldChain {
		if err := fs.writeFATEntry(cluster, 0); err != nil {
			return fmt.Errorf(
				"free old cluster %d: %w",
				cluster,
				err,
			)
		}
	}

	return nil
}

func (fs *Filesystem) Mkdir(path string) error {
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
		return fmt.Errorf(
			"file or directory %q already exists",
			path,
		)
	}

	cluster, err := fs.allocateCluster()
	if err != nil {
		return err
	}

	rollback := func(original error) error {
		if rollbackErr := fs.writeFATEntry(cluster, 0); rollbackErr != nil {
			return errors.Join(original, rollbackErr)
		}

		return original
	}

	directoryData := make([]byte, fs.layout.BytesPerCluster)

	dotEntry, err := encodeSpecialDirectoryEntry(
		".",
		cluster,
	)
	if err != nil {
		return rollback(err)
	}

	dotDotCluster := parentCluster

	if parentCluster == fs.boot.RootCluster {
		dotDotCluster = 0
	}

	dotDotEntry, err := encodeSpecialDirectoryEntry(
		"..",
		dotDotCluster,
	)
	if err != nil {
		return rollback(err)
	}

	copy(directoryData[0:32], dotEntry[:])
	copy(directoryData[32:64], dotDotEntry[:])

	if err := fs.writeCluster(
		cluster,
		directoryData,
	); err != nil {
		return rollback(err)
	}

	if err := fs.createDirectoryRecord(
		parentCluster,
		name,
		cluster,
		0,
		true,
	); err != nil {
		return rollback(err)
	}

	return nil
}

func (fs *Filesystem) findFreeDirectorySlots(
	startCluster uint32,
	count int,
) ([]directoryEntrySlot, error) {
	if count <= 0 {
		return nil, fmt.Errorf("invalid slot count: %d", count)
	}

	clusters, err := fs.readClusterChain(startCluster)
	if err != nil {
		return nil, err
	}

	var slots []directoryEntrySlot
	endOfDirectory := false
	foundFree := false

	for _, cluster := range clusters {
		data, err := fs.readCluster(cluster)
		if err != nil {
			return nil, err
		}

		for offset := 0; offset < len(data); offset += 32 {
			entry := data[offset : offset+32]

			if endOfDirectory {
				slots = append(slots, directoryEntrySlot{
					Cluster: cluster,
					Offset:  uint32(offset),
				})

				if len(slots) == count {
					return slots, nil
				}

				continue
			}

			if entry[0] == 0x00 {
				endOfDirectory = true
				foundFree = true

				slots = append(slots, directoryEntrySlot{
					Cluster: cluster,
					Offset:  uint32(offset),
				})

				if len(slots) == count {
					return slots, nil
				}

				continue
			}

			if entry[0] == 0xE5 {
				foundFree = true

				slots = append(slots, directoryEntrySlot{
					Cluster: cluster,
					Offset:  uint32(offset),
				})

				if len(slots) == count {
					return slots, nil
				}

				continue
			}

			slots = nil
		}
	}

	if endOfDirectory {
		return nil, fmt.Errorf(
			"%w: need %d contiguous entries",
			errDirectoryEndReached,
			count,
		)
	}

	if foundFree {
		return nil, fmt.Errorf(
			"%w: directory has no run of %d free entries",
			errDirectoryNoContiguousSlots,
			count,
		)
	}

	return nil, fmt.Errorf(
		"%w: need %d entries",
		errDirectoryFull,
		count,
	)
}

func (fs *Filesystem) expandDirectory(
	directoryCluster uint32,
	clusterCount int,
) error {
	if err := fs.writable(); err != nil {
		return err
	}

	if clusterCount <= 0 {
		return fmt.Errorf(
			"invalid directory expansion cluster count: %d",
			clusterCount,
		)
	}

	clusters, err := fs.readClusterChain(directoryCluster)
	if err != nil {
		return err
	}

	if len(clusters) == 0 {
		return fmt.Errorf("directory cluster chain is empty")
	}

	lastCluster := clusters[len(clusters)-1]

	newChain, err := fs.allocateClusterChain(clusterCount)
	if err != nil {
		return err
	}

	// Link the existing directory chain to the new clusters.
	if err := fs.writeFATEntry(lastCluster, newChain[0]); err != nil {
		if rollbackErr := fs.freeClusterChain(newChain[0]); rollbackErr != nil {
			return errors.Join(err, rollbackErr)
		}

		return err
	}

	// The clusters returned by allocateClusterChain are already
	// linked together. Initialize every new directory cluster.
	for _, cluster := range newChain {
		data := make([]byte, fs.layout.BytesPerCluster)

		if err := fs.writeCluster(cluster, data); err != nil {
			// Restore the old directory chain.
			rollbackErr := fs.writeFATEntry(
				lastCluster,
				fat32EOF,
			)

			freeErr := fs.freeClusterChain(newChain[0])

			return errors.Join(
				err,
				rollbackErr,
				freeErr,
			)
		}
	}

	return nil
}




// createDirectoryRecord behaviour:
// directory completely full
//
//	│
//	└── expand
//
// directory has 0x00 but insufficient slots before cluster end
//
//	│
//	└── expand
//
// directory has scattered deleted slots, no 0x00
//
//	│
//	└── return errDirectoryNoContiguousSlots
func (fs *Filesystem) createDirectoryRecord(
	directoryCluster uint32,
	name string,
	startCluster uint32,
	size uint32,
	isDir bool,
) error {
	if err := fs.writable(); err != nil {
		return err
	}

	var shortName [11]byte

	if isShortName(name) {
		var err error

		shortName, err = encodeShortName(name)
		if err != nil {
			return err
		}
	} else {
		var err error

		shortName, err = fs.generateUniqueShortName(
			directoryCluster,
			name,
		)
		if err != nil {
			return err
		}
	}

	record, err := encodeDirectoryRecordWithShortName(
		name,
		shortName,
		startCluster,
		size,
		isDir,
	)
	if err != nil {
		return err
	}

	slotCount := len(record.LFN) + 1

	slots, err := fs.findFreeDirectorySlots(
		directoryCluster,
		slotCount,
	)
	if err != nil {
		if !errors.Is(err, errDirectoryFull) &&
			!errors.Is(err, errDirectoryEndReached) {
			return err
		}

		entriesPerCluster := int(
			fs.layout.BytesPerCluster / 32,
		)

		clustersNeeded := (slotCount + entriesPerCluster - 1) /
			entriesPerCluster

		if err := fs.expandDirectory(
			directoryCluster,
			clustersNeeded,
		); err != nil {
			return err
		}

		slots, err = fs.findFreeDirectorySlots(
			directoryCluster,
			slotCount,
		)
		if err != nil {
			return err
		}
	}

	return fs.writeEncodedDirectoryRecord(
		record,
		slots,
	)
}

package fat32

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// ErrProtected is returned when an operation would modify or remove a
// read-only or system entry, such as the YP-Z5's System directory.
var ErrProtected = errors.New("read-only or system entry")

func (fs *Filesystem) Delete(path string) (err error) {
	if err := fs.writable(); err != nil {
		return err
	}

	defer fs.commit(&err)

	return fs.deleteFile(path)
}

func (fs *Filesystem) deleteFile(path string) error {
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

	if isProtected(record.Entry) {
		return fmt.Errorf("delete %q: %w", path, ErrProtected)
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
	if len(chain) > 0 {
		return fs.freeClusters(chain)
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
func (fs *Filesystem) WriteFile(path string, data []byte) (err error) {
	if err := fs.writable(); err != nil {
		return err
	}

	defer fs.commit(&err)

	return fs.writeFile(path, data)
}

func (fs *Filesystem) writeFile(path string, data []byte) error {
	if err := fs.writable(); err != nil {
		return err
	}

	parentPath, name, err := splitFilePath(path)
	if err != nil {
		return err
	}

	if err := validateLongName(name); err != nil {
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

func (fs *Filesystem) ReplaceFile(path string, data []byte) (err error) {
	if err := fs.writable(); err != nil {
		return err
	}

	defer fs.commit(&err)

	return fs.replaceFile(path, data)
}

func (fs *Filesystem) replaceFile(path string, data []byte) error {
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

	if isProtected(record.Entry) {
		return fmt.Errorf("replace %q: %w", path, ErrProtected)
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

	stampModified(&shortEntry, fs.now())

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

	if len(oldChain) > 0 {
		return fs.freeClusters(oldChain)
	}

	return nil
}

func (fs *Filesystem) Mkdir(path string) (err error) {
	if err := fs.writable(); err != nil {
		return err
	}

	defer fs.commit(&err)

	return fs.mkdir(path)
}

func (fs *Filesystem) mkdir(path string) error {
	if err := fs.writable(); err != nil {
		return err
	}

	parentPath, name, err := splitFilePath(path)
	if err != nil {
		return err
	}

	if err := validateLongName(name); err != nil {
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

	now := fs.now()
	stampCreated(&dotEntry, now)
	stampCreated(&dotDotEntry, now)

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

func (fs *Filesystem) RemoveAll(path string) (err error) {
	if err := fs.writable(); err != nil {
		return err
	}

	defer fs.commit(&err)

	if err := fs.checkRemovable(path); err != nil {
		return err
	}

	return fs.removeAll(path)
}

// checkRemovable walks path and fails with ErrProtected if it is, or
// contains, a protected entry, so RemoveAll never deletes part of a tree
// and then stops.
func (fs *Filesystem) checkRemovable(path string) error {
	record, err := fs.findDirectoryRecord(path)
	if err != nil {
		return err
	}

	return fs.checkRemovableEntry(path, record.Entry)
}

func (fs *Filesystem) checkRemovableEntry(path string, entry DirectoryEntry) error {
	if isProtected(entry) {
		return fmt.Errorf("remove %q: %w", path, ErrProtected)
	}

	if !entry.IsDir {
		return nil
	}

	children, err := fs.readDirectory(entry.StartCluster)
	if err != nil {
		return err
	}

	for _, child := range children {
		if child.Name == "." || child.Name == ".." {
			continue
		}

		if err := fs.checkRemovableEntry(path+"/"+child.Name, child); err != nil {
			return err
		}
	}

	return nil
}

func (fs *Filesystem) removeAll(path string) error {
	if err := fs.writable(); err != nil {
		return err
	}

	record, err := fs.findDirectoryRecord(path)
	if err != nil {
		return err
	}

	if !record.Entry.IsDir {
		return fs.deleteFile(path)
	}

	// Never allow the filesystem root to be removed.
	if path == "/" {
		return fmt.Errorf("cannot remove root directory")
	}

	entries, err := fs.readDirectoryRecords(record.Entry.StartCluster)
	if err != nil {
		return err
	}

	for _, child := range entries {
		switch child.Entry.Name {
		case ".", "..":
			continue
		}

		childPath := path
		if childPath != "/" {
			childPath += "/"
		}
		childPath += child.Entry.Name

		if err := fs.removeAll(childPath); err != nil {
			return err
		}
	}

	// The directory is now empty, so remove its directory record.
	if err := fs.deleteDirectoryRecord(record); err != nil {
		return err
	}

	// Finally release the directory's own cluster chain.
	if record.Entry.StartCluster != 0 {
		chain, err := fs.readClusterChain(record.Entry.StartCluster)
		if err != nil {
			return err
		}

		if err := fs.freeClusters(chain); err != nil {
			return err
		}
	}

	return nil
}

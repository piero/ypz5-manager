package fat32

import (
	"errors"
	"fmt"
)

var errDirectoryFull = errors.New("directory is full")

var errDirectoryNoContiguousSlots = errors.New(
	"directory has no contiguous free slots",
)

var errDirectoryEndReached = errors.New(
	"directory end reached before enough contiguous slots were found",
)

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

	rollback := func(original error) error {
		var rollbackErr error

		if err := fs.freeClusterChain(newChain[0]); err != nil {
			rollbackErr = errors.Join(rollbackErr, err)
		}

		return errors.Join(original, rollbackErr)
	}

	if err := fs.writeFATEntry(lastCluster, newChain[0]); err != nil {
		return rollback(err)
	}

	for _, cluster := range newChain {
		data := make([]byte, fs.layout.BytesPerCluster)

		if err := fs.writeCluster(cluster, data); err != nil {
			var rollbackErr error

			if err := fs.writeFATEntry(lastCluster, fat32EOF); err != nil {
				rollbackErr = errors.Join(rollbackErr, err)
			}

			if err := fs.freeClusterChain(newChain[0]); err != nil {
				rollbackErr = errors.Join(rollbackErr, err)
			}

			return errors.Join(err, rollbackErr)
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

	stampCreated(&record.Short, fs.now())

	slotCount := len(record.LFN) + 1

	slots, err := fs.findFreeDirectorySlots(
		directoryCluster,
		slotCount,
	)
	if err == nil {
		return fs.writeEncodedDirectoryRecord(
			record,
			slots,
		)
	}

	if !errors.Is(err, errDirectoryFull) &&
		!errors.Is(err, errDirectoryEndReached) {
		return err
	}

	clusters, err := fs.readClusterChain(directoryCluster)
	if err != nil {
		return err
	}

	if len(clusters) == 0 {
		return fmt.Errorf("directory cluster chain is empty")
	}

	lastCluster := clusters[len(clusters)-1]

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

	expandedClusters, err := fs.readClusterChain(directoryCluster)
	if err != nil {
		return err
	}

	if len(expandedClusters) <= len(clusters) {
		return fmt.Errorf(
			"directory expansion did not add clusters",
		)
	}

	newStartCluster := expandedClusters[len(clusters)]

	rollbackExpansion := func(original error) error {
		var rollbackErr error

		if err := fs.writeFATEntry(lastCluster, fat32EOF); err != nil {
			rollbackErr = errors.Join(rollbackErr, err)
			return errors.Join(original, rollbackErr)
		}

		if err := fs.freeClusterChain(newStartCluster); err != nil {
			rollbackErr = errors.Join(rollbackErr, err)
		}

		return errors.Join(original, rollbackErr)
	}

	slots, err = fs.findFreeDirectorySlots(
		directoryCluster,
		slotCount,
	)
	if err != nil {
		return rollbackExpansion(err)
	}

	if err := fs.writeEncodedDirectoryRecord(
		record,
		slots,
	); err != nil {
		return rollbackExpansion(err)
	}

	return nil
}

func (fs *Filesystem) writeDirectoryEntry(
	location directoryEntryLocation,
	data [32]byte,
) error {
	if err := fs.writable(); err != nil {
		return err
	}

	if location.Offset%32 != 0 {
		return fmt.Errorf("invalid directory entry offset: %d", location.Offset)
	}

	clusterData, err := fs.readCluster(location.Cluster)
	if err != nil {
		return err
	}

	if int(location.Offset)+32 > len(clusterData) {
		return fmt.Errorf(
			"directory entry offset %d is outside cluster %d",
			location.Offset,
			location.Cluster,
		)
	}

	copy(clusterData[location.Offset:location.Offset+32], data[:])

	return fs.writeCluster(location.Cluster, clusterData)
}

// Validate all slots first.
// Read and save the affected clusters.
// Perform the writes.
// If any write fails, restore the original cluster contents.
func (fs *Filesystem) writeEncodedDirectoryRecord(
	record encodedDirectoryRecord,
	slots []directoryEntrySlot,
) error {
	if err := fs.writable(); err != nil {
		return err
	}

	required := len(record.LFN) + 1
	if len(slots) != required {
		return fmt.Errorf(
			"directory record requires %d slots, got %d",
			required,
			len(slots),
		)
	}

	for i := 1; i < len(slots); i++ {
		prev := slots[i-1]
		curr := slots[i]

		if curr.Cluster == prev.Cluster {
			if curr.Offset != prev.Offset+32 {
				return fmt.Errorf(
					"directory slots are not contiguous at index %d",
					i,
				)
			}
			continue
		}

		if prev.Offset != uint32(fs.layout.BytesPerCluster-32) ||
			curr.Offset != 0 {
			return fmt.Errorf(
				"directory slots are not contiguous at index %d",
				i,
			)
		}

		nextCluster, err := fs.readFATEntry(prev.Cluster)
		if err != nil {
			return err
		}

		if nextCluster != curr.Cluster {
			return fmt.Errorf(
				"directory slots are not contiguous at index %d",
				i,
			)
		}
	}

	originalClusters := make(map[uint32][]byte)

	for _, slot := range slots {
		if _, exists := originalClusters[slot.Cluster]; exists {
			continue
		}

		data, err := fs.readCluster(slot.Cluster)
		if err != nil {
			return err
		}

		originalClusters[slot.Cluster] = data
	}

	restore := func() error {
		var restoreErr error

		for cluster, data := range originalClusters {
			if err := fs.writeCluster(cluster, data); err != nil {
				restoreErr = errors.Join(restoreErr, err)
			}
		}

		return restoreErr
	}

	for i, entry := range record.LFN {
		location := directoryEntryLocation{
			Cluster: slots[i].Cluster,
			Offset:  slots[i].Offset,
		}

		if err := fs.writeDirectoryEntry(location, entry); err != nil {
			return errors.Join(err, restore())
		}
	}

	shortLocation := directoryEntryLocation{
		Cluster: slots[len(record.LFN)].Cluster,
		Offset:  slots[len(record.LFN)].Offset,
	}

	if err := fs.writeDirectoryEntry(shortLocation, record.Short); err != nil {
		return errors.Join(err, restore())
	}

	return nil
}

// read whole sector -> change one byte in memory -> write whole sector
func (fs *Filesystem) markDirectoryEntryDeleted(
	location directoryEntryLocation,
) error {
	if err := fs.writable(); err != nil {
		return err
	}

	if location.Offset%32 != 0 {
		return fmt.Errorf(
			"invalid directory entry offset: %d",
			location.Offset,
		)
	}

	sectorSize := int64(fs.boot.BytesPerSector)

	offset := fs.layout.DataOffset +
		int64(location.Cluster-2)*fs.layout.BytesPerCluster +
		int64(location.Offset)

	sectorOffset := (offset / sectorSize) * sectorSize
	entryOffset := offset - sectorOffset

	buf := make([]byte, sectorSize)

	if _, err := fs.reader.ReadAt(buf, sectorOffset); err != nil {
		return fmt.Errorf(
			"read directory sector at cluster %d offset %d: %w",
			location.Cluster,
			location.Offset,
			err,
		)
	}

	buf[entryOffset] = 0xE5

	n, err := fs.writer.WriteAt(buf, sectorOffset)
	if err != nil {
		return fmt.Errorf(
			"write directory sector at cluster %d offset %d: %w",
			location.Cluster,
			location.Offset,
			err,
		)
	}

	if n != len(buf) {
		return fmt.Errorf(
			"short write of directory sector at cluster %d offset %d: wrote %d of %d bytes",
			location.Cluster,
			location.Offset,
			n,
			len(buf),
		)
	}

	return nil
}

func (fs *Filesystem) deleteDirectoryRecord(
	record directoryRecord,
) error {
	if err := fs.writable(); err != nil {
		return err
	}

	locations := make([]directoryEntryLocation, 0, len(record.LFN)+1)
	locations = append(locations, record.LFN...)
	locations = append(locations, record.Short)

	originalClusters := make(map[uint32][]byte)

	for _, location := range locations {
		if _, exists := originalClusters[location.Cluster]; exists {
			continue
		}

		data, err := fs.readCluster(location.Cluster)
		if err != nil {
			return err
		}

		originalClusters[location.Cluster] = data
	}

	restore := func() error {
		var restoreErr error

		for cluster, data := range originalClusters {
			if err := fs.writeCluster(cluster, data); err != nil {
				restoreErr = errors.Join(restoreErr, err)
			}
		}

		return restoreErr
	}

	for _, location := range locations {
		if err := fs.markDirectoryEntryDeleted(location); err != nil {
			return errors.Join(err, restore())
		}
	}

	return nil
}

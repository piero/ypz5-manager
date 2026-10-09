package fat32

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
)

const (
	fat32BadCluster = 0x0FFFFFF7
	fat32EOF        = 0x0FFFFFF8
)

// fatCache is an in-memory copy of the first FAT. It is loaded on first
// use and updated after every successful FAT write, so lookups and free
// cluster searches never touch the disk. Writes still go to the disk
// immediately (to every FAT copy).
type fatCache struct {
	loaded bool

	// entries holds the 28-bit value of each FAT entry, indexed by
	// cluster number, for clusters 0..MaxCluster.
	entries []uint32

	// lowestFree is a lower bound for the first free cluster: no
	// cluster below it is free.
	lowestFree uint32
}

// loadFAT reads the first FAT into memory if it is not loaded yet.
func (fs *Filesystem) loadFAT() error {
	if fs.fat.loaded {
		return nil
	}

	sectorSize := int64(fs.boot.BytesPerSector)
	if sectorSize == 0 {
		return fmt.Errorf("load FAT: invalid bytes per sector")
	}

	// The cache covers every cluster that both exists on the volume and
	// has an entry in the FAT; either limit may be unknown in a partial
	// geometry.
	count := int64(-1)

	if fs.boot.SectorsPerCluster != 0 && fs.boot.TotalSectors != 0 {
		count = int64(fs.boot.MaxCluster()) + 1
	}

	if fs.boot.SectorsPerFAT != 0 {
		fatEntries := int64(fs.boot.SectorsPerFAT) * sectorSize / 4

		if count < 0 || fatEntries < count {
			count = fatEntries
		}
	}

	if count < 0 {
		return fmt.Errorf("load FAT: unknown FAT size")
	}

	size := (count*4 + sectorSize - 1) / sectorSize * sectorSize

	buf := make([]byte, size)

	if _, err := fs.reader.ReadAt(buf, fs.layout.FATOffset); err != nil {
		return fmt.Errorf("read FAT: %w", err)
	}

	entries := make([]uint32, count)

	for i := range entries {
		entries[i] = binary.LittleEndian.Uint32(buf[i*4:]) & 0x0FFFFFFF
	}

	fs.fat = fatCache{
		loaded:     true,
		entries:    entries,
		lowestFree: 2,
	}

	// Keep FSInfo exact on a writable filesystem, also when the cache
	// is reloaded after a failed write.
	if fs.info.present && fs.writer != nil {
		free := fs.countFreeClusters()

		if free != fs.info.freeCount {
			fs.info.freeCount = free
			fs.info.dirty = true
		}
	}

	return nil
}

// maxFATCluster returns the highest cluster number in the loaded FAT.
func (fs *Filesystem) maxFATCluster() uint32 {
	return uint32(len(fs.fat.entries) - 1)
}

func (fs *Filesystem) readFATEntry(cluster uint32) (uint32, error) {
	if err := fs.loadFAT(); err != nil {
		return 0, err
	}

	if cluster > fs.maxFATCluster() {
		return 0, fmt.Errorf(
			"cluster %d is beyond the end of the volume",
			cluster,
		)
	}

	return fs.fat.entries[cluster], nil
}

func (fs *Filesystem) readClusterChain(start uint32) ([]uint32, error) {
	/*
		If start = 2

		we do:

			2
			│
			├── FAT[2] → 3
			│
			3
			│
			├── FAT[3] → 4
			│
			4
			│
			├── FAT[4] → 5
			│
			5
			│
			└── FAT[5] → EOF

		and return []uint32{2, 3, 4, 5}

	*/
	if start < 2 {
		return nil, fmt.Errorf("invalid start cluster: %d", start)
	}

	var chain []uint32
	visited := make(map[uint32]bool)

	cluster := start

	for {
		if visited[cluster] {
			return nil, fmt.Errorf(
				"cycle detected at cluster %d",
				cluster,
			)
		}

		visited[cluster] = true
		chain = append(chain, cluster)

		next, err := fs.readFATEntry(cluster)
		if err != nil {
			return nil, err
		}

		switch {
		case next >= fat32EOF:
			return chain, nil

		case next == fat32BadCluster:
			return nil, fmt.Errorf(
				"bad cluster in chain after cluster %d",
				cluster,
			)

		case next < 2:
			return nil, fmt.Errorf(
				"invalid cluster %d after cluster %d",
				next,
				cluster,
			)

		case next > fs.maxFATCluster():
			return nil, fmt.Errorf(
				"cluster %d after cluster %d is beyond the end of the volume",
				next,
				cluster,
			)

		default:
			cluster = next
		}
	}
}

// FAT entry = 0          → free
// FAT entry = 2..        → allocated / part of a chain
// FAT entry >= 0x0FFFFFF8 → end of chain
// 0x0FFFFFF7             → bad cluster
func (fs *Filesystem) IsFreeCluster(cluster uint32) (bool, error) {
	value, err := fs.readFATEntry(cluster)
	if err != nil {
		return false, err
	}

	// We've already masked the FAT entry to 28 bits in readFATEntry()
	// so this method can simply check for zero.
	return value == 0, nil
}

func (fs *Filesystem) FindFreeCluster(start uint32) (uint32, error) {
	clusters, err := fs.findFreeClusters(start, 1)
	if err != nil {
		return 0, err
	}

	return clusters[0], nil
}

// findFreeClusters returns the count lowest free clusters at or above
// start, without allocating them.
func (fs *Filesystem) findFreeClusters(start uint32, count int) ([]uint32, error) {
	if err := fs.loadFAT(); err != nil {
		return nil, err
	}

	if start < fs.fat.lowestFree {
		start = fs.fat.lowestFree
	}

	if start < 2 {
		start = 2
	}

	clusters := make([]uint32, 0, count)
	maxCluster := fs.maxFATCluster()

	for cluster := start; cluster <= maxCluster; cluster++ {
		if fs.fat.entries[cluster] != 0 {
			continue
		}

		if len(clusters) == 0 && start == fs.fat.lowestFree {
			fs.fat.lowestFree = cluster
		}

		clusters = append(clusters, cluster)

		if len(clusters) == count {
			return clusters, nil
		}
	}

	if len(clusters) == 0 && start <= fs.fat.lowestFree {
		fs.fat.lowestFree = maxCluster + 1
	}

	if count == 1 {
		return nil, fmt.Errorf("no free clusters")
	}

	return nil, fmt.Errorf(
		"not enough free clusters: need %d, found %d",
		count,
		len(clusters),
	)
}

func (fs *Filesystem) allocateCluster() (uint32, error) {
	chain, err := fs.allocateClusterChain(1)
	if err != nil {
		return 0, err
	}

	return chain[0], nil
}

// allocateClusterChain allocates count free clusters, linked in order
// and terminated with EOF. The FAT is written in one batch: either the
// whole chain is allocated, or (after a failed write) every FAT sector
// is restored and nothing is allocated.
func (fs *Filesystem) allocateClusterChain(count int) ([]uint32, error) {
	if err := fs.writable(); err != nil {
		return nil, err
	}

	if count <= 0 {
		return nil, fmt.Errorf("invalid cluster count: %d", count)
	}

	chain, err := fs.findFreeClusters(2, count)
	if err != nil {
		return nil, err
	}

	updates := make(map[uint32]uint32, count)

	for i, cluster := range chain {
		if i < len(chain)-1 {
			updates[cluster] = chain[i+1]
		} else {
			updates[cluster] = fat32EOF
		}
	}

	if err := fs.writeFATEntries(updates); err != nil {
		return nil, fmt.Errorf(
			"allocate %d clusters: %w",
			count,
			err,
		)
	}

	return chain, nil
}

func (fs *Filesystem) ClusterChain(start uint32) ([]uint32, error) {
	return fs.readClusterChain(start)
}

func (fs *Filesystem) writeFATEntry(
	cluster uint32,
	value uint32,
) error {
	return fs.writeFATEntries(map[uint32]uint32{cluster: value})
}

// writeFATEntries sets several FAT entries, writing each affected FAT
// sector once per FAT copy. If a write fails, the sectors already
// written are restored, and the cache is left unchanged.
func (fs *Filesystem) writeFATEntries(updates map[uint32]uint32) error {
	if err := fs.writable(); err != nil {
		return err
	}

	if len(updates) == 0 {
		return nil
	}

	if err := fs.loadFAT(); err != nil {
		return err
	}

	sectorSize := int64(fs.boot.BytesPerSector)

	// Group the updates by FAT sector, in disk order.
	bySector := make(map[int64][]uint32)

	for cluster := range updates {
		if cluster < 2 || cluster > fs.maxFATCluster() {
			return fmt.Errorf("invalid FAT cluster: %d", cluster)
		}

		sector := int64(cluster) * 4 / sectorSize
		bySector[sector] = append(bySector[sector], cluster)
	}

	sectors := make([]int64, 0, len(bySector))

	for sector := range bySector {
		sectors = append(sectors, sector)
	}

	sort.Slice(sectors, func(i, j int) bool { return sectors[i] < sectors[j] })

	type fatSector struct {
		offset int64
		old    []byte
		new    []byte
	}

	pending := make([]fatSector, 0, len(sectors)*int(fs.boot.NumberOfFATs))
	previous := make(map[uint32]uint32, len(updates))

	// Prepare every FAT sector before writing anything.
	for fat := uint8(0); fat < fs.boot.NumberOfFATs; fat++ {
		fatOffset := fs.layout.FATOffset +
			int64(fat)*int64(fs.boot.SectorsPerFAT)*sectorSize

		for _, sector := range sectors {
			offset := fatOffset + sector*sectorSize
			buf := make([]byte, sectorSize)

			if _, err := fs.reader.ReadAt(buf, offset); err != nil {
				return fmt.Errorf(
					"read FAT sector at offset %d: %w",
					offset,
					err,
				)
			}

			old := append([]byte(nil), buf...)

			for _, cluster := range bySector[sector] {
				entryOffset := int64(cluster)*4 - sector*sectorSize
				entry := buf[entryOffset : entryOffset+4]

				oldValue := binary.LittleEndian.Uint32(entry)

				if fat == 0 {
					previous[cluster] = oldValue & 0x0FFFFFFF
				}

				binary.LittleEndian.PutUint32(
					entry,
					(oldValue&0xF0000000)|(updates[cluster]&0x0FFFFFFF),
				)
			}

			pending = append(pending, fatSector{
				offset: offset,
				old:    old,
				new:    buf,
			})
		}
	}

	for i, sector := range pending {
		n, err := fs.writer.WriteAt(sector.new, sector.offset)
		if err == nil && n != len(sector.new) {
			err = fmt.Errorf(
				"short write: wrote %d of %d bytes",
				n,
				len(sector.new),
			)
		}

		if err == nil {
			continue
		}

		err = fmt.Errorf(
			"write FAT sector at offset %d: %w",
			sector.offset,
			err,
		)

		// Restore this sector too: a failed write may be partial.
		var rollbackErr error

		for j := i; j >= 0; j-- {
			restore := pending[j]

			rn, rerr := fs.writer.WriteAt(restore.old, restore.offset)
			if rerr == nil && rn != len(restore.old) {
				rerr = fmt.Errorf(
					"short write: wrote %d of %d bytes",
					rn,
					len(restore.old),
				)
			}

			if rerr != nil {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf(
					"rollback FAT sector at offset %d: %w",
					restore.offset,
					rerr,
				))
			}
		}

		if rollbackErr != nil {
			// The disk may no longer match the cache: reload it (and
			// recount free clusters) on next use.
			fs.fat = fatCache{}

			return fmt.Errorf("%w; rollback failed: %v", err, rollbackErr)
		}

		return err
	}

	for cluster, value := range updates {
		value &= 0x0FFFFFFF
		fs.fat.entries[cluster] = value

		if value == 0 && cluster < fs.fat.lowestFree {
			fs.fat.lowestFree = cluster
		}

		fs.noteFATChange(cluster, previous[cluster], value)
	}

	return nil
}

// freeClusters marks clusters as free in one batch.
func (fs *Filesystem) freeClusters(clusters []uint32) error {
	updates := make(map[uint32]uint32, len(clusters))

	for _, cluster := range clusters {
		updates[cluster] = 0
	}

	if err := fs.writeFATEntries(updates); err != nil {
		return fmt.Errorf("free %d clusters: %w", len(clusters), err)
	}

	return nil
}

func (fs *Filesystem) freeClusterChain(start uint32) error {
	if err := fs.writable(); err != nil {
		return err
	}

	chain, err := fs.readClusterChain(start)
	if err != nil {
		return err
	}

	return fs.freeClusters(chain)
}

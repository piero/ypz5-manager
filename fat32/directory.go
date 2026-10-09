package fat32

import (
	"fmt"
)

func (fs *Filesystem) readDirectoryRecords(startCluster uint32) ([]directoryRecord, error) {
	clusters, err := fs.readClusterChain(startCluster)
	if err != nil {
		return nil, err
	}

	var records []directoryRecord
	var lfn []lfnEntry

	for _, cluster := range clusters {
		data, err := fs.readCluster(cluster)
		if err != nil {
			return nil, err
		}

		for offset := 0; offset < len(data); offset += 32 {
			entry := data[offset : offset+32]

			if entry[0] == 0x00 {
				return records, nil
			}

			if entry[0] == 0xE5 {
				lfn = nil
				continue
			}

			location := directoryEntryLocation{
				Cluster: cluster,
				Offset:  uint32(offset),
			}

			if entry[0x0B] == 0x0F {
				name, err := parseLFNEntry(entry)
				if err != nil {
					// A damaged LFN entry invalidates its run; the
					// file is still listed under its 8.3 name.
					lfn = nil
					continue
				}

				// The last-entry flag marks the first entry on disk of
				// a new LFN run: anything collected so far is orphaned.
				if name.IsLast {
					lfn = nil
				}

				name.EntryCluster = cluster
				name.EntryOffset = uint32(offset)

				lfn = append(lfn, name)
				continue
			}

			// Volume-label entries are filesystem metadata, not files
			// or directories exposed through the directory API.
			if entry[0x0B]&0x08 != 0 {
				lfn = nil
				continue
			}

			dirEntry, err := parseDirectoryEntry(entry)
			if err != nil {
				return nil, err
			}

			// Like other FAT implementations, an LFN run that is
			// damaged or belongs to another entry is ignored and the
			// 8.3 name is used, rather than failing the directory.
			if len(lfn) > 0 && !lfnBelongsTo(lfn, entry) {
				lfn = nil
			}

			if len(lfn) > 0 {
				dirEntry.Name = combineLFN(lfn)
				dirEntry.LFNEntries = uint8(len(lfn))
			} else {
				dirEntry.Name = parseShortNameWithCase(entry)
			}

			dirEntry.EntryCluster = cluster
			dirEntry.EntryOffset = uint32(offset)

			record := directoryRecord{
				Entry: dirEntry,
				Short: location,
			}

			for _, lfnEntry := range lfn {
				record.LFN = append(record.LFN, directoryEntryLocation{
					Cluster: lfnEntry.EntryCluster,
					Offset:  lfnEntry.EntryOffset,
				})
			}

			records = append(records, record)
			lfn = nil
		}
	}

	return records, nil
}

func (fs *Filesystem) readDirectory(startCluster uint32) ([]DirectoryEntry, error) {
	records, err := fs.readDirectoryRecords(startCluster)
	if err != nil {
		return nil, err
	}

	entries := make([]DirectoryEntry, 0, len(records))

	for _, record := range records {
		entries = append(entries, record.Entry)
	}

	return entries, nil
}

func (fs *Filesystem) ReadRootDirectory() ([]DirectoryEntry, error) {
	return fs.readDirectory(fs.boot.RootCluster)
}

func (fs *Filesystem) ReadDir(path string) ([]DirectoryEntry, error) {
	dir, err := fs.Find(path)
	if err != nil {
		return nil, err
	}

	entries, err := fs.readDirectory(dir.StartCluster)
	if err != nil {
		return nil, err
	}

	filtered := entries[:0]

	for _, entry := range entries {
		if entry.Name == "." || entry.Name == ".." {
			continue
		}

		filtered = append(filtered, entry)
	}

	return filtered, nil
}

// Find resolves path to a directory.
func (fs *Filesystem) Find(path string) (DirectoryEntry, error) {
	entry, err := fs.findEntry(path)
	if err != nil {
		return DirectoryEntry{}, err
	}

	if !entry.IsDir {
		return DirectoryEntry{}, fmt.Errorf(
			"%q is not a directory",
			path,
		)
	}

	return entry, nil
}

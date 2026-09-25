package fat32

import (
	"fmt"
	"strings"
)

func (fs *Filesystem) findEntry(path string) (DirectoryEntry, error) {
	if path == "/" {
		return DirectoryEntry{
			Name:         "/",
			IsDir:        true,
			StartCluster: fs.RootCluster(),
		}, nil
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")

	currentCluster := fs.RootCluster()

	for i, part := range parts {
		entries, err := fs.readDirectory(currentCluster)
		if err != nil {
			return DirectoryEntry{}, err
		}

		found := false

		for _, entry := range entries {
			if entry.Name != part {
				continue
			}

			// Every component except the final one must be a directory.
			if i < len(parts)-1 && !entry.IsDir {
				return DirectoryEntry{}, fmt.Errorf(
					"%q is not a directory",
					part,
				)
			}

			// We found the requested entry.
			if i == len(parts)-1 {
				return entry, nil
			}

			currentCluster = entry.StartCluster
			found = true
			break
		}

		if !found {
			return DirectoryEntry{}, fmt.Errorf(
				"entry not found: %s",
				path,
			)
		}
	}

	return DirectoryEntry{}, fmt.Errorf("invalid path: %q", path)
}

func (fs *Filesystem) Lookup(path string) (DirectoryEntry, error) {
	return fs.findEntry(path)
}

func (fs *Filesystem) findDirectoryRecord(path string) (directoryRecord, error) {
	if path == "/" {
		return directoryRecord{}, fmt.Errorf("root directory has no directory entry")
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")

	currentCluster := fs.RootCluster()

	for i, part := range parts {
		records, err := fs.readDirectoryRecords(currentCluster)
		if err != nil {
			return directoryRecord{}, err
		}

		found := false

		for _, record := range records {
			if record.Entry.Name != part {
				continue
			}

			if i < len(parts)-1 {
				if !record.Entry.IsDir {
					return directoryRecord{}, fmt.Errorf(
						"%q is not a directory",
						part,
					)
				}

				currentCluster = record.Entry.StartCluster
				found = true
				break
			}

			return record, nil
		}

		if !found {
			return directoryRecord{}, fmt.Errorf(
				"entry not found: %s",
				path,
			)
		}
	}

	return directoryRecord{}, fmt.Errorf("invalid path: %q", path)
}

// func (fs *Filesystem) LookupRecord(path string) (directoryRecord, error) {
// 	return fs.findDirectoryRecord(path)
// }

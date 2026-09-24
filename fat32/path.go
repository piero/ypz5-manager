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

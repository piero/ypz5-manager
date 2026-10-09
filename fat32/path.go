package fat32

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNotFound is returned when a path does not exist.
var ErrNotFound = errors.New("entry not found")

func (fs *Filesystem) findEntry(path string) (DirectoryEntry, error) {
	if path == "/" {
		return DirectoryEntry{
			Name:         "/",
			IsDir:        true,
			StartCluster: fs.RootCluster(),
		}, nil
	}

	if path == "" {
		return DirectoryEntry{
			Name:         "/",
			IsDir:        true,
			StartCluster: fs.RootCluster(),
		}, nil
	}

	if strings.HasSuffix(path, "/") {
		return DirectoryEntry{}, fmt.Errorf(
			"path has trailing separator: %q",
			path,
		)
	}

	path = strings.TrimPrefix(path, "/")

	if path == "" {
		return DirectoryEntry{
			Name:         "/",
			IsDir:        true,
			StartCluster: fs.RootCluster(),
		}, nil
	}

	rawParts := strings.Split(path, "/")
	parts := make([]string, 0, len(rawParts))

	for _, part := range rawParts {
		if part == "" {
			return DirectoryEntry{}, fmt.Errorf(
				"path contains an empty component: %q",
				path,
			)
		}

		if part == "." {
			continue
		}

		parts = append(parts, part)
	}

	if len(parts) == 0 {
		return DirectoryEntry{
			Name:         "/",
			IsDir:        true,
			StartCluster: fs.RootCluster(),
		}, nil
	}

	currentCluster := fs.RootCluster()

	for i, part := range parts {
		if part == ".." {
			if currentCluster == fs.RootCluster() {
				continue
			}

			entries, err := fs.readDirectory(currentCluster)
			if err != nil {
				return DirectoryEntry{}, err
			}

			var parentCluster uint32
			found := false

			for _, entry := range entries {
				if !entry.IsDir ||
					!strings.EqualFold(entry.Name, "..") {
					continue
				}

				parentCluster = entry.StartCluster

				if parentCluster == 0 {
					parentCluster = fs.RootCluster()
				}

				found = true
				break
			}

			if !found {
				return DirectoryEntry{}, fmt.Errorf(
					"parent directory entry not found for cluster %d",
					currentCluster,
				)
			}

			currentCluster = parentCluster
			continue
		}

		entries, err := fs.readDirectory(currentCluster)
		if err != nil {
			return DirectoryEntry{}, err
		}

		found := false

		for _, entry := range entries {
			if !strings.EqualFold(entry.Name, part) {
				continue
			}

			if i < len(parts)-1 && !entry.IsDir {
				return DirectoryEntry{}, fmt.Errorf(
					"%q is not a directory",
					part,
				)
			}

			if i == len(parts)-1 {
				return entry, nil
			}

			currentCluster = entry.StartCluster
			found = true
			break
		}

		if !found {
			return DirectoryEntry{}, fmt.Errorf(
				"%w: %s",
				ErrNotFound,
				path,
			)
		}
	}

	return DirectoryEntry{}, fmt.Errorf(
		"invalid path: %q",
		path,
	)
}

// Lookup resolves path to a file or directory.
func (fs *Filesystem) Lookup(path string) (DirectoryEntry, error) {
	return fs.findEntry(path)
}

func (fs *Filesystem) findDirectoryRecord(
	path string,
) (directoryRecord, error) {
	if path == "/" {
		return directoryRecord{}, fmt.Errorf(
			"root directory has no directory entry",
		)
	}

	path = strings.Trim(path, "/")

	if path == "" {
		return directoryRecord{}, fmt.Errorf(
			"root directory has no directory entry",
		)
	}

	rawParts := strings.Split(path, "/")
	parts := make([]string, 0, len(rawParts))

	for _, part := range rawParts {
		if part == "" {
			return directoryRecord{}, fmt.Errorf(
				"path contains an empty component: %q",
				path,
			)
		}

		if part == "." {
			continue
		}

		parts = append(parts, part)
	}

	if len(parts) == 0 {
		return directoryRecord{}, fmt.Errorf(
			"root directory has no directory entry",
		)
	}

	currentCluster := fs.RootCluster()

	for i, part := range parts {
		if part == ".." {
			if currentCluster == fs.RootCluster() {
				continue
			}

			records, err := fs.readDirectoryRecords(currentCluster)
			if err != nil {
				return directoryRecord{}, err
			}

			var parentCluster uint32
			found := false

			for _, record := range records {
				if !record.Entry.IsDir ||
					!strings.EqualFold(record.Entry.Name, "..") {
					continue
				}

				parentCluster = record.Entry.StartCluster

				if parentCluster == 0 {
					parentCluster = fs.RootCluster()
				}

				found = true
				break
			}

			if !found {
				return directoryRecord{}, fmt.Errorf(
					"parent directory entry not found for cluster %d",
					currentCluster,
				)
			}

			currentCluster = parentCluster
			continue
		}

		records, err := fs.readDirectoryRecords(currentCluster)
		if err != nil {
			return directoryRecord{}, err
		}

		found := false

		for _, record := range records {
			if !strings.EqualFold(record.Entry.Name, part) {
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
				"%w: %s",
				ErrNotFound,
				path,
			)
		}
	}

	return directoryRecord{}, fmt.Errorf(
		"invalid path: %q",
		path,
	)
}

// splitFilePath splits a file path into its parent directory and filename.
func splitFilePath(path string) (string, string, error) {
	path = strings.TrimSpace(path)

	if path == "" {
		return "", "", fmt.Errorf("empty path")
	}

	if path == "/" {
		return "", "", fmt.Errorf(
			"path has no filename: %q",
			path,
		)
	}

	if strings.HasSuffix(path, "/") {
		return "", "", fmt.Errorf(
			"path has trailing separator: %q",
			path,
		)
	}

	index := strings.LastIndex(path, "/")

	if index < 0 {
		return ".", path, nil
	}

	parent := path[:index]
	name := path[index+1:]

	if parent == "" {
		parent = "/"
	} else {
		parentParts := strings.Split(parent, "/")

		for i, part := range parentParts {
			// The first component is empty for an absolute path,
			// e.g. "/Music/file.txt". That is valid.
			if i == 0 && part == "" {
				continue
			}

			if part == "" {
				return "", "", fmt.Errorf(
					"path contains an empty component: %q",
					path,
				)
			}
		}
	}

	if name == "" {
		return "", "", fmt.Errorf(
			"path has no filename: %q",
			path,
		)
	}

	return parent, name, nil
}

// findDirectoryCluster resolves the parent directory cluster from a path.
func (fs *Filesystem) findDirectoryCluster(path string) (uint32, error) {
	if path == "" || path == "." || path == "/" {
		return fs.boot.RootCluster, nil
	}

	path = strings.Trim(path, "/")

	if path == "" {
		return fs.boot.RootCluster, nil
	}

	currentCluster := fs.boot.RootCluster

	components := strings.Split(path, "/")

	for _, component := range components {
		if component == "" {
			return 0, fmt.Errorf(
				"path contains an empty component: %q",
				path,
			)
		}

		if component == "." {
			continue
		}

		if component == ".." &&
			currentCluster == fs.boot.RootCluster {
			continue
		}

		records, err := fs.readDirectoryRecords(currentCluster)
		if err != nil {
			return 0, err
		}

		var found *DirectoryEntry

		for i := range records {
			entry := &records[i].Entry

			if strings.EqualFold(entry.Name, component) {
				found = entry
				break
			}
		}

		if found == nil {
			return 0, fmt.Errorf(
				"directory %q not found: %w",
				component,
				ErrNotFound,
			)
		}

		if !found.IsDir {
			return 0, fmt.Errorf(
				"%q is not a directory",
				component,
			)
		}

		if component == ".." && found.StartCluster == 0 {
			currentCluster = fs.boot.RootCluster
			continue
		}

		currentCluster = found.StartCluster
	}

	return currentCluster, nil
}

func (fs *Filesystem) findDirectoryEntry(
	directoryCluster uint32,
	name string,
) (*directoryRecord, error) {
	records, err := fs.readDirectoryRecords(directoryCluster)
	if err != nil {
		return nil, err
	}

	for i := range records {
		if strings.EqualFold(records[i].Entry.Name, name) ||
			strings.EqualFold(records[i].Entry.ShortName, name) {
			return &records[i], nil
		}
	}

	return nil, nil
}

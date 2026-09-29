package fat32

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
)

// what the application sees
type DirectoryEntry struct {
	Name         string
	ShortName    string
	IsDir        bool
	Size         uint32
	StartCluster uint32

	EntryCluster uint32
	EntryOffset  uint32
	LFNEntries   uint8
}

type directoryEntryLocation struct {
	Cluster uint32
	Offset  uint32
}

// how an entry is physically represented on FAT32
type directoryRecord struct {
	Entry DirectoryEntry
	Short directoryEntryLocation
	LFN   []directoryEntryLocation
}

type directoryEntrySlot struct {
	Cluster uint32
	Offset  uint32
}

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
					return nil, err
				}

				name.EntryCluster = cluster
				name.EntryOffset = uint32(offset)

				lfn = append(lfn, name)
				continue
			}

			dirEntry, err := parseDirectoryEntry(entry)
			if err != nil {
				return nil, err
			}

			if len(lfn) > 0 {
				checksum := shortNameChecksum(entry)

				for _, lfnEntry := range lfn {
					if lfnEntry.Checksum != checksum {
						return nil, fmt.Errorf(
							"LFN checksum mismatch for %q",
							dirEntry.ShortName,
						)
					}
				}

				dirEntry.Name = combineLFN(lfn)
				dirEntry.LFNEntries = uint8(len(lfn))
			} else {
				dirEntry.Name = dirEntry.ShortName
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

func parseShortName(data []byte) string {
	name := strings.TrimRight(string(data[0:8]), " ")
	ext := strings.TrimRight(string(data[8:11]), " ")

	if ext == "" {
		return name
	}

	return name + "." + ext
}

func encodeShortName(name string) ([11]byte, error) {
	var result [11]byte

	parts := strings.SplitN(name, ".", 2)

	if len(parts[0]) == 0 || len(parts[0]) > 8 {
		return result, fmt.Errorf("invalid short name: %q", name)
	}

	if len(parts) == 2 && (len(parts[1]) == 0 || len(parts[1]) > 3) {
		return result, fmt.Errorf("invalid short name: %q", name)
	}

	for _, part := range parts {
		for i := 0; i < len(part); i++ {
			c := part[i]

			if c < 0x21 || c > 0x7E {
				return result, fmt.Errorf(
					"invalid character %#02x in short name %q",
					c,
					name,
				)
			}

			if strings.ContainsRune(`"*/:<>?\|+=[ ]`, rune(c)) {
				return result, fmt.Errorf(
					"invalid character %q in short name %q",
					c,
					name,
				)
			}
		}
	}

	for i := range result {
		result[i] = ' '
	}

	copy(result[0:8], strings.ToUpper(parts[0]))

	if len(parts) == 2 {
		copy(result[8:11], strings.ToUpper(parts[1]))
	}

	return result, nil
}

func isShortName(name string) bool {
	encoded, err := encodeShortName(name)
	if err != nil {
		return false
	}

	return parseShortName(encoded[:]) == strings.ToUpper(name)
}

func generateShortName(name string, n int) ([11]byte, error) {
	var result [11]byte

	if n < 1 || n > 9999999 {
		return result, fmt.Errorf("invalid short-name sequence: %d", n)
	}

	parts := strings.SplitN(name, ".", 2)

	base := parts[0]
	ext := ""

	if len(parts) == 2 {
		ext = parts[1]
	}

	var prefix string

	for i := 0; i < len(base); i++ {
		c := base[i]

		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}

		if c >= 'A' && c <= 'Z' ||
			c >= '0' && c <= '9' {
			prefix += string(c)
		}
	}

	if prefix == "" {
		prefix = "FILE"
	}

	suffix := fmt.Sprintf("~%d", n)

	maxPrefix := 8 - len(suffix)

	if maxPrefix < 1 {
		return result, fmt.Errorf(
			"short-name sequence is too large: %d",
			n,
		)
	}

	if len(prefix) > maxPrefix {
		prefix = prefix[:maxPrefix]
	}

	shortName := prefix + suffix

	if len(ext) > 3 {
		ext = ext[:3]
	}

	copy(result[0:8], []byte(fmt.Sprintf("%-8s", shortName)))
	copy(result[8:11], []byte(fmt.Sprintf("%-3s", strings.ToUpper(ext))))

	return result, nil
}

func parseDirectoryEntry(data []byte) (DirectoryEntry, error) {
	if len(data) != 32 {
		return DirectoryEntry{}, fmt.Errorf(
			"directory entry has %d bytes, want 32",
			len(data),
		)
	}

	attr := data[0x0B]

	if attr == 0x0F {
		return DirectoryEntry{}, fmt.Errorf(
			"directory entry is an LFN entry",
		)
	}

	startClusterHigh := binary.LittleEndian.Uint16(data[0x14:0x16])
	startClusterLow := binary.LittleEndian.Uint16(data[0x1A:0x1C])

	startCluster :=
		uint32(startClusterHigh)<<16 |
			uint32(startClusterLow)

	return DirectoryEntry{
		ShortName:    parseShortName(data),
		IsDir:        attr&0x10 != 0,
		Size:         binary.LittleEndian.Uint32(data[0x1C:0x20]),
		StartCluster: startCluster,
	}, nil
}

func encodeDirectoryEntry(
	shortName [11]byte,
	attr byte,
	startCluster uint32,
	size uint32,
) [32]byte {
	var data [32]byte

	copy(data[0:11], shortName[:])

	data[0x0B] = attr

	binary.LittleEndian.PutUint16(
		data[0x14:0x16],
		uint16(startCluster>>16),
	)

	binary.LittleEndian.PutUint16(
		data[0x1A:0x1C],
		uint16(startCluster),
	)

	binary.LittleEndian.PutUint32(
		data[0x1C:0x20],
		size,
	)

	return data
}

func encodeSpecialDirectoryEntry(
	name string,
	startCluster uint32,
) ([32]byte, error) {
	var shortName [11]byte

	for i := range shortName {
		shortName[i] = ' '
	}

	switch name {
	case ".":
		shortName[0] = '.'

	case "..":
		shortName[0] = '.'
		shortName[1] = '.'

	default:
		return [32]byte{}, fmt.Errorf(
			"invalid special directory entry: %q",
			name,
		)
	}

	return encodeDirectoryEntry(
		shortName,
		0x10,
		startCluster,
		0,
	), nil
}

type lfnEntry struct {
	Sequence     uint8
	IsLast       bool
	Checksum     byte
	Name         string
	NameUnits    []uint16
	EntryCluster uint32
	EntryOffset  uint32
}

// The 13 UTF-16 characters are laid out as:
//
//	bytes 1–10: characters 1–5
//	bytes 14–25: characters 6–11
//	bytes 28–31: characters 12–13
//	byte 0x0B: 0x0F
//	byte 0x0D: short-name checksum
func parseLFNEntry(data []byte) (lfnEntry, error) {
	var result lfnEntry

	if len(data) != 32 {
		return result, fmt.Errorf(
			"invalid LFN entry length: %d",
			len(data),
		)
	}

	if data[0x0B] != 0x0F {
		return result, fmt.Errorf(
			"invalid LFN entry attribute: 0x%02x",
			data[0x0B],
		)
	}

	sequence := data[0x00]

	result.Sequence = sequence & 0x1F
	result.IsLast = sequence&0x40 != 0
	result.Checksum = data[0x0D]

	const (
		terminator = uint16(0x0000)
		unused     = uint16(0xFFFF)
	)

	offsets := [...]int{
		0x01, 0x03, 0x05, 0x07, 0x09,
		0x0E, 0x10, 0x12, 0x14, 0x16, 0x18,
		0x1C, 0x1E,
	}

	units := make([]uint16, 0, len(offsets))

	for _, charOffset := range offsets {
		value := binary.LittleEndian.Uint16(
			data[charOffset : charOffset+2],
		)

		if value == terminator {
			break
		}

		if value == unused {
			break
		}

		units = append(units, value)
	}

	result.NameUnits = units

	// This is retained for compatibility with existing tests/users of
	// lfnEntry.Name. It is not used to combine LFN entries, because
	// an individual entry may contain an unpaired surrogate.
	result.Name = string(utf16.Decode(units))

	return result, nil
}

func encodeLFNEntry(
	sequence uint8,
	name string,
	checksum byte,
) ([32]byte, error) {
	units := utf16.Encode([]rune(name))

	if len(units) > 13 {
		return [32]byte{}, fmt.Errorf(
			"LFN entry name is too long: %d UTF-16 code units",
			len(units),
		)
	}

	return encodeLFNEntryUnits(sequence, units, checksum)
}

func encodeLFNEntryUnits(
	sequence uint8,
	units []uint16,
	checksum byte,
) ([32]byte, error) {
	var result [32]byte

	if len(units) > 13 {
		return result, fmt.Errorf(
			"LFN entry has too many UTF-16 code units: %d",
			len(units),
		)
	}

	result[0x00] = sequence
	result[0x0B] = 0x0F
	result[0x0C] = 0x00
	result[0x0D] = checksum
	result[0x1A] = 0x00
	result[0x1B] = 0x00

	const (
		terminator = uint16(0x0000)
		unused     = uint16(0xFFFF)
	)

	offsets := [...]int{
		0x01, 0x03, 0x05, 0x07, 0x09,
		0x0E, 0x10, 0x12, 0x14, 0x16, 0x18,
		0x1C, 0x1E,
	}

	for i, offset := range offsets {
		value := unused

		if i < len(units) {
			value = units[i]
		} else if i == len(units) {
			value = terminator
		}

		binary.LittleEndian.PutUint16(
			result[offset:offset+2],
			value,
		)
	}

	return result, nil
}

func encodeLFNEntries(
	name string,
	checksum byte,
) ([][32]byte, error) {
	units := utf16.Encode([]rune(name))

	if len(units) == 0 {
		return nil, fmt.Errorf("empty LFN")
	}

	count := (len(units) + 12) / 13

	result := make([][32]byte, 0, count)

	// FAT stores LFN entries in reverse logical order:
	// the highest sequence number appears first on disk.
	for i := count - 1; i >= 0; i-- {
		start := i * 13
		end := start + 13

		if end > len(units) {
			end = len(units)
		}

		sequence := uint8(i + 1)

		if i == count-1 {
			sequence |= 0x40
		}

		entry, err := encodeLFNEntryUnits(
			sequence,
			units[start:end],
			checksum,
		)
		if err != nil {
			return nil, err
		}

		result = append(result, entry)
	}

	return result, nil
}

func combineLFN(entries []lfnEntry) string {
	if len(entries) == 0 {
		return ""
	}

	var units []uint16

	// LFN entries are stored on disk in reverse logical order.
	// Reassemble their UTF-16 code units before decoding them.
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].NameUnits != nil {
			units = append(units, entries[i].NameUnits...)
			continue
		}

		// Fallback for lfnEntry values constructed directly by older
		// tests/code rather than by parseLFNEntry.
		units = append(units, utf16.Encode([]rune(entries[i].Name))...)
	}

	return string(utf16.Decode(units))
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

func (fs *Filesystem) DebugDirectory(path string) error {
	dir, err := fs.Find(path)
	if err != nil {
		return err
	}

	clusters, err := fs.readClusterChain(dir.StartCluster)
	if err != nil {
		return err
	}

	for _, cluster := range clusters {
		data, err := fs.readCluster(cluster)
		if err != nil {
			return err
		}

		for offset := 0; offset < len(data); offset += 32 {
			entry := data[offset : offset+32]

			if entry[0] == 0x00 {
				return nil
			}

			if entry[0] == 0xE5 {
				continue
			}

			// Only print entries around Pollon's known location for now.
			if cluster == 169063 &&
				offset >= 2200 &&
				offset <= 2400 {
				fmt.Printf(
					"cluster=%d offset=%d first=%02X attr=%02X\n",
					cluster,
					offset,
					entry[0],
					entry[0x0B],
				)
			}
		}
	}

	return nil
}

func shortNameChecksum(data []byte) byte {
	var checksum byte

	for i := 0; i < 11; i++ {
		checksum = ((checksum & 1) << 7) +
			(checksum >> 1) +
			data[i]
	}

	return checksum
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

type encodedDirectoryRecord struct {
	LFN   [][32]byte
	Short [32]byte
}

func encodeDirectoryRecord(
	name string,
	startCluster uint32,
	size uint32,
) (encodedDirectoryRecord, error) {
	var shortName [11]byte

	if isShortName(name) {
		var err error

		shortName, err = encodeShortName(name)
		if err != nil {
			return encodedDirectoryRecord{}, err
		}
	} else {
		var err error

		shortName, err = generateShortName(name, 1)
		if err != nil {
			return encodedDirectoryRecord{}, err
		}
	}

	return encodeDirectoryRecordWithShortName(
		name,
		shortName,
		startCluster,
		size,
		false,
	)
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

// splitFilePath splits a file path into its parent directory and filename.
func splitFilePath(path string) (string, string, error) {
	path = strings.TrimSpace(path)

	if path == "" {
		return "", "", fmt.Errorf("empty path")
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
				"directory %q not found",
				component,
			)
		}

		if !found.IsDir {
			return 0, fmt.Errorf(
				"%q is not a directory",
				component,
			)
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

func (fs *Filesystem) shortNameExists(
	directoryCluster uint32,
	shortName string,
) (bool, error) {
	records, err := fs.readDirectoryRecords(directoryCluster)
	if err != nil {
		return false, err
	}

	for _, record := range records {
		if strings.EqualFold(record.Entry.ShortName, shortName) {
			return true, nil
		}
	}

	return false, nil
}

func (fs *Filesystem) generateUniqueShortName(
	directoryCluster uint32,
	name string,
) ([11]byte, error) {
	if isShortName(name) {
		return encodeShortName(name)
	}

	for n := 1; n <= 9999999; n++ {
		shortName, err := generateShortName(name, n)
		if err != nil {
			return [11]byte{}, err
		}

		exists, err := fs.shortNameExists(
			directoryCluster,
			parseShortName(shortName[:]),
		)
		if err != nil {
			return [11]byte{}, err
		}

		if !exists {
			return shortName, nil
		}
	}

	return [11]byte{}, fmt.Errorf(
		"unable to generate unique short name for %q",
		name,
	)
}

func encodeDirectoryRecordWithShortName(
	name string,
	shortName [11]byte,
	startCluster uint32,
	size uint32,
	isDir bool,
) (encodedDirectoryRecord, error) {
	var result encodedDirectoryRecord

	attr := byte(0x20) // archive

	if isDir {
		attr = 0x10 // directory
	}

	shortEntry := encodeDirectoryEntry(
		shortName,
		attr,
		startCluster,
		size,
	)

	result.Short = shortEntry

	if !isShortName(name) {
		checksum := shortNameChecksum(shortEntry[:])

		lfnEntries, err := encodeLFNEntries(name, checksum)
		if err != nil {
			return result, err
		}

		result.LFN = lfnEntries
	}

	return result, nil
}

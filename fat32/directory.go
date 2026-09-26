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

	return nil, fmt.Errorf(
		"directory has no run of %d free entries",
		count,
	)
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

	if n < 1 || n > 9 {
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

type lfnEntry struct {
	Sequence uint8
	IsLast   bool
	Checksum byte
	Name     string

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
	if len(data) != 32 {
		return lfnEntry{}, fmt.Errorf(
			"LFN entry has %d bytes, want 32",
			len(data),
		)
	}

	if data[0x0B] != 0x0F {
		return lfnEntry{}, fmt.Errorf(
			"not an LFN entry: attributes = %#02x",
			data[0x0B],
		)
	}

	sequence := data[0x00]

	var chars []uint16

	// Characters 1–5.
	for offset := 0x01; offset < 0x0B; offset += 2 {
		chars = append(chars, binary.LittleEndian.Uint16(data[offset:offset+2]))
	}

	// Characters 6–11.
	for offset := 0x0E; offset < 0x1A; offset += 2 {
		chars = append(chars, binary.LittleEndian.Uint16(data[offset:offset+2]))
	}

	// Characters 12–13.
	for offset := 0x1C; offset < 0x20; offset += 2 {
		chars = append(chars, binary.LittleEndian.Uint16(data[offset:offset+2]))
	}

	// Stop at the UTF-16 terminator.
	for i, char := range chars {
		if char == 0x0000 {
			chars = chars[:i]
			break
		}
	}

	return lfnEntry{
		Sequence: sequence & 0x3F,
		IsLast:   sequence&0x40 != 0,
		Checksum: data[0x0D],
		Name:     string(utf16.Decode(chars)),
	}, nil
}

func encodeLFNEntry(
	sequence uint8,
	name string,
	checksum byte,
) ([32]byte, error) {
	var data [32]byte

	chars := utf16.Encode([]rune(name))

	if len(chars) > 13 {
		return data, fmt.Errorf(
			"LFN segment is %d UTF-16 code units, want at most 13",
			len(chars),
		)
	}

	data[0x00] = sequence
	data[0x0B] = 0x0F
	data[0x0D] = checksum

	charOffsets := []int{
		0x01, 0x03, 0x05, 0x07, 0x09,
		0x0E, 0x10, 0x12, 0x14, 0x16, 0x18,
		0x1C, 0x1E,
	}

	for i, offset := range charOffsets {
		value := uint16(0xFFFF)

		if i < len(chars) {
			value = chars[i]
		} else if i == len(chars) {
			value = 0x0000
		}

		binary.LittleEndian.PutUint16(
			data[offset:offset+2],
			value,
		)
	}

	return data, nil
}

func encodeLFNEntries(name string, checksum byte) ([][32]byte, error) {
	chars := utf16.Encode([]rune(name))

	if len(chars) == 0 {
		return nil, fmt.Errorf("LFN name is empty")
	}

	const charsPerEntry = 13

	count := (len(chars) + charsPerEntry - 1) / charsPerEntry
	entries := make([][32]byte, 0, count)

	for i := count - 1; i >= 0; i-- {
		start := i * charsPerEntry
		end := start + charsPerEntry

		if end > len(chars) {
			end = len(chars)
		}

		segment := string(utf16.Decode(chars[start:end]))

		sequence := uint8(i + 1)

		if i == count-1 {
			sequence |= 0x40
		}

		entry, err := encodeLFNEntry(
			sequence,
			segment,
			checksum,
		)
		if err != nil {
			return nil, err
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

func combineLFN(entries []lfnEntry) string {
	var name string

	for i := len(entries) - 1; i >= 0; i-- {
		name += entries[i].Name
	}

	return name
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

	for _, location := range record.LFN {
		if err := fs.markDirectoryEntryDeleted(location); err != nil {
			return err
		}
	}

	if err := fs.markDirectoryEntryDeleted(record.Short); err != nil {
		return err
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
	var result encodedDirectoryRecord

	var shortName [11]byte

	if isShortName(name) {
		var err error

		shortName, err = encodeShortName(name)
		if err != nil {
			return result, err
		}
	} else {
		var err error

		shortName, err = generateShortName(name, 1)
		if err != nil {
			return result, err
		}
	}

	shortEntry := encodeDirectoryEntry(
		shortName,
		0x20, // archive
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

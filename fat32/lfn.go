package fat32

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf16"
)

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

	// Bits 5 and 7 of the LFN sequence byte are reserved.
	if sequence&0xA0 != 0 {
		return result, fmt.Errorf(
			"invalid LFN sequence byte: 0x%02x",
			sequence,
		)
	}

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

func validateLFNSequence(entries []lfnEntry) error {
	if len(entries) == 0 {
		return nil
	}

	// The first LFN entry on disk must have the last-entry flag
	// and its sequence number must equal the number of LFN entries.
	if !entries[0].IsLast {
		return fmt.Errorf(
			"LFN sequence is missing last-entry flag",
		)
	}

	expected := uint8(len(entries))

	for i, entry := range entries {
		if entry.Sequence != expected {
			return fmt.Errorf(
				"invalid LFN sequence at entry %d: got %d, want %d",
				i,
				entry.Sequence,
				expected,
			)
		}

		// Only the first entry on disk may have the last-entry flag.
		if i > 0 && entry.IsLast {
			return fmt.Errorf(
				"LFN sequence has unexpected last-entry flag at entry %d",
				i,
			)
		}

		if expected == 0 {
			return fmt.Errorf("invalid LFN sequence number 0")
		}

		expected--
	}

	return nil
}

// lfnBelongsTo reports whether the LFN run is a valid sequence whose
// checksums match the short entry that follows it.
func lfnBelongsTo(entries []lfnEntry, shortEntry []byte) bool {
	if validateLFNSequence(entries) != nil {
		return false
	}

	checksum := shortNameChecksum(shortEntry)

	for _, entry := range entries {
		if entry.Checksum != checksum {
			return false
		}
	}

	return true
}

// validateLongName checks that name can be stored as a FAT long filename.
func validateLongName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid file name: %q", name)
	}

	if len(utf16.Encode([]rune(name))) > 255 {
		return fmt.Errorf(
			"file name is longer than 255 characters: %q",
			name,
		)
	}

	for _, r := range name {
		if r < 0x20 || strings.ContainsRune(`"*/:<>?\|`, r) {
			return fmt.Errorf(
				"invalid character %q in file name %q",
				r,
				name,
			)
		}
	}

	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return fmt.Errorf(
			"file name ends with a dot or space: %q",
			name,
		)
	}

	return nil
}

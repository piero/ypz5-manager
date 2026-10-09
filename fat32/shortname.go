package fat32

import (
	"fmt"
	"strings"
)

// NT case flags stored at offset 0x0C of a short entry: the 8.3 name is
// stored upper-case, and these bits say which parts to show lower-case.
const (
	caseLowerBase = 0x08
	caseLowerExt  = 0x10
)

// shortNameInvalidChars are printable ASCII characters that are not
// allowed in an 8.3 name.
const shortNameInvalidChars = `"*+,./:;<=>?[\]| `

func parseShortName(data []byte) string {
	name := strings.TrimRight(string(data[0:8]), " ")
	ext := strings.TrimRight(string(data[8:11]), " ")

	if ext == "" {
		return name
	}

	return name + "." + ext
}

// parseShortNameWithCase returns the 8.3 name, lower-casing the base
// and/or extension as requested by the entry's NT case flags.
func parseShortNameWithCase(data []byte) string {
	name := strings.TrimRight(string(data[0:8]), " ")
	ext := strings.TrimRight(string(data[8:11]), " ")

	if data[0x0C]&caseLowerBase != 0 {
		name = strings.ToLower(name)
	}

	if data[0x0C]&caseLowerExt != 0 {
		ext = strings.ToLower(ext)
	}

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

			if strings.ContainsRune(shortNameInvalidChars, rune(c)) {
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

// isShortName reports whether name can be stored as an 8.3 entry alone,
// without LFN entries. The base and extension may each be entirely upper
// or lower case (lower case is recorded with NT case flags); mixed case
// needs an LFN to be preserved.
func isShortName(name string) bool {
	encoded, err := encodeShortName(name)
	if err != nil {
		return false
	}

	if parseShortName(encoded[:]) != strings.ToUpper(name) {
		return false
	}

	for _, part := range strings.SplitN(name, ".", 2) {
		if part != strings.ToUpper(part) && part != strings.ToLower(part) {
			return false
		}
	}

	return true
}

// shortNameCaseFlags returns the NT case flags that make an 8.3 entry
// display as name. It assumes isShortName(name) is true.
func shortNameCaseFlags(name string) byte {
	parts := strings.SplitN(name, ".", 2)

	var flags byte

	if parts[0] != strings.ToUpper(parts[0]) {
		flags |= caseLowerBase
	}

	if len(parts) == 2 && parts[1] != strings.ToUpper(parts[1]) {
		flags |= caseLowerExt
	}

	return flags
}

// shortNameChars converts s to characters valid in an 8.3 name, the way
// Windows does: letters are upper-cased, spaces and dots are dropped and
// any other invalid or non-ASCII character becomes '_'.
func shortNameChars(s string) string {
	var b strings.Builder

	for _, r := range s {
		switch {
		case r == ' ' || r == '.':
			continue

		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 'a' + 'A')

		case r < 0x21 || r > 0x7E ||
			strings.ContainsRune(shortNameInvalidChars, r):
			b.WriteByte('_')

		default:
			b.WriteRune(r)
		}
	}

	return b.String()
}

func generateShortName(name string, n int) ([11]byte, error) {
	var result [11]byte

	if n < 1 || n > 9999999 {
		return result, fmt.Errorf("invalid short-name sequence: %d", n)
	}

	// Leading dots are not part of the extension (".DS_Store").
	name = strings.TrimLeft(name, ".")

	base := name
	ext := ""

	// The extension is whatever follows the last dot.
	if i := strings.LastIndex(name, "."); i >= 0 {
		base = name[:i]
		ext = name[i+1:]
	}

	prefix := shortNameChars(base)
	ext = shortNameChars(ext)

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
	copy(result[8:11], []byte(fmt.Sprintf("%-3s", ext)))

	return result, nil
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

package fat32

import (
	"encoding/binary"
	"time"
)

// Offsets of the timestamp fields in a short directory entry.
const (
	entryCreateTenths = 0x0D
	entryCreateTime   = 0x0E
	entryCreateDate   = 0x10
	entryAccessDate   = 0x12
	entryWriteTime    = 0x16
	entryWriteDate    = 0x18
)

// now returns the current time used for directory entry timestamps.
func (fs *Filesystem) now() time.Time {
	if fs.clock != nil {
		return fs.clock()
	}

	return time.Now()
}

// encodeFATDateTime converts t to FAT's local date and time format,
// clamped to the representable range 1980-01-01 .. 2107-12-31. tenths
// is the creation time's extra precision: 0..199 units of 10 ms.
func encodeFATDateTime(t time.Time) (date, clock uint16, tenths uint8) {
	t = t.Local()

	switch {
	case t.Year() < 1980:
		return 1<<5 | 1, 0, 0

	case t.Year() > 2107:
		return 127<<9 | 12<<5 | 31, 23<<11 | 59<<5 | 29, 199
	}

	date = uint16(t.Year()-1980)<<9 | uint16(t.Month())<<5 | uint16(t.Day())
	clock = uint16(t.Hour())<<11 | uint16(t.Minute())<<5 | uint16(t.Second()/2)
	tenths = uint8(t.Second()%2*100 + t.Nanosecond()/10_000_000)

	return date, clock, tenths
}

// decodeFATDateTime converts a FAT date and time to local time. It
// returns the zero time for an unset or invalid date.
func decodeFATDateTime(date, clock uint16) time.Time {
	year := int(date>>9) + 1980
	month := time.Month(date >> 5 & 0x0F)
	day := int(date & 0x1F)

	if month < 1 || month > 12 || day < 1 {
		return time.Time{}
	}

	return time.Date(
		year,
		month,
		day,
		int(clock>>11),
		int(clock>>5&0x3F),
		int(clock&0x1F)*2,
		0,
		time.Local,
	)
}

// stampCreated sets the creation, modification and access timestamps
// of a short directory entry.
func stampCreated(entry *[32]byte, t time.Time) {
	date, clock, tenths := encodeFATDateTime(t)

	entry[entryCreateTenths] = tenths
	binary.LittleEndian.PutUint16(entry[entryCreateTime:], clock)
	binary.LittleEndian.PutUint16(entry[entryCreateDate:], date)

	stampModified(entry, t)
}

// stampModified sets the modification and access timestamps of a short
// directory entry.
func stampModified(entry *[32]byte, t time.Time) {
	date, clock, _ := encodeFATDateTime(t)

	binary.LittleEndian.PutUint16(entry[entryAccessDate:], date)
	binary.LittleEndian.PutUint16(entry[entryWriteTime:], clock)
	binary.LittleEndian.PutUint16(entry[entryWriteDate:], date)
}

package fat32

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestGenerateShortNameUsesLastDotAndSanitizes(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"Mr. Blue Sky.mp3", "MRBLUE~1.MP3"},
		{"04 - Running Free.mp3", "04-RUN~1.MP3"},
		{"a+b,c;d=e.mp3", "A_B_C_~1.MP3"},
		{".DS_Store", "DS_STO~1"},
		{"Città.mp3", "CITT_~1.MP3"},
		{"archive.tar.gz", "ARCHIV~1.GZ"},
		{"weird.m p3", "WEIRD~1.MP3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := generateShortName(tt.name, 1)
			if err != nil {
				t.Fatal(err)
			}

			if gotName := parseShortName(got[:]); gotName != tt.want {
				t.Fatalf("short name = %q, want %q", gotName, tt.want)
			}
		})
	}
}

func TestEncodeShortNameRejectsInvalidCharacters(t *testing.T) {
	for _, name := range []string{"A.B.C", "A,B.TXT", "A;B.TXT", "A.B;"} {
		if _, err := encodeShortName(name); err == nil {
			t.Errorf("encodeShortName(%q) unexpectedly succeeded", name)
		}
	}
}

func TestIsShortNameRequiresUniformCase(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"HELLO.TXT", true},
		{"hello.txt", true},
		{"HELLO.txt", true},
		{"Hello.txt", false},
		{"hello.Txt", false},
		{"a.b.c", false},
	}

	for _, tt := range tests {
		if got := isShortName(tt.name); got != tt.want {
			t.Errorf("isShortName(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestShortNameCaseFlagsRoundTrip(t *testing.T) {
	for _, name := range []string{"hello.txt", "HELLO.txt", "hello.TXT", "readme"} {
		record, err := encodeDirectoryRecord(name, 3, 1)
		if err != nil {
			t.Fatal(err)
		}

		if len(record.LFN) != 0 {
			t.Fatalf("%q: unexpected LFN entries", name)
		}

		if got := parseShortNameWithCase(record.Short[:]); got != name {
			t.Errorf("round trip of %q = %q", name, got)
		}
	}
}

func TestValidateLongName(t *testing.T) {
	long := string(bytes.Repeat([]byte("a"), 256))

	tests := []struct {
		name    string
		wantErr bool
	}{
		{"01 - Song.mp3", false},
		{"日本語.mp3", false},
		{".DS_Store", false},
		{"", true},
		{".", true},
		{"..", true},
		{"a:b.mp3", true},
		{"a?b.mp3", true},
		{"a\\b.mp3", true},
		{"tab\t.mp3", true},
		{"trailing.", true},
		{"trailing ", true},
		{long, true},
		{long[:255], false},
	}

	for _, tt := range tests {
		err := validateLongName(tt.name)

		if tt.wantErr && err == nil {
			t.Errorf("validateLongName(%q) unexpectedly succeeded", tt.name)
		}

		if !tt.wantErr && err != nil {
			t.Errorf("validateLongName(%q): %v", tt.name, err)
		}
	}
}

func TestYPZ5WriteFileRejectsInvalidName(t *testing.T) {
	_, fs := openYPZ5WriteImage(t)

	before, _ := fs.FreeClusters()

	if err := fs.WriteFile("/YPZ5 a:b.mp3", []byte("x")); err == nil {
		t.Fatal("WriteFile with invalid name unexpectedly succeeded")
	}

	if after, _ := fs.FreeClusters(); after != before {
		t.Fatalf("free clusters changed from %d to %d", before, after)
	}

	checkConsistency(t, fs)
}

// TestYPZ5FSInfoTracksAllocations verifies that the FSInfo free count is
// updated on disk by writes and deletes.
func TestYPZ5FSInfoTracksAllocations(t *testing.T) {
	_, fs := openYPZ5WriteImage(t)

	checkConsistency(t, fs)

	before, ok := fs.FreeClusters()
	if !ok {
		t.Fatal("YP-Z5 image has no FSInfo free count")
	}

	data := bytes.Repeat([]byte("x"), int(fs.BytesPerCluster())*3+1)

	if err := fs.Mkdir("/YPZ5 FSInfo Test"); err != nil {
		t.Fatal(err)
	}

	if err := fs.WriteFile("/YPZ5 FSInfo Test/Song.mp3", data); err != nil {
		t.Fatal(err)
	}

	if got, _ := fs.FreeClusters(); got != before-5 {
		t.Fatalf("free clusters after write = %d, want %d", got, before-5)
	}

	checkConsistency(t, fs)

	if err := fs.RemoveAll("/YPZ5 FSInfo Test"); err != nil {
		t.Fatal(err)
	}

	if got, _ := fs.FreeClusters(); got != before {
		t.Fatalf("free clusters after delete = %d, want %d", got, before)
	}

	checkConsistency(t, fs)
}

// TestYPZ5FSInfoStaleCountIsCorrected verifies that a wrong FSInfo free
// count is recomputed when the filesystem is opened for writing.
func TestYPZ5FSInfoStaleCountIsCorrected(t *testing.T) {
	_, fs := openYPZ5WriteImage(t)

	want := fs.info.freeCount

	stale := make([]byte, 4)
	binary.LittleEndian.PutUint32(stale, 12345)

	if _, err := fs.writer.WriteAt(
		stale,
		fs.info.offset+fsInfoFreeCountOffset,
	); err != nil {
		t.Fatal(err)
	}

	if err := fs.Open(); err != nil {
		t.Fatal(err)
	}

	if got, _ := fs.FreeClusters(); got != want {
		t.Fatalf("free clusters = %d, want %d", got, want)
	}

	if err := fs.Mkdir("/YPZ5 Stale"); err != nil {
		t.Fatal(err)
	}

	checkConsistency(t, fs)
}

// setAttr sets attribute bits on the entry at path.
func setAttr(t *testing.T, fs *Filesystem, path string, attr uint8) {
	t.Helper()

	record, err := fs.findDirectoryRecord(path)
	if err != nil {
		t.Fatal(err)
	}

	cluster, err := fs.readCluster(record.Short.Cluster)
	if err != nil {
		t.Fatal(err)
	}

	var short [32]byte
	copy(short[:], cluster[record.Short.Offset:])
	short[0x0B] |= attr

	if err := fs.writeDirectoryEntry(record.Short, short); err != nil {
		t.Fatal(err)
	}
}

// TestYPZ5ProtectedEntriesCannotBeRemoved checks read-only and system
// entries created by the test, and any protected entries already in the
// image's root (on the YP-Z5: System, DevIcon.fil and DevLogo.fil).
func TestYPZ5ProtectedEntriesCannotBeRemoved(t *testing.T) {
	_, fs := openYPZ5WriteImage(t)

	dir := testDir(t, fs, "YPZ5 Protected")
	readOnly := dir + "/read-only.txt"
	system := dir + "/System Folder"

	if err := fs.WriteFile(readOnly, []byte("keep")); err != nil {
		t.Fatal(err)
	}

	if err := fs.Mkdir(system); err != nil {
		t.Fatal(err)
	}

	setAttr(t, fs, readOnly, AttrReadOnly)
	setAttr(t, fs, system, AttrSystem)

	protected := []string{readOnly, system}

	root, err := fs.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range root {
		if entry.Protected() {
			protected = append(protected, "/"+entry.Name)
		}
	}

	for _, path := range protected {
		if err := fs.RemoveAll(path); !errors.Is(err, ErrProtected) {
			t.Fatalf("RemoveAll(%q) error = %v, want ErrProtected", path, err)
		}

		if _, err := fs.Lookup(path); err != nil {
			t.Fatalf("%s missing after refused delete: %v", path, err)
		}
	}

	if err := fs.Delete(readOnly); !errors.Is(err, ErrProtected) {
		t.Fatalf("Delete error = %v, want ErrProtected", err)
	}

	if err := fs.ReplaceFile(readOnly, []byte("x")); !errors.Is(err, ErrProtected) {
		t.Fatalf("ReplaceFile error = %v, want ErrProtected", err)
	}

	checkConsistency(t, fs)
}

// TestYPZ5RemoveAllChecksWholeTreeFirst verifies that a directory holding
// a protected entry is left completely untouched.
func TestYPZ5RemoveAllChecksWholeTreeFirst(t *testing.T) {
	_, fs := openYPZ5WriteImage(t)

	dir := testDir(t, fs, "YPZ5 Tree")

	for _, name := range []string{dir + "/a.txt", dir + "/b.txt"} {
		if err := fs.WriteFile(name, []byte(name)); err != nil {
			t.Fatal(err)
		}
	}

	setAttr(t, fs, dir+"/b.txt", AttrReadOnly)

	if err := fs.RemoveAll(dir); !errors.Is(err, ErrProtected) {
		t.Fatalf("RemoveAll error = %v, want ErrProtected", err)
	}

	entries, err := fs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 2 {
		t.Fatalf("%s has %d entries after refused delete, want 2", dir, len(entries))
	}

	checkConsistency(t, fs)
}

func TestFATDateTimeRoundTrip(t *testing.T) {
	in := time.Date(2026, 10, 9, 18, 30, 15, 250_000_000, time.Local)

	date, clock, tenths := encodeFATDateTime(in)

	// FAT time has a 2-second resolution; the odd second and the
	// milliseconds go into the creation tenths.
	if got, want := decodeFATDateTime(date, clock), in.Truncate(2*time.Second); !got.Equal(want) {
		t.Fatalf("round trip = %v, want %v", got, want)
	}

	if tenths != 125 {
		t.Fatalf("tenths = %d, want 125", tenths)
	}

	if got := decodeFATDateTime(0, 0); !got.IsZero() {
		t.Fatalf("zero date decoded as %v", got)
	}

	date, _, _ = encodeFATDateTime(time.Date(1970, 1, 1, 0, 0, 0, 0, time.Local))
	if got := decodeFATDateTime(date, 0); got.Year() != 1980 || got.Month() != 1 || got.Day() != 1 {
		t.Fatalf("pre-1980 date clamped to %v, want 1980-01-01", got)
	}
}

func TestYPZ5NewEntriesHaveTimestamps(t *testing.T) {
	_, fs := openYPZ5WriteImage(t)

	created := time.Date(2026, 10, 9, 18, 30, 16, 0, time.Local) // even: FAT has 2 s resolution
	fs.clock = func() time.Time { return created }

	if err := fs.Mkdir("/YPZ5 Dated"); err != nil {
		t.Fatal(err)
	}

	if err := fs.WriteFile("/YPZ5 Dated/Song.mp3", []byte("v1")); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/YPZ5 Dated", "/YPZ5 Dated/Song.mp3"} {
		entry, err := fs.Lookup(path)
		if err != nil {
			t.Fatal(err)
		}

		if !entry.Modified.Equal(created) {
			t.Fatalf("%s modified = %v, want %v", path, entry.Modified, created)
		}
	}

	modified := created.Add(time.Hour)
	fs.clock = func() time.Time { return modified }

	if err := fs.ReplaceFile("/YPZ5 Dated/Song.mp3", []byte("v2")); err != nil {
		t.Fatal(err)
	}

	record, err := fs.findDirectoryRecord("/YPZ5 Dated/Song.mp3")
	if err != nil {
		t.Fatal(err)
	}

	if !record.Entry.Modified.Equal(modified) {
		t.Fatalf("modified after replace = %v, want %v", record.Entry.Modified, modified)
	}

	cluster, err := fs.readCluster(record.Short.Cluster)
	if err != nil {
		t.Fatal(err)
	}

	short := cluster[record.Short.Offset : record.Short.Offset+32]

	createdOnDisk := decodeFATDateTime(
		binary.LittleEndian.Uint16(short[entryCreateDate:]),
		binary.LittleEndian.Uint16(short[entryCreateTime:]),
	)

	if !createdOnDisk.Equal(created) {
		t.Fatalf("creation time changed by replace: %v, want %v", createdOnDisk, created)
	}
}

// TestYPZ5DamagedLFNFallsBackToShortName verifies that a corrupted long
// filename only affects its own entry: the directory stays readable and
// writable, and the file is listed under its 8.3 name.
func TestYPZ5DamagedLFNFallsBackToShortName(t *testing.T) {
	_, fs := openYPZ5WriteImage(t)

	dir := testDir(t, fs, "YPZ5 Damaged LFN")
	path := dir + "/01 - A Long File Name.mp3"

	for _, name := range []string{path, dir + "/02 - Another Track.mp3"} {
		if err := fs.WriteFile(name, []byte(name)); err != nil {
			t.Fatal(err)
		}
	}

	record, err := fs.findDirectoryRecord(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(record.LFN) == 0 {
		t.Fatal("test file has no LFN entries")
	}

	// Corrupt the checksum of the file's first LFN entry.
	location := record.LFN[0]

	cluster, err := fs.readCluster(location.Cluster)
	if err != nil {
		t.Fatal(err)
	}

	var lfn [32]byte
	copy(lfn[:], cluster[location.Offset:])
	lfn[0x0D] ^= 0xFF

	if err := fs.writeDirectoryEntry(location, lfn); err != nil {
		t.Fatal(err)
	}

	entries, err := fs.ReadDir(dir)
	if err != nil {
		t.Fatalf("directory with a damaged LFN is unreadable: %v", err)
	}

	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name)
	}

	joined := strings.Join(names, "\n")

	if !strings.Contains(joined, record.Entry.ShortName) {
		t.Fatalf("8.3 name %q not listed:\n%s", record.Entry.ShortName, joined)
	}

	if strings.Contains(joined, "01 - A Long File Name.mp3") {
		t.Fatal("damaged long name is still listed")
	}

	if _, err := fs.ReadFile(dir + "/" + record.Entry.ShortName); err != nil {
		t.Fatalf("file unreadable under its 8.3 name: %v", err)
	}

	if err := fs.WriteFile(dir+"/New.mp3", []byte("new")); err != nil {
		t.Fatalf("directory with a damaged LFN is not writable: %v", err)
	}
}

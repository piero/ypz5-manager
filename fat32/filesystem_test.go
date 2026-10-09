package fat32

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func TestNew(t *testing.T) {
	data := []byte("test filesystem")
	reader := bytes.NewReader(data)

	fs := New(reader)

	if fs.reader != reader {
		t.Fatalf("reader = %v, want %v", fs.reader, reader)
	}
}

func TestBootSector(t *testing.T) {
	var boot [512]byte

	binary.LittleEndian.PutUint16(boot[0x0B:0x0D], 512)
	boot[0x0D] = 8
	binary.LittleEndian.PutUint16(boot[0x0E:0x10], 32)
	boot[0x10] = 2
	binary.LittleEndian.PutUint32(boot[0x24:0x28], 1000)
	binary.LittleEndian.PutUint32(boot[0x2C:0x30], 2)

	got, err := readBootSector(bytes.NewReader(boot[:]))
	if err != nil {
		t.Fatal(err)
	}

	if got.BytesPerSector != 512 {
		t.Errorf("BytesPerSector = %d, want 512", got.BytesPerSector)
	}

	if got.SectorsPerCluster != 8 {
		t.Errorf("SectorsPerCluster = %d, want 8", got.SectorsPerCluster)
	}

	if got.ReservedSectors != 32 {
		t.Errorf("ReservedSectors = %d, want 32", got.ReservedSectors)
	}

	if got.NumberOfFATs != 2 {
		t.Errorf("NumberOfFATs = %d, want 2", got.NumberOfFATs)
	}

	if got.SectorsPerFAT != 1000 {
		t.Errorf("SectorsPerFAT = %d, want 1000", got.SectorsPerFAT)
	}

	if got.RootCluster != 2 {
		t.Errorf("RootCluster = %d, want 2", got.RootCluster)
	}
}

func TestBootSectorLayout(t *testing.T) {
	boot := BootSector{
		BytesPerSector:    512,
		SectorsPerCluster: 8,
		ReservedSectors:   36,
		NumberOfFATs:      2,
		SectorsPerFAT:     3862,
		RootCluster:       2,
	}

	layout := boot.Layout()

	if layout.FATOffset != 18432 {
		t.Errorf("FATOffset = %d, want 18432", layout.FATOffset)
	}

	if layout.DataOffset != 3973120 {
		t.Errorf("DataOffset = %d, want 3973120", layout.DataOffset)
	}

	if layout.BytesPerCluster != 4096 {
		t.Errorf("BytesPerCluster = %d, want 4096", layout.BytesPerCluster)
	}
}

func TestNewIsReadOnly(t *testing.T) {
	data := make([]byte, 4096)

	fs := New(bytes.NewReader(data))

	if err := fs.writable(); err == nil {
		t.Fatal("read-only filesystem unexpectedly has write access")
	}
}

func TestWriteNewFileData(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		firstCluster    = uint32(2)
	)

	data := make([]byte, dataOffset+3*bytesPerCluster)

	// Cluster 2 is initially free.
	binary.LittleEndian.PutUint32(
		data[fatOffset+firstCluster*4:],
		0,
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: &writerAtBuffer{data: data},
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      4,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	content := []byte("hello")

	chain, err := fs.writeNewFileData(content)
	if err != nil {
		t.Fatal(err)
	}

	if len(chain) != 1 {
		t.Fatalf("got %d clusters, want 1", len(chain))
	}

	if chain[0] != firstCluster {
		t.Fatalf("got cluster %d, want %d", chain[0], firstCluster)
	}

	got, err := fs.readCluster(firstCluster)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got[:len(content)], content) {
		t.Fatalf("got %q, want %q", got[:len(content)], content)
	}

	for _, b := range got[len(content):] {
		if b != 0 {
			t.Fatalf("expected unused portion of cluster to be zero, got %x", b)
		}
	}

	entry, err := fs.readFATEntry(firstCluster)
	if err != nil {
		t.Fatal(err)
	}

	if entry < fat32EOF {
		t.Fatalf("cluster %d is not marked EOF: %x", firstCluster, entry)
	}
}

func TestGenerateUniqueShortNameAvoidsExistingShortName(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
	)

	data := make([]byte, dataOffset+3*bytesPerCluster)

	binary.LittleEndian.PutUint32(
		data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: &writerAtBuffer{data: data},
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      4,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	// Occupy the first generated alias.
	if err := fs.WriteFile("SOMELO~1.TXT", []byte("existing")); err != nil {
		t.Fatal(err)
	}

	shortName, err := fs.generateUniqueShortName(
		rootCluster,
		"Some long filename.txt",
	)
	if err != nil {
		t.Fatal(err)
	}

	got := parseShortName(shortName[:])
	want := "SOMELO~2.TXT"

	if got != want {
		t.Fatalf("short name = %q, want %q", got, want)
	}
}

type failOnClusterWriteAt struct {
	data       []byte
	failOffset int64
}

func (w *failOnClusterWriteAt) WriteAt(p []byte, off int64) (int, error) {
	if off == int64(w.failOffset) {
		return 0, errors.New("injected directory write failure")
	}

	if off < 0 || off+int64(len(p)) > int64(len(w.data)) {
		return 0, io.ErrShortBuffer
	}

	copy(w.data[off:off+int64(len(p))], p)
	return len(p), nil
}

type failOnWriteCountAt struct {
	data       []byte
	failOffset int64
	failCount  int
	writes     int
}

func (w *failOnWriteCountAt) WriteAt(p []byte, off int64) (int, error) {
	if off == w.failOffset {
		w.writes++

		if w.writes == w.failCount {
			return 0, errors.New("injected FAT rollback failure")
		}
	}

	if off < 0 || off+int64(len(p)) > int64(len(w.data)) {
		return 0, io.ErrShortBuffer
	}

	copy(w.data[off:off+int64(len(p))], p)
	return len(p), nil
}

func TestAllocateClusterChainLinksClusters(t *testing.T) {
	const (
		sectorSize      = 512
		bytesPerCluster = 512
		fatOffset       = 0
		dataOffset      = 512
		rootCluster     = uint32(2)
	)

	data := make([]byte, dataOffset+8*bytesPerCluster)

	binary.LittleEndian.PutUint32(
		data[fatOffset+rootCluster*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: &writerAtBuffer{data: data},
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      1,
			SectorsPerFAT:     1,
			TotalSectors:      9,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      dataOffset,
			BytesPerCluster: bytesPerCluster,
		},
	}

	chain, err := fs.allocateClusterChain(3)
	if err != nil {
		t.Fatalf("allocateClusterChain failed: %v", err)
	}

	if len(chain) != 3 {
		t.Fatalf(
			"chain length = %d, want 3",
			len(chain),
		)
	}

	for i := 0; i < len(chain)-1; i++ {
		next, err := fs.readFATEntry(chain[i])
		if err != nil {
			t.Fatal(err)
		}

		if next != chain[i+1] {
			t.Fatalf(
				"FAT[%d] = %d, want %d",
				chain[i],
				next,
				chain[i+1],
			)
		}
	}

	last, err := fs.readFATEntry(chain[len(chain)-1])
	if err != nil {
		t.Fatal(err)
	}

	if last != fat32EOF {
		t.Fatalf(
			"FAT[%d] = 0x%08x, want EOF",
			chain[len(chain)-1],
			last,
		)
	}
}

func TestWriteFileReusesSlotsAfterDeletingLongFilename(t *testing.T) {
	const (
		sectorSize    = 512
		sectorsPerFAT = 1
		numberOfFATs  = 1
		totalSectors  = 20
		rootCluster   = uint32(2)
	)

	data := make([]byte, sectorSize*totalSectors)

	fatOffset := int64(0)

	binary.LittleEndian.PutUint32(
		data[fatOffset+int64(rootCluster)*4:],
		fat32EOF,
	)

	fs := &Filesystem{
		reader: bytes.NewReader(data),
		writer: &writerAtBuffer{data: data},
		boot: BootSector{
			BytesPerSector:    sectorSize,
			SectorsPerCluster: 1,
			NumberOfFATs:      numberOfFATs,
			SectorsPerFAT:     sectorsPerFAT,
			TotalSectors:      totalSectors,
			RootCluster:       rootCluster,
		},
		layout: Layout{
			FATOffset:       fatOffset,
			DataOffset:      sectorSize * 2,
			BytesPerCluster: sectorSize,
		},
	}

	firstName := "First sufficiently long filename.txt"
	secondName := "Second sufficiently long filename.txt"

	if err := fs.WriteFile(firstName, []byte("first")); err != nil {
		t.Fatalf("first WriteFile failed: %v", err)
	}

	first, err := fs.findDirectoryEntry(rootCluster, firstName)
	if err != nil {
		t.Fatal(err)
	}

	if first == nil {
		t.Fatalf("first file %q not found", firstName)
	}

	if len(first.LFN) == 0 {
		t.Fatal("first file has no LFN entries")
	}

	firstSlots := append(
		[]directoryEntryLocation(nil),
		first.LFN...,
	)
	firstSlots = append(firstSlots, first.Short)

	if err := fs.Delete(firstName); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if err := fs.WriteFile(secondName, []byte("second")); err != nil {
		t.Fatalf("second WriteFile failed: %v", err)
	}

	second, err := fs.findDirectoryEntry(rootCluster, secondName)
	if err != nil {
		t.Fatal(err)
	}

	if second == nil {
		t.Fatalf("second file %q not found", secondName)
	}

	secondSlots := append(
		[]directoryEntryLocation(nil),
		second.LFN...,
	)
	secondSlots = append(secondSlots, second.Short)

	if len(secondSlots) != len(firstSlots) {
		t.Fatalf(
			"second record uses %d slots, first used %d",
			len(secondSlots),
			len(firstSlots),
		)
	}

	for i := range firstSlots {
		if secondSlots[i] != firstSlots[i] {
			t.Fatalf(
				"second record slot %d = %+v, want %+v",
				i,
				secondSlots[i],
				firstSlots[i],
			)
		}
	}
}

type failDirectoryAndFATRollbackBuffer struct {
	data []byte

	directoryOffset         int64
	failDirectoryWrite      bool
	failDirectoryWriteCount int
	directoryWrites         int

	fatOffset         int64
	failFATWriteCount int
	fatWrites         int
}

func (b *failDirectoryAndFATRollbackBuffer) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off+int64(len(p)) > int64(len(b.data)) {
		return 0, io.EOF
	}

	start := int(off)
	end := start + len(p)

	copy(p, b.data[start:end])

	return len(p), nil
}

func (b *failDirectoryAndFATRollbackBuffer) WriteAt(p []byte, off int64) (int, error) {
	if off == b.directoryOffset {
		b.directoryWrites++

		if b.failDirectoryWrite ||
			(b.failDirectoryWriteCount > 0 &&
				b.directoryWrites == b.failDirectoryWriteCount) {
			return 0, errors.New("injected directory write failure")
		}
	}

	if off == b.fatOffset {
		b.fatWrites++

		if b.failFATWriteCount > 0 &&
			b.fatWrites == b.failFATWriteCount {
			return 0, errors.New("injected FAT rollback failure")
		}
	}

	if off < 0 || off+int64(len(p)) > int64(len(b.data)) {
		return 0, io.ErrShortBuffer
	}

	start := int(off)
	end := start + len(p)

	copy(b.data[start:end], p)

	return len(p), nil
}

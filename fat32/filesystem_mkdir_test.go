package fat32

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
)

func TestMkdirRoundTrip(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 40

		rootCluster = uint32(2)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	boot := BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: sectorsPerCluster,
		NumberOfFATs:      numberOfFATs,
		SectorsPerFAT:     sectorsPerFAT,
		TotalSectors:      totalSectors,
		RootCluster:       rootCluster,
	}

	layout := Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: int64(sectorSize),
	}

	rootOffset := int(dataOffset)
	data[rootOffset+32] = 0x00

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	if err := fs.Mkdir("TESTDIR"); err != nil {
		t.Fatal(err)
	}

	record, err := fs.findDirectoryEntry(
		rootCluster,
		"TESTDIR",
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("new directory was not found")
	}

	if !record.Entry.IsDir {
		t.Fatal("new entry is not a directory")
	}

	if record.Entry.StartCluster < 2 {
		t.Fatalf(
			"new directory cluster = %d, want allocated cluster",
			record.Entry.StartCluster,
		)
	}

	directoryData, err := fs.readCluster(
		record.Entry.StartCluster,
	)
	if err != nil {
		t.Fatal(err)
	}

	dotEntry, err := encodeSpecialDirectoryEntry(
		".",
		record.Entry.StartCluster,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(
		directoryData[0:32],
		dotEntry[:],
	) {
		t.Fatal(
			"first directory entry is not the expected '.' entry",
		)
	}

	dotDotEntry, err := encodeSpecialDirectoryEntry(
		"..",
		0,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(
		directoryData[32:64],
		dotDotEntry[:],
	) {
		t.Fatal(
			"second directory entry is not the expected '..' entry",
		)
	}

	if directoryData[64] != 0x00 {
		t.Fatalf(
			"directory entry after '..' = 0x%02x, want end marker",
			directoryData[64],
		)
	}
}

// Checks that long-name directory creation gets an LFN,
// a generated short alias, and the directory attribute.
func TestMkdirLongFilename(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 30

		rootCluster = uint32(2)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	boot := BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: sectorsPerCluster,
		NumberOfFATs:      numberOfFATs,
		SectorsPerFAT:     sectorsPerFAT,
		TotalSectors:      totalSectors,
		RootCluster:       rootCluster,
	}

	layout := Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: int64(sectorSize),
	}

	data[int(dataOffset)+32] = 0x00

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	name := "A reasonably long directory name"

	if err := fs.Mkdir(name); err != nil {
		t.Fatal(err)
	}

	record, err := fs.findDirectoryEntry(
		rootCluster,
		name,
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("created directory was not found")
	}

	if !record.Entry.IsDir {
		t.Fatal("created entry is not a directory")
	}

	if record.Entry.Name != name {
		t.Fatalf(
			"directory name = %q, want %q",
			record.Entry.Name,
			name,
		)
	}

	if record.Entry.ShortName != "AREASO~1" {
		t.Fatalf(
			"short name = %q, want %q",
			record.Entry.ShortName,
			"AREASO~1",
		)
	}

	if record.Entry.LFNEntries == 0 {
		t.Fatal("created directory has no LFN entries")
	}

	chain, err := fs.ClusterChain(record.Entry.StartCluster)
	if err != nil {
		t.Fatal(err)
	}

	if len(chain) != 1 {
		t.Fatalf(
			"directory cluster count = %d, want 1",
			len(chain),
		)
	}

	records, err := fs.readDirectoryRecords(
		record.Entry.StartCluster,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(records) != 2 {
		t.Fatalf(
			"new directory contains %d records, want 2",
			len(records),
		)
	}

	if records[0].Entry.Name != "." {
		t.Fatalf(
			"first directory entry = %q, want \".\"",
			records[0].Entry.Name,
		)
	}

	if !records[0].Entry.IsDir {
		t.Fatal("'.' entry is not a directory")
	}

	if records[0].Entry.StartCluster != record.Entry.StartCluster {
		t.Fatalf(
			"'.' cluster = %d, want %d",
			records[0].Entry.StartCluster,
			record.Entry.StartCluster,
		)
	}

	if records[1].Entry.Name != ".." {
		t.Fatalf(
			"second directory entry = %q, want \"..\"",
			records[1].Entry.Name,
		)
	}

	if !records[1].Entry.IsDir {
		t.Fatal("'..' entry is not a directory")
	}

	if records[1].Entry.StartCluster != 0 {
		t.Fatalf(
			"'..' cluster = %d, want 0",
			records[1].Entry.StartCluster,
		)
	}
}

// Reject duplicate directory creation without mutation.
// Confirms that the duplicate check occurs before allocating a new cluster
// or touching the directory.
func TestMkdirDuplicateDoesNotMutate(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 30

		rootCluster      = uint32(2)
		directoryCluster = uint32(3)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(directoryCluster)*4:],
		fat32EOF,
	)

	boot := BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: sectorsPerCluster,
		NumberOfFATs:      numberOfFATs,
		SectorsPerFAT:     sectorsPerFAT,
		TotalSectors:      totalSectors,
		RootCluster:       rootCluster,
	}

	layout := Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: int64(sectorSize),
	}

	shortName, err := encodeShortName("MUSIC")
	if err != nil {
		t.Fatal(err)
	}

	directoryEntry := encodeDirectoryEntry(
		shortName,
		0x10,
		directoryCluster,
		0,
	)

	directoryOffset := int(dataOffset)

	copy(
		data[directoryOffset:directoryOffset+32],
		directoryEntry[:],
	)

	data[directoryOffset+32] = 0x00

	before := append([]byte(nil), data...)

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	err = fs.Mkdir("MUSIC")
	if err == nil {
		t.Fatal("Mkdir succeeded, want duplicate-name error")
	}

	if !bytes.Equal(buffer.data, before) {
		t.Fatal("filesystem was modified when creating duplicate directory")
	}
}

// Create a directory inside another directory.
// Verifies that Mkdir correctly resolves the parent path
// and allocates the new directory independently of the root directory.
func TestMkdirInSubdirectory(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 40

		rootCluster   = uint32(2)
		parentCluster = uint32(3)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(parentCluster)*4:],
		fat32EOF,
	)

	boot := BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: sectorsPerCluster,
		NumberOfFATs:      numberOfFATs,
		SectorsPerFAT:     sectorsPerFAT,
		TotalSectors:      totalSectors,
		RootCluster:       rootCluster,
	}

	layout := Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: int64(sectorSize),
	}

	parentShortName, err := encodeShortName("PARENT")
	if err != nil {
		t.Fatal(err)
	}

	parentEntry := encodeDirectoryEntry(
		parentShortName,
		0x10,
		parentCluster,
		0,
	)

	rootOffset := int(dataOffset)

	copy(
		data[rootOffset:rootOffset+32],
		parentEntry[:],
	)

	data[rootOffset+32] = 0x00

	parentOffset := int(dataOffset) +
		(int(parentCluster)-2)*int(layout.BytesPerCluster)

	data[parentOffset+32] = 0x00

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	if err := fs.Mkdir("PARENT/CHILD"); err != nil {
		t.Fatal(err)
	}

	record, err := fs.findDirectoryEntry(
		parentCluster,
		"CHILD",
	)
	if err != nil {
		t.Fatal(err)
	}

	if record == nil {
		t.Fatal("child directory was not created")
	}

	if !record.Entry.IsDir {
		t.Fatal("child entry is not a directory")
	}

	childCluster := record.Entry.StartCluster

	if childCluster < 2 {
		t.Fatalf(
			"child start cluster = %d, want allocated cluster",
			childCluster,
		)
	}

	if childCluster == parentCluster {
		t.Fatal("child directory reused parent cluster")
	}

	rootChild, err := fs.findDirectoryEntry(
		rootCluster,
		"CHILD",
	)
	if err != nil {
		t.Fatal(err)
	}

	if rootChild != nil {
		t.Fatal("child directory was unexpectedly created in root")
	}

	childData, err := fs.readCluster(childCluster)
	if err != nil {
		t.Fatal(err)
	}

	dotEntry, err := encodeSpecialDirectoryEntry(
		".",
		childCluster,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(
		childData[0:32],
		dotEntry[:],
	) {
		t.Fatal(
			"first child-directory entry is not the expected '.' entry",
		)
	}

	dotDotEntry, err := encodeSpecialDirectoryEntry(
		"..",
		parentCluster,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(
		childData[32:64],
		dotDotEntry[:],
	) {
		t.Fatal(
			"second child-directory entry is not the expected '..' entry",
		)
	}

	if childData[64] != 0x00 {
		t.Fatalf(
			"directory entry after '..' = 0x%02x, want end marker",
			childData[64],
		)
	}
}

// Verify nested .. points to the actual parent.
//
// This isolates the distinction between:
// root-level directory: .. → 0
// nested directory: .. → actual parent cluster.
func TestMkdirInSubdirectoryDotDotPointsToParent(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 40

		rootCluster   = uint32(2)
		parentCluster = uint32(3)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(parentCluster)*4:],
		fat32EOF,
	)

	boot := BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: sectorsPerCluster,
		NumberOfFATs:      numberOfFATs,
		SectorsPerFAT:     sectorsPerFAT,
		TotalSectors:      totalSectors,
		RootCluster:       rootCluster,
	}

	layout := Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: int64(sectorSize),
	}

	parentShortName, err := encodeShortName("PARENT")
	if err != nil {
		t.Fatal(err)
	}

	parentEntry := encodeDirectoryEntry(
		parentShortName,
		0x10,
		parentCluster,
		0,
	)

	rootOffset := int(dataOffset)

	copy(
		data[rootOffset:rootOffset+32],
		parentEntry[:],
	)

	data[rootOffset+32] = 0x00

	parentOffset := int(dataOffset) +
		(int(parentCluster)-2)*int(layout.BytesPerCluster)

	data[parentOffset+32] = 0x00

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	if err := fs.Mkdir("PARENT/CHILD"); err != nil {
		t.Fatal(err)
	}

	child, err := fs.findDirectoryEntry(
		parentCluster,
		"CHILD",
	)
	if err != nil {
		t.Fatal(err)
	}

	if child == nil {
		t.Fatal("child directory was not created")
	}

	childData, err := fs.readCluster(
		child.Entry.StartCluster,
	)
	if err != nil {
		t.Fatal(err)
	}

	dotDotEntry := childData[32:64]

	gotCluster :=
		uint32(binary.LittleEndian.Uint16(
			dotDotEntry[0x14:0x16],
		))<<16 |
			uint32(binary.LittleEndian.Uint16(
				dotDotEntry[0x1A:0x1C],
			))

	if gotCluster != parentCluster {
		t.Fatalf(
			"'..' cluster = %d, want parent cluster %d",
			gotCluster,
			parentCluster,
		)
	}
}

// Reject Mkdir when the parent doesn't exist.
// This verifies the ordering:
//
//	resolve parent → validate parent → allocate cluster → write directory
//
// rather than allocating a cluster before discovering that the parent path is invalid.
func TestMkdirMissingParentDoesNotMutate(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 30

		rootCluster = uint32(2)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	boot := BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: sectorsPerCluster,
		NumberOfFATs:      numberOfFATs,
		SectorsPerFAT:     sectorsPerFAT,
		TotalSectors:      totalSectors,
		RootCluster:       rootCluster,
	}

	layout := Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: int64(sectorSize),
	}

	data[int(dataOffset)+32] = 0x00

	before := append([]byte(nil), data...)

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	err := fs.Mkdir(
		"MISSING/CHILD",
	)
	if err == nil {
		t.Fatal("Mkdir succeeded, want missing-parent error")
	}

	if !bytes.Equal(buffer.data, before) {
		t.Fatal("filesystem was modified when parent directory was missing")
	}
}

// Test Mkdir rollback when writing the new directory cluster fails
// This exercises the Mkdir rollback path after the cluster has already been allocated.
// Verifies that an allocation is not leaked if initialization of the new directory cluster fails.
func TestMkdirRollsBackClusterOnDirectoryClusterWriteFailure(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 30

		rootCluster = uint32(2)
		newCluster  = uint32(3)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	boot := BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: sectorsPerCluster,
		NumberOfFATs:      numberOfFATs,
		SectorsPerFAT:     sectorsPerFAT,
		TotalSectors:      totalSectors,
		RootCluster:       rootCluster,
	}

	layout := Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: int64(sectorSize),
	}

	data[int(dataOffset)+32] = 0x00

	newClusterOffset := dataOffset +
		int64(newCluster-2)*layout.BytesPerCluster

	buffer := &failDirectoryAndFATRollbackBuffer{
		data: data,

		directoryOffset:         newClusterOffset,
		failDirectoryWriteCount: 1,

		fatOffset: fatOffset,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	err := fs.Mkdir("CHILD")
	if err == nil {
		t.Fatal("Mkdir succeeded, want directory-cluster write error")
	}

	fatValue := binary.LittleEndian.Uint32(
		buffer.data[int(fatOffset)+int(newCluster)*4:],
	)

	if fatValue != 0 {
		t.Fatalf(
			"new cluster FAT entry = 0x%08x, want free",
			fatValue,
		)
	}

	record, err := fs.findDirectoryEntry(
		rootCluster,
		"CHILD",
	)
	if err != nil {
		t.Fatal(err)
	}

	if record != nil {
		t.Fatal("directory entry was created despite cluster write failure")
	}
}

// Rollback when creating the parent directory entry fails.
//
// The new directory cluster has been allocated and initialized,
// but the parent directory cannot be updated.
//
// This should verify the complete rollback sequence:
//
//	allocate cluster → initialize cluster → parent-directory write fails → free allocated cluster → no directory entry remains.
func TestMkdirRollsBackClusterOnDirectoryWriteFailure(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 30

		rootCluster = uint32(2)
		newCluster  = uint32(3)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	boot := BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: sectorsPerCluster,
		NumberOfFATs:      numberOfFATs,
		SectorsPerFAT:     sectorsPerFAT,
		TotalSectors:      totalSectors,
		RootCluster:       rootCluster,
	}

	layout := Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: int64(sectorSize),
	}

	rootOffset := dataOffset

	data[int(rootOffset)+32] = 0x00

	rootDirectoryBefore := append(
		[]byte(nil),
		data[int(rootOffset):int(rootOffset)+int(sectorSize)]...,
	)

	parentDirectoryOffset := dataOffset +
		int64(rootCluster-2)*layout.BytesPerCluster

	buffer := &failDirectoryAndFATRollbackBuffer{
		data: data,

		directoryOffset:    parentDirectoryOffset,
		failDirectoryWrite: true,

		fatOffset: fatOffset,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	err := fs.Mkdir("CHILD")
	if err == nil {
		t.Fatal("Mkdir succeeded, want directory write error")
	}

	fatValue := binary.LittleEndian.Uint32(
		buffer.data[int(fatOffset)+int(newCluster)*4:],
	)

	if fatValue != 0 {
		t.Fatalf(
			"new cluster FAT entry = 0x%08x, want free",
			fatValue,
		)
	}

	rootDirectoryAfter := buffer.data[int(rootOffset) : int(rootOffset)+int(sectorSize)]

	if !bytes.Equal(rootDirectoryAfter, rootDirectoryBefore) {
		t.Fatal("root directory was modified despite directory write failure")
	}

	record, err := fs.findDirectoryEntry(
		rootCluster,
		"CHILD",
	)
	if err != nil {
		t.Fatal(err)
	}

	if record != nil {
		t.Fatal("directory entry exists despite Mkdir failure")
	}
}

// Verify unique short-name generation for directories
//
// Ensure a long directory name doesn't accidentally reuse
// another directory's short alias.
func TestMkdirLongFilenameCollision(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 2
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 100

		rootCluster = uint32(2)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize) * 2

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	boot := BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: sectorsPerCluster,
		NumberOfFATs:      numberOfFATs,
		SectorsPerFAT:     sectorsPerFAT,
		TotalSectors:      totalSectors,
		RootCluster:       rootCluster,
	}

	layout := Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: int64(sectorSize),
	}

	rootOffset := int(dataOffset)

	data[rootOffset+32] = 0x00

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	if err := fs.Mkdir("Some long directory name"); err != nil {
		t.Fatal(err)
	}

	if err := fs.Mkdir("Some long directory name 2"); err != nil {
		t.Fatal(err)
	}

	first, err := fs.findDirectoryEntry(
		rootCluster,
		"Some long directory name",
	)
	if err != nil {
		t.Fatal(err)
	}

	if first == nil {
		t.Fatal("first directory was not created")
	}

	second, err := fs.findDirectoryEntry(
		rootCluster,
		"Some long directory name 2",
	)
	if err != nil {
		t.Fatal(err)
	}

	if second == nil {
		t.Fatal("second directory was not created")
	}

	if strings.EqualFold(
		first.Entry.ShortName,
		second.Entry.ShortName,
	) {
		t.Fatalf(
			"directories have the same short name: %q",
			first.Entry.ShortName,
		)
	}

	if first.Entry.ShortName != "SOMELO~1" {
		t.Fatalf(
			"first short name = %q, want SOMELO~1",
			first.Entry.ShortName,
		)
	}

	if second.Entry.ShortName != "SOMELO~2" {
		t.Fatalf(
			"second short name = %q, want SOMELO~2",
			second.Entry.ShortName,
		)
	}

	if !first.Entry.IsDir || !second.Entry.IsDir {
		t.Fatal("created entries are not directories")
	}
}

// Verify Mkdir does not overwrite an existing directory entry.
//
// Confirm the duplicate check happens before allocation,
// so a failed duplicate Mkdir cannot leak a cluster or modify the directory.
func TestMkdirDuplicateDoesNotMutateDirectory(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 40

		rootCluster = uint32(2)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	boot := BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: sectorsPerCluster,
		NumberOfFATs:      numberOfFATs,
		SectorsPerFAT:     sectorsPerFAT,
		TotalSectors:      totalSectors,
		RootCluster:       rootCluster,
	}

	layout := Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: int64(sectorSize),
	}

	rootOffset := int(dataOffset)

	data[rootOffset+32] = 0x00

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	if err := fs.Mkdir("TESTDIR"); err != nil {
		t.Fatal(err)
	}

	rootBefore := append(
		[]byte(nil),
		buffer.data[rootOffset:rootOffset+int(sectorSize)]...,
	)

	fatBefore := append(
		[]byte(nil),
		buffer.data[int(fatOffset):int(fatOffset)+int(sectorSize)]...,
	)

	err := fs.Mkdir("TESTDIR")
	if err == nil {
		t.Fatal("duplicate Mkdir succeeded")
	}

	rootAfter := buffer.data[rootOffset : rootOffset+int(sectorSize)]

	if !bytes.Equal(rootAfter, rootBefore) {
		t.Fatal("root directory was modified by duplicate Mkdir")
	}

	fatAfter := buffer.data[int(fatOffset) : int(fatOffset)+int(sectorSize)]

	if !bytes.Equal(fatAfter, fatBefore) {
		t.Fatal("FAT was modified by duplicate Mkdir")
	}
}

// Test Mkdir after parent-directory expansion.
//
// Sequence:
// 1. Root directory is full at cluster 2.
// 2. createDirectoryRecord() calls expandDirectory().
// 3. expandDirectory() allocates cluster 3 for the expanded root directory and links 2 → 3.
// 4. Mkdir() then allocates cluster 4 for CHILD.
// 5. The CHILD directory entry is written into cluster 3.
func TestMkdirExpandsParentDirectory(t *testing.T) {
	const (
		sectorSize        uint16 = 512
		sectorsPerCluster uint8  = 1
		sectorsPerFAT     uint32 = 1
		numberOfFATs      uint8  = 1
		totalSectors      uint32 = 40

		rootCluster = uint32(2)
	)

	data := make([]byte, int(sectorSize)*int(totalSectors))

	fatOffset := int64(0)
	dataOffset := int64(sectorSize)

	binary.LittleEndian.PutUint32(
		data[int(fatOffset)+int(rootCluster)*4:],
		fat32EOF,
	)

	boot := BootSector{
		BytesPerSector:    sectorSize,
		SectorsPerCluster: sectorsPerCluster,
		NumberOfFATs:      numberOfFATs,
		SectorsPerFAT:     sectorsPerFAT,
		TotalSectors:      totalSectors,
		RootCluster:       rootCluster,
	}

	layout := Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: int64(sectorSize),
	}

	// Fill the root directory with live entries so that there is
	// no contiguous free slot and Mkdir must expand the directory.
	for i := 0; i < 16; i++ {
		name := fmt.Sprintf("FILE%02d.TXT", i)

		shortName, err := encodeShortName(name)
		if err != nil {
			t.Fatal(err)
		}

		entry := encodeDirectoryEntry(
			shortName,
			0x20,
			0,
			0,
		)

		offset := int(dataOffset) + i*32

		copy(
			data[offset:offset+32],
			entry[:],
		)
	}

	buffer := &failOnWriteCountAt{
		data:       data,
		failOffset: -1,
	}

	fs := NewReadWrite(
		bytes.NewReader(data),
		buffer,
	)

	fs.boot = boot
	fs.layout = layout

	if err := fs.Mkdir("CHILD"); err != nil {
		t.Fatal(err)
	}

	entry, err := fs.findDirectoryEntry(
		rootCluster,
		"CHILD",
	)
	if err != nil {
		t.Fatal(err)
	}

	if entry == nil {
		t.Fatal("CHILD directory was not created")
	}

	if !entry.Entry.IsDir {
		t.Fatal("CHILD is not a directory")
	}

	childCluster := entry.Entry.StartCluster

	if childCluster < 2 {
		t.Fatalf(
			"CHILD start cluster = %d, want valid cluster >= 2",
			childCluster,
		)
	}

	// Verify that the root directory was expanded.
	//
	// The allocator is free to choose any available cluster for
	// the expansion and for CHILD, so do not assume specific
	// cluster numbers.
	rootChain, err := fs.readClusterChain(rootCluster)
	if err != nil {
		t.Fatal(err)
	}

	if len(rootChain) < 2 {
		t.Fatalf(
			"root directory chain has %d clusters, want at least 2",
			len(rootChain),
		)
	}

	// The CHILD directory entry must live somewhere in the
	// expanded root directory chain.
	foundChild := false

	for _, cluster := range rootChain {
		records, err := fs.readDirectoryRecords(cluster)
		if err != nil {
			t.Fatal(err)
		}

		for _, record := range records {
			if strings.EqualFold(record.Entry.Name, "CHILD") {
				foundChild = true
				break
			}
		}

		if foundChild {
			break
		}
	}

	if !foundChild {
		t.Fatal("CHILD entry was not found in expanded root directory")
	}

	// The CHILD directory itself must contain "." and "..".
	records, err := fs.readDirectoryRecords(childCluster)
	if err != nil {
		t.Fatal(err)
	}

	if len(records) != 2 {
		t.Fatalf(
			"new directory contains %d records, want 2",
			len(records),
		)
	}

	if records[0].Entry.Name != "." {
		t.Fatalf(
			"first entry = %q, want %q",
			records[0].Entry.Name,
			".",
		)
	}

	if records[0].Entry.StartCluster != childCluster {
		t.Fatalf(
			"'.' cluster = %d, want %d",
			records[0].Entry.StartCluster,
			childCluster,
		)
	}

	if records[1].Entry.Name != ".." {
		t.Fatalf(
			"second entry = %q, want %q",
			records[1].Entry.Name,
			"..",
		)
	}

	if records[1].Entry.StartCluster != 0 {
		t.Fatalf(
			"'..' cluster = %d, want 0 for a directory directly under the root",
			records[1].Entry.StartCluster,
		)
	}
}

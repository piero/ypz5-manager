// Package fat32 reads and writes a FAT32 filesystem stored in an
// io.ReaderAt / io.WriterAt, such as a disk image or a raw device.
//
// Every top-level mutation (WriteFile, ReplaceFile, Mkdir, Delete,
// RemoveAll) is ordered so that a crash or failed write can only leak
// clusters, never cross-link them, and ends by updating FSInfo and
// flushing the writer.
//
// Files in this package:
//
//	filesystem.go        Filesystem, Open, flushing (commit)
//	operations.go        public mutations: WriteFile, ReplaceFile, Mkdir, Delete, RemoveAll
//	path.go              path resolution, ErrNotFound
//	directory.go         reading directories
//	directory_write.go   adding, growing and deleting directory records
//	entry.go             the 32-byte directory entry, attributes
//	shortname.go         8.3 names and NT case flags
//	lfn.go               long file names
//	timestamp.go         FAT dates and times
//	fat.go               the in-memory FAT, allocation and cluster chains
//	cluster.go           reading and writing data clusters
//	file.go              reading and writing file contents
//	fsinfo.go            the FSInfo free-cluster count
//	boot_sector.go       boot sector, layout.go: derived geometry
//	partition.go         finding the filesystem inside an MBR disk
package fat32

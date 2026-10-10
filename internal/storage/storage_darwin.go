package storage

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// dkiocSynchronizeCache is DKIOCSYNCHRONIZECACHE, _IO('d', 22) from
// <sys/disk.h>: it asks a disk to flush its write cache.
const dkiocSynchronizeCache = 0x20006416

// fullSync flushes f all the way to the medium.
//
// For a raw device (/dev/rdiskN) writes bypass the buffer cache, so only
// the drive's own cache needs flushing, which DKIOCSYNCHRONIZECACHE does;
// fsync on a character device fails with ENOTTY.
//
// For a regular file fsync only hands data to the drive, which may still
// cache it; F_FULLFSYNC asks the drive to flush too, falling back to a
// plain fsync where it is unsupported.
func fullSync(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}

	if info.Mode()&os.ModeCharDevice != 0 {
		err := unix.IoctlSetInt(int(f.Fd()), dkiocSynchronizeCache, 0)
		if err == nil || isUnsupported(err) {
			return nil
		}

		return &os.PathError{Op: "sync", Path: f.Name(), Err: err}
	}

	if _, err := unix.FcntlInt(f.Fd(), unix.F_FULLFSYNC, 0); err == nil {
		return nil
	} else if !isUnsupported(err) {
		return err
	}

	return f.Sync()
}

func isUnsupported(err error) bool {
	return errors.Is(err, unix.ENOTSUP) ||
		errors.Is(err, unix.ENOTTY) ||
		errors.Is(err, unix.EINVAL)
}

// mountedDevices returns the device nodes of all mounted filesystems,
// e.g. "/dev/disk4s1".
func mountedDevices() ([]string, error) {
	n, err := unix.Getfsstat(nil, unix.MNT_NOWAIT)
	if err != nil {
		return nil, err
	}

	stats := make([]unix.Statfs_t, n)

	n, err = unix.Getfsstat(stats, unix.MNT_NOWAIT)
	if err != nil {
		return nil, err
	}

	devices := make([]string, 0, n)

	for _, stat := range stats[:n] {
		devices = append(devices, unix.ByteSliceToString(stat.Mntfromname[:]))
	}

	return devices, nil
}

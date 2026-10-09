package main

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// fullSync flushes f all the way to the medium. On macOS fsync only
// hands data to the drive, which may still cache it; F_FULLFSYNC asks
// the drive to flush too. Raw devices may not support it, in which case
// a plain fsync is used.
func fullSync(f *os.File) error {
	_, err := unix.FcntlInt(f.Fd(), unix.F_FULLFSYNC, 0)
	if err == nil {
		return nil
	}

	if errors.Is(err, unix.ENOTSUP) ||
		errors.Is(err, unix.ENOTTY) ||
		errors.Is(err, unix.EINVAL) {
		return f.Sync()
	}

	return err
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

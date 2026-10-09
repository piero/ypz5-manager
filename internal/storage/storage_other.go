//go:build !darwin

package storage

import "os"

func fullSync(f *os.File) error {
	return f.Sync()
}

// mountedDevices is only implemented on macOS; elsewhere the mount
// check is skipped.
func mountedDevices() ([]string, error) {
	return nil, nil
}

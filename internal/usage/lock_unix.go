//go:build !windows

package usage

import (
	"os"

	"golang.org/x/sys/unix"
)

// lockExclusive blocks until it holds an exclusive flock on f.
func lockExclusive(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX)
}

// unlockFile releases the flock held on f.
func unlockFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}

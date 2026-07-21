package usage

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockAllBytes covers the whole file, which is enough since the sidecar lock
// file carries no content.
const lockAllBytes = ^uint32(0)

// lockExclusive blocks until it holds an exclusive LockFileEx lock on f.
func lockExclusive(f *os.File) error {
	ol := new(windows.Overlapped)
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, lockAllBytes, lockAllBytes, ol)
}

// unlockFile releases the lock held on f.
func unlockFile(f *os.File) error {
	ol := new(windows.Overlapped)
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, lockAllBytes, lockAllBytes, ol)
}

package usage

import (
	"fmt"
	"os"
	"path/filepath"
)

// withFileLock runs fn while holding an exclusive advisory lock on a sidecar
// `<path>.lock` file, so a load→refresh→save sequence cannot interleave with
// another claudeops process (or another goroutine) touching the same
// credentials. The lock lives in a sidecar because SaveCredentials replaces
// `path` via rename, which would drop a lock taken on the original inode.
func withFileLock(path string, fn func() error) error {
	lockPath := path + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open credentials lock %s: %w", lockPath, err)
	}
	defer f.Close()

	if err := lockExclusive(f); err != nil {
		return fmt.Errorf("lock %s: %w", lockPath, err)
	}
	defer func() { _ = unlockFile(f) }()

	return fn()
}

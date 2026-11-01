//go:build !windows

package live

import (
	"errors"
	"syscall"
)

// processAlive says whether a process exists. Not being allowed to signal it still means it does.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

//go:build windows

package live

import "os"

// processAlive says whether a process exists: on Windows, finding one fails once it is gone.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = process.Release()
	return true
}

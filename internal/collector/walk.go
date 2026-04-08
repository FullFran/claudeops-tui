package collector

import (
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

func addDirsRecursively(w *fsnotify.Watcher, root string) error {
	_ = os.MkdirAll(root, 0o755)
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			_ = w.Add(path)
		}
		return nil
	})
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	if err != nil {
		return false
	}
	return info.IsDir()
}

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

// jsonlFilesUnder returns every .jsonl file in the tree rooted at dir. It
// covers files created just before the directory was added to the watcher,
// whose events are never delivered.
func jsonlFilesUnder(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && filepath.Ext(path) == ".jsonl" {
			out = append(out, path)
		}
		return nil
	})
	return out
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	if err != nil {
		return false
	}
	return info.IsDir()
}

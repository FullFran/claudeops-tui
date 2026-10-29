package pi

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed extension/claudeops.ts
var ExtensionSource []byte

// ExtensionFileName is the name of the installed extension file.
const ExtensionFileName = "claudeops.ts"

// ExtensionDirs returns all directories where Pi extensions can be installed.
func ExtensionDirs() []string {
	var dirs []string
	if env := os.Getenv("PI_CODING_AGENT_DIR"); env != "" {
		dirs = append(dirs, filepath.Join(env, "extensions"))
	}
	if env := os.Getenv("GENTLE_SHELL_AGENT_DIR"); env != "" {
		dirs = append(dirs, filepath.Join(env, "extensions"))
	}
	if len(dirs) > 0 {
		return dirs
	}
	home, err := os.UserHomeDir()
	if err == nil {
		dirs = append(dirs,
			filepath.Join(home, ".gentle-shell", "agent", "extensions"),
			filepath.Join(home, ".pi", "agent", "extensions"),
		)
	}
	return dirs
}

// InstallExtension writes the claudeops.ts extension into existing or target extension dirs.
// If force is true, overwrites existing file. Returns list of installed file paths.
func InstallExtension(force bool) ([]string, error) {
	dirs := ExtensionDirs()
	var installed []string

	for _, dir := range dirs {
		parent := filepath.Dir(dir)
		// Only install in an agent directory if its parent agent dir exists or was explicitly configured
		if _, err := os.Stat(parent); err != nil && os.Getenv("PI_CODING_AGENT_DIR") == "" && os.Getenv("GENTLE_SHELL_AGENT_DIR") == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return installed, fmt.Errorf("create extension dir %s: %w", dir, err)
		}
		dest := filepath.Join(dir, ExtensionFileName)
		if fi, err := os.Stat(dest); err == nil && !force && fi.Size() > 0 {
			// Already installed
			installed = append(installed, dest)
			continue
		}
		if err := os.WriteFile(dest, ExtensionSource, 0o644); err != nil {
			return installed, fmt.Errorf("write %s: %w", dest, err)
		}
		installed = append(installed, dest)
	}

	if len(installed) == 0 && len(dirs) > 0 {
		// Fallback: install in the primary directory
		primary := dirs[0]
		if err := os.MkdirAll(primary, 0o755); err != nil {
			return nil, err
		}
		dest := filepath.Join(primary, ExtensionFileName)
		if err := os.WriteFile(dest, ExtensionSource, 0o644); err != nil {
			return nil, err
		}
		installed = append(installed, dest)
	}

	return installed, nil
}

// UninstallExtension removes claudeops.ts from all candidate extension dirs.
func UninstallExtension() ([]string, error) {
	var removed []string
	for _, dir := range ExtensionDirs() {
		dest := filepath.Join(dir, ExtensionFileName)
		if _, err := os.Stat(dest); err == nil {
			if err := os.Remove(dest); err == nil {
				removed = append(removed, dest)
			}
		}
	}
	return removed, nil
}

// ExtensionStatusInfo reports the install status of claudeops.ts in candidate dirs.
type ExtensionStatusInfo struct {
	Dir       string
	Path      string
	Installed bool
	Size      int64
}

// ExtensionStatus lists install status across candidate directories.
func ExtensionStatus() []ExtensionStatusInfo {
	var out []ExtensionStatusInfo
	for _, dir := range ExtensionDirs() {
		dest := filepath.Join(dir, ExtensionFileName)
		fi, err := os.Stat(dest)
		installed := err == nil && !fi.IsDir()
		var size int64
		if installed {
			size = fi.Size()
		}
		out = append(out, ExtensionStatusInfo{
			Dir:       dir,
			Path:      dest,
			Installed: installed,
			Size:      size,
		})
	}
	return out
}

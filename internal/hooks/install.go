package hooks

import (
	"fmt"
	"os"
)

// StatusReport summarises which managed events currently have a claudeops
// hook installed and whether the binary they point to is reachable.
type StatusReport struct {
	SettingsPath string
	Binary       string
	BinaryExists bool
	Events       map[string]bool // event name → installed
}

// Install merges the managed claudeops hook entries into the settings file at
// path. binary is the absolute path of the claudeops executable to invoke
// (typically os.Executable()). Install is idempotent: re-running replaces any
// pre-existing entries tagged "_source: claudeops".
func Install(path, binary string) error {
	s, err := Load(path)
	if err != nil {
		return err
	}
	stripOurs(s)
	entry := Entry{
		Type:    "command",
		Command: fmt.Sprintf("%s hooks handle", binary),
		Timeout: 5,
		Source:  SourceMarker,
	}
	for _, ev := range ManagedEvents {
		s.Hooks[ev] = append(s.Hooks[ev], Group{Matcher: "", Hooks: []Entry{entry}})
	}
	return Save(path, s)
}

// Uninstall removes every hook entry tagged as ours, prunes now-empty groups
// and events, and saves the result.
func Uninstall(path string) error {
	s, err := Load(path)
	if err != nil {
		return err
	}
	stripOurs(s)
	return Save(path, s)
}

// Status reports which of ManagedEvents currently have a claudeops-tagged
// entry in the settings file.
func Status(path, binary string) (StatusReport, error) {
	r := StatusReport{
		SettingsPath: path,
		Binary:       binary,
		Events:       map[string]bool{},
	}
	for _, ev := range ManagedEvents {
		r.Events[ev] = false
	}
	if _, err := os.Stat(binary); err == nil {
		r.BinaryExists = true
	}
	s, err := Load(path)
	if err != nil {
		return r, err
	}
	for _, ev := range ManagedEvents {
		for _, g := range s.Hooks[ev] {
			for _, h := range g.Hooks {
				if h.Source == SourceMarker {
					r.Events[ev] = true
				}
			}
		}
	}
	return r, nil
}

// stripOurs removes all claudeops-tagged entries from s.Hooks and deletes
// groups/events that become empty.
func stripOurs(s *Settings) {
	for ev, groups := range s.Hooks {
		kept := make([]Group, 0, len(groups))
		for _, g := range groups {
			entries := make([]Entry, 0, len(g.Hooks))
			for _, h := range g.Hooks {
				if h.Source == SourceMarker {
					continue
				}
				entries = append(entries, h)
			}
			if len(entries) > 0 {
				g.Hooks = entries
				kept = append(kept, g)
			}
		}
		if len(kept) == 0 {
			delete(s.Hooks, ev)
		} else {
			s.Hooks[ev] = kept
		}
	}
}

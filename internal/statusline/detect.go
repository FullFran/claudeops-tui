package statusline

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// DetectAgent names the agent running in the tmux pane you are looking at, or
// "" when there is no tmux, no active pane, or nothing recognisable in it.
//
// The point is to show the quota you are actually spending: sitting in a Claude
// Code pane, the Anthropic window is what matters; sitting in an opencode pane
// wired to OpenAI models, it is the Codex window. Showing both at all times
// makes the interesting number harder to find, not easier.
//
// Resolution has two steps because the interesting agents are Node programs and
// the process name alone says "node":
//
//  1. Ask tmux for the active pane's command and pid.
//  2. If that command is a runtime rather than an agent, walk the pane's
//     process and its children looking for a known agent in the command line.
//
// Everything here is best effort. A miss returns "" and the caller falls back
// to its configured default.
func DetectAgent(known map[string]string) string {
	cmd, pid := activePane()
	if cmd == "" {
		return ""
	}
	if name := matchKnown(cmd, known); name != "" {
		return name
	}
	if isRuntime(cmd) && pid != "" {
		return scanProcessTree(pid, known)
	}
	return ""
}

// activePane returns the command and pid of the pane the user is looking at.
func activePane() (string, string) {
	if os.Getenv("TMUX") == "" {
		return "", ""
	}
	out, err := exec.Command("tmux", "display-message", "-p",
		"#{pane_current_command}\t#{pane_pid}").Output()
	if err != nil {
		return "", ""
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "\t", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

// matchKnown resolves a command name against the configured agent map.
// Matching is on the basename and case-insensitive, so "/usr/bin/Claude" and
// "claude" both land on the same entry.
func matchKnown(cmd string, known map[string]string) string {
	base := strings.ToLower(filepath.Base(cmd))
	if provider, ok := known[base]; ok {
		return provider
	}
	return ""
}

// isRuntime reports whether a process name tells us nothing on its own.
// These are the interpreters agents ship inside.
func isRuntime(cmd string) bool {
	switch strings.ToLower(filepath.Base(cmd)) {
	case "node", "bun", "deno", "python", "python3", "sh", "bash", "zsh", "fish":
		return true
	}
	return false
}

// scanProcessTree looks for a known agent in the command line of pid and its
// direct children. One level down is enough: agents launched through a wrapper
// put the real program there, and going deeper starts matching unrelated tools
// the agent itself spawned.
//
// Matching is per argument, on the basename, and exact. Substring matching
// against the whole command line looks simpler and is wrong: a working
// directory of ~/projects/claude-tools would make every agent look like Claude.
// Arguments are scanned in order so the program name is considered before its
// flags, and the known names are sorted so that a line mentioning two agents
// resolves the same way every time — Go randomises map iteration, which would
// otherwise make detection flicker between redraws.
func scanProcessTree(pid string, known map[string]string) string {
	// Fold the map to lowercase keys once so lookups below are exact.
	lower := make(map[string]string, len(known))
	names := make([]string, 0, len(known))
	for n, prov := range known {
		l := strings.ToLower(n)
		lower[l] = prov
		names = append(names, l)
	}
	sort.Strings(names)

	pids := append([]string{pid}, childPIDs(pid)...)
	for _, p := range pids {
		line := cmdline(p)
		if line == "" {
			continue
		}
		if prov := matchArgs(line, names, lower); prov != "" {
			return prov
		}
	}
	return ""
}

// matchArgs finds the first argument in line whose basename is a known agent.
func matchArgs(line string, names []string, lower map[string]string) string {
	for _, arg := range strings.Fields(line) {
		base := strings.ToLower(filepath.Base(arg))
		for _, n := range names {
			if base == n {
				return lower[n]
			}
		}
	}
	return ""
}

// matchCmdline is matchArgs over a raw agent map; the entry point for tests and
// any caller that already has a command line in hand.
func matchCmdline(line string, known map[string]string) string {
	lower := make(map[string]string, len(known))
	names := make([]string, 0, len(known))
	for n, prov := range known {
		l := strings.ToLower(n)
		lower[l] = prov
		names = append(names, l)
	}
	sort.Strings(names)
	return matchArgs(line, names, lower)
}

func childPIDs(pid string) []string {
	out, err := exec.Command("pgrep", "-P", pid).Output()
	if err != nil {
		return nil
	}
	var pids []string
	for _, l := range strings.Fields(string(out)) {
		pids = append(pids, l)
	}
	return pids
}

// cmdline reads a process's argv, NUL separators replaced by spaces. Linux
// only; elsewhere it returns "" and detection falls back to the pane command.
func cmdline(pid string) string {
	b, err := os.ReadFile(filepath.Join("/proc", pid, "cmdline"))
	if err != nil {
		return ""
	}
	return strings.ReplaceAll(string(b), "\x00", " ")
}

package statusline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var agents = map[string]string{
	"claude":   "claude",
	"opencode": "codex",
	"codex":    "codex",
}

func TestMatchKnown(t *testing.T) {
	cases := map[string]string{
		"claude":            "claude",
		"opencode":          "codex",
		"codex":             "codex",
		"/usr/bin/claude":   "claude", // matched on the basename
		"CLAUDE":            "claude", // and case-insensitively
		"nvim":              "",
		"":                  "",
		"claude-code-extra": "", // no partial matches: exact basename only
	}
	for cmd, want := range cases {
		if got := matchKnown(cmd, agents); got != want {
			t.Errorf("%q: got %q want %q", cmd, got, want)
		}
	}
}

func TestIsRuntime(t *testing.T) {
	// These names say nothing about which agent is running, so detection has to
	// look further into the process tree.
	for _, cmd := range []string{"node", "bun", "deno", "python3", "bash", "/usr/bin/node"} {
		if !isRuntime(cmd) {
			t.Errorf("%q should be treated as a runtime", cmd)
		}
	}
	for _, cmd := range []string{"claude", "opencode", "nvim", "htop"} {
		if isRuntime(cmd) {
			t.Errorf("%q should not be treated as a runtime", cmd)
		}
	}
}

func TestDetectAgentOutsideTmux(t *testing.T) {
	// No tmux means no active pane to inspect; the caller falls back to its
	// configured default rather than guessing.
	t.Setenv("TMUX", "")
	if got := DetectAgent(agents); got != "" {
		t.Errorf("got %q want empty outside tmux", got)
	}
}

func TestScanCmdlineMatchesArgumentsNotPaths(t *testing.T) {
	// The bug this guards: matching a substring of the whole command line makes
	// any path containing an agent name win. A scratch dir literally named
	// /tmp/claude-1000/... made every opencode pane resolve to claude.
	cases := []struct {
		name string
		line string
		want string
	}{
		{
			name: "agent name in the path must not match",
			line: "/bin/sh /tmp/claude-1000/work/fakebin/opencode",
			want: "codex",
		},
		{
			name: "plain invocation",
			line: "/usr/bin/claude --resume",
			want: "claude",
		},
		{
			name: "node wrapper",
			line: "node /usr/lib/node_modules/opencode/bin/opencode.js",
			want: "",
		},
		{
			name: "a directory that merely mentions an agent",
			line: "nvim /home/me/projects/claude-tools/main.go",
			want: "",
		},
		{
			name: "no agent anywhere",
			line: "/usr/bin/htop",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchCmdline(tc.line, agents); got != tc.want {
				t.Errorf("got %q want %q for %q", got, tc.want, tc.line)
			}
		})
	}
}

func TestScanCmdlineIsDeterministic(t *testing.T) {
	// Go randomises map iteration; with two agents on one line the answer must
	// still be the same on every redraw.
	line := "/bin/sh /usr/local/bin/claude /usr/local/bin/opencode"
	first := matchCmdline(line, agents)
	for range 50 {
		if got := matchCmdline(line, agents); got != first {
			t.Fatalf("detection flickered: %q then %q", first, got)
		}
	}
}

func TestScanProcessTreeFindsSelf(t *testing.T) {
	// /proc is Linux-only; elsewhere cmdline returns "" and detection degrades
	// to the pane command, which this test cannot exercise.
	if _, err := os.Stat("/proc/self/cmdline"); err != nil {
		t.Skip("no /proc on this platform")
	}
	// The test binary's argv[0] basename is the map key, so an exact match on
	// it proves the /proc walk reaches a real process.
	pid := itoa(os.Getpid())
	line := cmdline(pid)
	if line == "" {
		t.Skip("cannot read own cmdline")
	}
	base := filepath.Base(strings.Fields(line)[0])
	if got := scanProcessTree(pid, map[string]string{base: "sentinel"}); got != "sentinel" {
		t.Errorf("got %q want sentinel; cmdline was %q", got, line)
	}
}

func TestScanProcessTreeUnknownPID(t *testing.T) {
	if got := scanProcessTree("0", agents); got != "" {
		t.Errorf("got %q want empty for a pid that cannot be read", got)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

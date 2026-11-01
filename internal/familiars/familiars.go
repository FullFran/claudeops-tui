// Package familiars shows familiard pets in other people's terminals without knowing how a pet
// is stored: the familiard program draws its animations as text, and this package asks for them.
// Nothing here runs unless familiard is installed, and it never brings a pet out on the desktop.
package familiars

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ProgramName is what familiard is called on this system.
func ProgramName() string {
	if runtime.GOOS == "windows" {
		return "familiard.exe"
	}
	return "familiard"
}

// ErrOutdated means the installed familiard is too old to draw frames for us.
var ErrOutdated = errors.New("familiard is too old for this, please update it")

const askTimeout = 30 * time.Second

// Client talks to one familiard program.
type Client struct {
	Path string
	// Octants asks for pets drawn with block octants, which are sharper but not known everywhere.
	Octants bool
	// run is the seam tests use. It returns stdout, and an error carrying stderr.
	run func(ctx context.Context, path string, args ...string) ([]byte, error)
}

// Find returns the familiard the user already has, which always wins, or else the one claudeops
// installed in dir. It returns nil when there is none.
func Find(dir string) *Client {
	if path, err := exec.LookPath("familiard"); err == nil {
		return NewClient(path)
	}
	path := filepath.Join(dir, ProgramName())
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return NewClient(path)
	}
	return nil
}

// NewClient returns a client for the familiard at path.
func NewClient(path string) *Client {
	return &Client{Path: path, run: runProgram}
}

func runProgram(ctx context.Context, path string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// DrawsOctants says whether this terminal is known to draw block octants. Ghostty is the one that
// has been seen doing it; anywhere else a missing character would be worse than a coarser pet.
// Inside tmux the terminal's own variable survives, while TERM and TERM_PROGRAM name tmux.
func DrawsOctants(getenv func(string) string) bool {
	return getenv("TERM_PROGRAM") == "ghostty" || getenv("TERM") == "xterm-ghostty" || getenv("GHOSTTY_RESOURCES_DIR") != ""
}

// Clip is one animation, as frames of text with their colors in ANSI sequences.
type Clip struct {
	FPS    int      `json:"fps"`
	Frames []string `json:"frames"`
}

// Pack is a pet drawn at one size.
type Pack struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Rows    int             `json:"rows"`
	Columns int             `json:"columns"`
	Clips   map[string]Clip `json:"clips"`
}

// Frame returns what to show of a clip at a moment, with offset telling pets apart so that they
// do not all move in step. A clip the pack lacks gives "".
func (p *Pack) Frame(clip string, now time.Time, offset int) string {
	c, ok := p.Clips[clip]
	if !ok || len(c.Frames) == 0 || c.FPS <= 0 {
		return ""
	}
	at := now.UnixMilli() * int64(c.FPS) / 1000
	return c.Frames[(at+int64(offset))%int64(len(c.Frames))]
}

// List returns the ids of the pets in the gallery.
func (c *Client) List(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, askTimeout)
	defer cancel()
	out, err := c.run(ctx, c.Path, "pets", "--json")
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("familiard pets: %w", err)
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids, nil
}

// Load asks familiard to draw a pet at this many lines.
func (c *Client) Load(ctx context.Context, id string, rows int) (*Pack, error) {
	ctx, cancel := context.WithTimeout(ctx, askTimeout)
	defer cancel()
	args := []string{"frames", id, "--rows", strconv.Itoa(rows)}
	if c.Octants {
		args = append(args, "--octants")
	}
	out, err := c.run(ctx, c.Path, args...)
	if err != nil {
		if strings.Contains(err.Error(), "unknown command") {
			return nil, ErrOutdated
		}
		return nil, err
	}
	var pack Pack
	if err := json.Unmarshal(out, &pack); err != nil {
		return nil, fmt.Errorf("familiard frames: %w", err)
	}
	if pack.Rows != rows || pack.Columns <= 0 || len(pack.Clips) == 0 {
		return nil, fmt.Errorf("familiard frames: unexpected answer for %s", id)
	}
	pack.Name = printable(pack.Name)
	return &pack, nil
}

// printable keeps a name from someone else's pack from reaching the terminal as a command.
func printable(name string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, name)
}

// Assigner gives each session a pet of its own. The choice looks random but comes from the
// session's id, so a session usually gets the same pet after a restart, and it stays put while
// the program runs. Pets only repeat once there are more sessions than pets.
type Assigner struct {
	chosen map[string]string
}

// NewAssigner returns an Assigner with nothing chosen yet.
func NewAssigner() *Assigner {
	return &Assigner{chosen: map[string]string{}}
}

// Sync forgets sessions and pets that are gone, and chooses for the new sessions.
func (a *Assigner) Sync(sessions, pets []string) map[string]string {
	alive := make(map[string]bool, len(sessions))
	for _, session := range sessions {
		alive[session] = true
	}
	exists := make(map[string]bool, len(pets))
	for _, pet := range pets {
		exists[pet] = true
	}
	inUse := map[string]bool{}
	for session, pet := range a.chosen {
		if alive[session] && exists[pet] {
			inUse[pet] = true
		} else {
			delete(a.chosen, session)
		}
	}
	if len(pets) == 0 {
		return a.chosen
	}
	ordered := append([]string(nil), pets...)
	sort.Strings(ordered)
	newcomers := append([]string(nil), sessions...)
	sort.Strings(newcomers)
	for _, session := range newcomers {
		if _, done := a.chosen[session]; done {
			continue
		}
		start := Seed(session) % len(ordered)
		pet := ordered[start]
		for step := range ordered {
			candidate := ordered[(start+step)%len(ordered)]
			if !inUse[candidate] {
				pet = candidate
				break
			}
		}
		a.chosen[session] = pet
		inUse[pet] = true
	}
	return a.chosen
}

// Seed turns a session's id into a stable number.
func Seed(session string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(session))
	return int(h.Sum32() & 0x7fffffff)
}

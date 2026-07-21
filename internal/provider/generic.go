package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// GenericConfig declares an arbitrary bearer/token HTTP provider entirely in
// configuration, so users can track ANY service CodexBar supports (and more)
// without a code change: point it at an endpoint, tell it where the token
// lives, and map response fields to quota windows.
type GenericConfig struct {
	Name       string              `toml:"name"`
	URL        string              `toml:"url"`
	Method     string              `toml:"method"`      // GET (default) or POST
	AuthScheme string              `toml:"auth_scheme"` // "Bearer" (default) or "token"
	TokenEnv   string              `toml:"token_env"`   // env var holding the token/API key
	TokenFile  string              `toml:"token_file"`  // OR a file whose trimmed contents are the token
	Body       string              `toml:"body"`        // optional request body (POST)
	Note       string              `toml:"note"`        // optional static note line
	Windows    []GenericWindowSpec `toml:"window"`
}

// GenericWindowSpec maps response fields to one quota window. Exactly one of
// UtilPath / RemainPath / (UsedPath+LimitPath) determines the utilization.
type GenericWindowSpec struct {
	Label      string `toml:"label"`
	UtilPath   string `toml:"util_path"`   // dot-path to a used-percent value in [0,100]
	RemainPath string `toml:"remain_path"` // dot-path to a remaining fraction in [0,1]
	UsedPath   string `toml:"used_path"`   // dot-path to a used amount (numerator)
	LimitPath  string `toml:"limit_path"`  // dot-path to a limit amount (denominator)
	ResetPath  string `toml:"reset_path"`  // optional dot-path to an RFC3339 reset time
}

// genericFile is the top-level shape of providers.toml.
type genericFile struct {
	Provider []GenericConfig `toml:"provider"`
}

// LoadGeneric reads providers.toml and returns a Generic provider per entry.
// A missing file yields (nil, nil) so callers can register unconditionally.
func LoadGeneric(path string) ([]*Generic, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f genericFile
	if err := toml.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	out := make([]*Generic, 0, len(f.Provider))
	for _, cfg := range f.Provider {
		out = append(out, NewGeneric(cfg))
	}
	return out, nil
}

// Generic is a configuration-driven Provider.
type Generic struct {
	Config GenericConfig
	HTTP   *http.Client
	Now    func() time.Time
}

// NewGeneric builds a Generic provider from a config.
func NewGeneric(cfg GenericConfig) *Generic {
	return &Generic{
		Config: cfg,
		HTTP:   &http.Client{Timeout: 10 * time.Second},
		Now:    time.Now,
	}
}

// Name implements Provider.
func (g *Generic) Name() string {
	if g.Config.Name != "" {
		return g.Config.Name
	}
	return "custom"
}

// Available reports whether the configured token can be resolved.
func (g *Generic) Available() bool {
	tok, _ := g.token()
	return tok != "" && g.Config.URL != ""
}

// token resolves the token from the env var first, then the file.
func (g *Generic) token() (string, error) {
	if g.Config.TokenEnv != "" {
		if v := os.Getenv(g.Config.TokenEnv); v != "" {
			return v, nil
		}
	}
	if g.Config.TokenFile != "" {
		path := expandHome(g.Config.TokenFile)
		b, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	return "", errors.New("no token configured")
}

// Fetch performs the configured request and maps the response to Usage.
func (g *Generic) Fetch(ctx context.Context) (Usage, error) {
	tok, err := g.token()
	if err != nil {
		return Usage{}, err
	}

	method := g.Config.Method
	if method == "" {
		method = http.MethodGet
	}
	var bodyReader *strings.Reader
	if g.Config.Body != "" {
		bodyReader = strings.NewReader(g.Config.Body)
	} else {
		bodyReader = strings.NewReader("")
	}
	req, err := http.NewRequestWithContext(ctx, method, g.Config.URL, bodyReader)
	if err != nil {
		return Usage{}, err
	}
	scheme := g.Config.AuthScheme
	if scheme == "" {
		scheme = "Bearer"
	}
	req.Header.Set("Authorization", scheme+" "+tok)
	req.Header.Set("User-Agent", "claudeops/0.1")
	req.Header.Set("Accept", "application/json")
	if g.Config.Body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	client := g.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return Usage{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Usage{}, fmt.Errorf("%s endpoint returned HTTP %d", g.Name(), resp.StatusCode)
	}

	var root any
	if err := json.NewDecoder(resp.Body).Decode(&root); err != nil {
		return Usage{}, err
	}
	return g.toUsage(root), nil
}

func (g *Generic) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

func (g *Generic) toUsage(root any) Usage {
	windows := make([]Window, 0, len(g.Config.Windows))
	for _, spec := range g.Config.Windows {
		util, ok := utilizationFor(root, spec)
		if !ok {
			continue
		}
		var resets time.Time
		if spec.ResetPath != "" {
			if s, ok := dotGetString(root, spec.ResetPath); ok {
				if t, err := time.Parse(time.RFC3339, s); err == nil {
					resets = t
				}
			}
		}
		label := spec.Label
		if label == "" {
			label = "quota"
		}
		windows = append(windows, Window{Label: label, Utilization: util, ResetsAt: resets})
	}
	return Usage{Provider: g.Name(), Windows: windows, Note: g.Config.Note, FetchedAt: g.now()}
}

// utilizationFor computes a window's utilization (0..100) from whichever of the
// mapping strategies is configured, in priority order.
func utilizationFor(root any, spec GenericWindowSpec) (float64, bool) {
	if spec.UtilPath != "" {
		if v, ok := dotGetFloat(root, spec.UtilPath); ok {
			return clampPercent(v), true
		}
	}
	if spec.RemainPath != "" {
		if v, ok := dotGetFloat(root, spec.RemainPath); ok {
			return clampPercent((1 - v) * 100), true
		}
	}
	if spec.UsedPath != "" && spec.LimitPath != "" {
		used, ok1 := dotGetFloat(root, spec.UsedPath)
		limit, ok2 := dotGetFloat(root, spec.LimitPath)
		if ok1 && ok2 && limit > 0 {
			return clampPercent(used / limit * 100), true
		}
	}
	return 0, false
}

func clampPercent(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// dotGet walks a decoded JSON tree by a dot path. Numeric segments index into
// arrays (e.g. "data.0.balance").
func dotGet(root any, path string) (any, bool) {
	cur := root
	for _, seg := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[seg]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(node) {
				return nil, false
			}
			cur = node[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

func dotGetFloat(root any, path string) (float64, bool) {
	v, ok := dotGet(root, path)
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func dotGetString(root any, path string) (string, bool) {
	v, ok := dotGet(root, path)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// expandHome expands a leading ~ to the user home directory.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

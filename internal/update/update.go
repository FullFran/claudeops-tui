package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const InstallTarget = "github.com/fullfran/claudeops-tui/cmd/claudeops@latest"

var ErrManual = errors.New("manual update required")

type Env struct {
	GOBIN  string
	GOPATH string
}

type Runner interface {
	Executable() (string, error)
	LookPath(file string) (string, error)
	GoEnv(ctx context.Context) (Env, error)
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type OSRunner struct{}

func (OSRunner) Executable() (string, error) {
	return os.Executable()
}

func (OSRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

func (OSRunner) GoEnv(ctx context.Context) (Env, error) {
	cmd := exec.CommandContext(ctx, "go", "env", "-json", "GOBIN", "GOPATH")
	out, err := cmd.Output()
	if err != nil {
		return Env{}, err
	}
	return parseGoEnvJSON(out)
}

func (OSRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type Decision struct {
	CanAuto        bool
	Reason         string
	InstallCommand string
	CurrentVersion string
	ExecutablePath string
	ExpectedPath   string
	InstalledNow   string
}

type Updater struct {
	Runner  Runner
	Version string
	Target  string
	Binary  string
}

func New(version string) Updater {
	return Updater{
		Runner:  OSRunner{},
		Version: version,
		Target:  InstallTarget,
		Binary:  "claudeops",
	}
}

func (u Updater) Decide(ctx context.Context) (Decision, error) {
	u = u.withDefaults()
	decision := Decision{
		CurrentVersion: u.Version,
		InstallCommand: "go install " + u.Target,
	}

	execPath, err := u.Runner.Executable()
	if err == nil {
		decision.ExecutablePath = execPath
	}

	if _, err := u.Runner.LookPath("go"); err != nil {
		decision.Reason = "Go is not available on PATH"
		return decision, nil
	}

	env, err := u.Runner.GoEnv(ctx)
	if err != nil {
		decision.Reason = "Could not determine Go install directories"
		return decision, nil
	}

	expectedPath := expectedBinaryPath(env, u.Binary)
	if expectedPath == "" {
		decision.Reason = "Could not determine target install path for `go install`"
		return decision, nil
	}
	decision.ExpectedPath = expectedPath

	if decision.ExecutablePath == "" {
		decision.Reason = "Could not determine current executable path"
		return decision, nil
	}

	if filepath.Clean(decision.ExecutablePath) != filepath.Clean(expectedPath) {
		decision.Reason = fmt.Sprintf("current executable is %s, but `go install` would write %s", decision.ExecutablePath, expectedPath)
		return decision, nil
	}

	decision.CanAuto = true
	return decision, nil
}

func (u Updater) Update(ctx context.Context) (Decision, error) {
	u = u.withDefaults()
	decision, err := u.Decide(ctx)
	if err != nil {
		return decision, err
	}
	if !decision.CanAuto {
		return decision, fmt.Errorf("%w: %s", ErrManual, decision.Reason)
	}

	output, err := u.Runner.Run(ctx, "go", "install", u.Target)
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return decision, fmt.Errorf("automatic update failed: %w\nmanual update: %s", err, decision.InstallCommand)
		}
		return decision, fmt.Errorf("automatic update failed: %w\nmanual update: %s\noutput:\n%s", err, decision.InstallCommand, trimmed)
	}

	versionOut, err := u.Runner.Run(ctx, decision.ExpectedPath, "version")
	if err == nil {
		decision.InstalledNow = strings.TrimSpace(string(versionOut))
	}

	return decision, nil
}

func expectedBinaryPath(env Env, binary string) string {
	if env.GOBIN != "" {
		return filepath.Join(env.GOBIN, binary)
	}
	if env.GOPATH != "" {
		return filepath.Join(env.GOPATH, "bin", binary)
	}
	return ""
}

func parseGoEnvJSON(out []byte) (Env, error) {
	var env Env
	if len(out) == 0 {
		return env, nil
	}
	if err := json.Unmarshal(out, &env); err != nil {
		return Env{}, fmt.Errorf("parse go env: %w", err)
	}
	env.GOBIN = strings.TrimSpace(env.GOBIN)
	env.GOPATH = strings.TrimSpace(env.GOPATH)
	return env, nil
}

func (u Updater) withDefaults() Updater {
	if u.Runner == nil {
		u.Runner = OSRunner{}
	}
	if u.Target == "" {
		u.Target = InstallTarget
	}
	if u.Binary == "" {
		u.Binary = "claudeops"
	}
	return u
}

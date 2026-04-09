// Package update implements a conservative self-update flow for claudeops.
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

var ErrManual = errors.New("automatic update is not available for this installation")

type Env struct {
	GOBIN  string
	GOPATH string
}

type Decision struct {
	CanAuto        bool
	Reason         string
	ExecutablePath string
	ExpectedPath   string
	InstallCommand string
	CurrentVersion string
	InstalledCheck string
	InstalledNow   string
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

func (r OSRunner) GoEnv(ctx context.Context) (Env, error) {
	out, err := r.Run(ctx, "go", "env", "-json", "GOBIN", "GOPATH")
	if err != nil {
		return Env{}, err
	}
	var env Env
	if err := json.Unmarshal(out, &env); err != nil {
		return Env{}, fmt.Errorf("parse go env: %w", err)
	}
	return env, nil
}

func (OSRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
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
	decision := Decision{
		InstallCommand: fmt.Sprintf("go install %s", u.Target),
		CurrentVersion: u.Version,
	}

	execPath, err := u.Runner.Executable()
	if err != nil {
		decision.Reason = fmt.Sprintf("could not locate the running executable: %v", err)
		return decision, nil
	}
	decision.ExecutablePath = execPath

	if filepath.Base(execPath) != u.Binary {
		decision.Reason = fmt.Sprintf("current executable name is %q, expected %q", filepath.Base(execPath), u.Binary)
		return decision, nil
	}

	if _, err := u.Runner.LookPath("go"); err != nil {
		decision.Reason = "Go is not available on PATH"
		return decision, nil
	}

	goEnv, err := u.Runner.GoEnv(ctx)
	if err != nil {
		decision.Reason = fmt.Sprintf("could not inspect Go environment: %v", err)
		return decision, nil
	}

	binDir := strings.TrimSpace(goEnv.GOBIN)
	if binDir == "" {
		gopath := strings.TrimSpace(goEnv.GOPATH)
		if gopath == "" {
			decision.Reason = "Go did not report GOBIN or GOPATH"
			return decision, nil
		}
		binDir = filepath.Join(gopath, "bin")
	}

	decision.ExpectedPath = filepath.Join(binDir, u.Binary)
	if filepath.Clean(execPath) != filepath.Clean(decision.ExpectedPath) {
		decision.Reason = fmt.Sprintf("current executable is %q, not the Go install target %q", execPath, decision.ExpectedPath)
		return decision, nil
	}

	decision.CanAuto = true
	decision.InstalledCheck = decision.ExpectedPath
	return decision, nil
}

func (u Updater) Update(ctx context.Context) (Decision, error) {
	decision, err := u.Decide(ctx)
	if err != nil {
		return decision, err
	}
	if !decision.CanAuto {
		return decision, fmt.Errorf("%w: %s", ErrManual, decision.Reason)
	}

	out, err := u.Runner.Run(ctx, "go", "install", u.Target)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return decision, fmt.Errorf("update failed: %w", err)
		}
		return decision, fmt.Errorf("update failed: %w\n%s", err, msg)
	}

	if versionOut, err := u.Runner.Run(ctx, decision.InstalledCheck, "version"); err == nil {
		decision.InstalledNow = strings.TrimSpace(string(versionOut))
	}

	return decision, nil
}

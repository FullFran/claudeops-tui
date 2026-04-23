package update

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakeRunner struct {
	execPath      string
	execErr       error
	goPath        string
	goPathErr     error
	goEnv         Env
	goEnvErr      error
	symlinkMap    map[string]string // path -> resolved path
	symlinkErr    error
	runResults    map[string]fakeRunResult
	runCalls      []fakeRunCall
	lookPathCalls []string
}

type fakeRunCall struct {
	name string
	args []string
}

type fakeRunResult struct {
	out []byte
	err error
}

func (f *fakeRunner) Executable() (string, error) {
	return f.execPath, f.execErr
}

func (f *fakeRunner) EvalSymlinks(path string) (string, error) {
	if f.symlinkErr != nil {
		return "", f.symlinkErr
	}
	if f.symlinkMap != nil {
		if resolved, ok := f.symlinkMap[path]; ok {
			return resolved, nil
		}
	}
	return path, nil
}

func (f *fakeRunner) LookPath(file string) (string, error) {
	f.lookPathCalls = append(f.lookPathCalls, file)
	if f.goPathErr != nil {
		return "", f.goPathErr
	}
	return f.goPath, nil
}

func (f *fakeRunner) GoEnv(context.Context) (Env, error) {
	return f.goEnv, f.goEnvErr
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := fakeRunCall{name: name, args: append([]string(nil), args...)}
	f.runCalls = append(f.runCalls, call)
	key := runKey(name, args...)
	result, ok := f.runResults[key]
	if !ok {
		return nil, errors.New("unexpected command: " + key)
	}
	return result.out, result.err
}

func runKey(name string, args ...string) string {
	key := name
	for _, arg := range args {
		key += "\x00" + arg
	}
	return key
}

func TestDecideAutoWhenExecutableMatchesGoBin(t *testing.T) {
	runner := &fakeRunner{
		execPath: "/tmp/go/bin/claudeops",
		goPath:   "/usr/bin/go",
		goEnv: Env{
			GOBIN: "/tmp/go/bin",
		},
	}

	updater := Updater{
		Runner:  runner,
		Version: "0.1.0",
		Target:  InstallTarget,
		Binary:  "claudeops",
	}

	decision, err := updater.Decide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !decision.CanAuto {
		t.Fatalf("expected automatic update, got manual: %s", decision.Reason)
	}
	if decision.ExpectedPath != "/tmp/go/bin/claudeops" {
		t.Fatalf("unexpected expected path: %s", decision.ExpectedPath)
	}
	if decision.InstallCommand != "go install "+InstallTarget {
		t.Fatalf("unexpected install command: %s", decision.InstallCommand)
	}
	if !reflect.DeepEqual(runner.lookPathCalls, []string{"go"}) {
		t.Fatalf("unexpected LookPath calls: %#v", runner.lookPathCalls)
	}
}

func TestDecideAutoWhenExecutableMatchesGopathBin(t *testing.T) {
	runner := &fakeRunner{
		execPath: "/tmp/go/bin/claudeops",
		goPath:   "/usr/bin/go",
		goEnv: Env{
			GOPATH: "/tmp/go",
		},
	}

	decision, err := New("0.1.0").withRunner(runner).Decide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !decision.CanAuto {
		t.Fatalf("expected automatic update, got manual: %s", decision.Reason)
	}
	if decision.ExpectedPath != "/tmp/go/bin/claudeops" {
		t.Fatalf("unexpected expected path: %s", decision.ExpectedPath)
	}
}

func TestDecideManualWhenExecutableIsOutsideGoBin(t *testing.T) {
	runner := &fakeRunner{
		execPath: "/usr/local/bin/claudeops",
		goPath:   "/usr/bin/go",
		goEnv: Env{
			GOPATH: "/tmp/go",
		},
	}

	decision, err := New("0.1.0").withRunner(runner).Decide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if decision.CanAuto {
		t.Fatal("expected manual update decision")
	}
	if decision.Reason == "" {
		t.Fatal("expected manual reason")
	}
}

func TestDecideManualWhenGoMissing(t *testing.T) {
	runner := &fakeRunner{
		execPath:  "/tmp/go/bin/claudeops",
		goPathErr: errors.New("missing"),
	}

	decision, err := New("0.1.0").withRunner(runner).Decide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if decision.CanAuto {
		t.Fatal("expected manual update decision")
	}
	if decision.Reason != "Go is not available on PATH" {
		t.Fatalf("unexpected reason: %s", decision.Reason)
	}
}

func TestUpdateRunsInstallAndVerifiesInstalledVersion(t *testing.T) {
	runner := &fakeRunner{
		execPath: "/tmp/go/bin/claudeops",
		goPath:   "/usr/bin/go",
		goEnv: Env{
			GOBIN: "/tmp/go/bin",
		},
		runResults: map[string]fakeRunResult{
			runKey("go", "install", InstallTarget):     {out: []byte("ok")},
			runKey("/tmp/go/bin/claudeops", "version"): {out: []byte("claudeops 0.2.0\n")},
		},
	}

	decision, err := New("0.1.0").withRunner(runner).Update(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if decision.InstalledNow != "claudeops 0.2.0" {
		t.Fatalf("unexpected installed version: %q", decision.InstalledNow)
	}
	if len(runner.runCalls) != 2 {
		t.Fatalf("expected 2 run calls, got %d", len(runner.runCalls))
	}
	if runner.runCalls[0].name != "go" || !reflect.DeepEqual(runner.runCalls[0].args, []string{"install", InstallTarget}) {
		t.Fatalf("unexpected install call: %#v", runner.runCalls[0])
	}
}

func TestUpdateReturnsManualErrorWhenUnsafe(t *testing.T) {
	runner := &fakeRunner{
		execPath: "/opt/claudeops/claudeops",
		goPath:   "/usr/bin/go",
		goEnv: Env{
			GOBIN: "/tmp/go/bin",
		},
	}

	_, err := New("0.1.0").withRunner(runner).Update(context.Background())
	if !errors.Is(err, ErrManual) {
		t.Fatalf("expected ErrManual, got %v", err)
	}
}

func TestParseGoEnvJSONHandlesEmptyGobin(t *testing.T) {
	out := []byte("{\n\t\"GOBIN\": \"\",\n\t\"GOPATH\": \"/home/franblakia/go\"\n}\n")
	env, err := parseGoEnvJSON(out)
	if err != nil {
		t.Fatal(err)
	}
	if env.GOBIN != "" {
		t.Fatalf("expected empty GOBIN, got %q", env.GOBIN)
	}
	if env.GOPATH != "/home/franblakia/go" {
		t.Fatalf("unexpected GOPATH: %q", env.GOPATH)
	}
	if got := expectedBinaryPath(env, "claudeops"); got != "/home/franblakia/go/bin/claudeops" {
		t.Fatalf("unexpected expected path: %s", got)
	}
}

func TestParseGoEnvJSONRejectsInvalidJSON(t *testing.T) {
	_, err := parseGoEnvJSON(bytes.TrimSpace([]byte("not-json")))
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestDecideManualWhenGoEnvFails(t *testing.T) {
	runner := &fakeRunner{
		execPath: "/tmp/go/bin/claudeops",
		goPath:   "/usr/bin/go",
		goEnvErr: errors.New("go env failed"),
	}

	decision, err := New("0.1.0").withRunner(runner).Decide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if decision.CanAuto {
		t.Fatal("expected manual update decision")
	}
	if decision.Reason != "Could not determine Go install directories" {
		t.Fatalf("unexpected reason: %s", decision.Reason)
	}
}

func TestDecideManualWhenExecutableFails(t *testing.T) {
	runner := &fakeRunner{
		execErr: errors.New("cannot resolve executable"),
		goPath:  "/usr/bin/go",
		goEnv:   Env{GOBIN: "/tmp/go/bin"},
	}

	decision, err := New("0.1.0").withRunner(runner).Decide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if decision.CanAuto {
		t.Fatal("expected manual update decision")
	}
	if decision.Reason != "Could not determine current executable path" {
		t.Fatalf("unexpected reason: %s", decision.Reason)
	}
}

func TestDecideManualWhenBothGobinAndGopathEmpty(t *testing.T) {
	runner := &fakeRunner{
		execPath: "/tmp/go/bin/claudeops",
		goPath:   "/usr/bin/go",
		goEnv:    Env{},
	}

	decision, err := New("0.1.0").withRunner(runner).Decide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if decision.CanAuto {
		t.Fatal("expected manual update decision")
	}
	if decision.Reason != "Could not determine target install path for `go install`" {
		t.Fatalf("unexpected reason: %s", decision.Reason)
	}
}

func TestUpdateFailsWithOutput(t *testing.T) {
	runner := &fakeRunner{
		execPath: "/tmp/go/bin/claudeops",
		goPath:   "/usr/bin/go",
		goEnv:    Env{GOBIN: "/tmp/go/bin"},
		runResults: map[string]fakeRunResult{
			runKey("go", "install", InstallTarget): {
				out: []byte("go: module not found"),
				err: errors.New("exit status 1"),
			},
		},
	}

	_, err := New("0.1.0").withRunner(runner).Update(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "go: module not found") {
		t.Fatalf("expected output in error, got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "automatic update failed") {
		t.Fatalf("expected 'automatic update failed' in error, got: %s", err.Error())
	}
}

func TestUpdateFailsWithoutOutput(t *testing.T) {
	runner := &fakeRunner{
		execPath: "/tmp/go/bin/claudeops",
		goPath:   "/usr/bin/go",
		goEnv:    Env{GOBIN: "/tmp/go/bin"},
		runResults: map[string]fakeRunResult{
			runKey("go", "install", InstallTarget): {
				out: []byte(""),
				err: errors.New("exit status 1"),
			},
		},
	}

	_, err := New("0.1.0").withRunner(runner).Update(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "output:") {
		t.Fatalf("expected no output section in error, got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "automatic update failed") {
		t.Fatalf("expected 'automatic update failed' in error, got: %s", err.Error())
	}
}

func TestUpdateSucceedsWhenVersionCheckFails(t *testing.T) {
	runner := &fakeRunner{
		execPath: "/tmp/go/bin/claudeops",
		goPath:   "/usr/bin/go",
		goEnv:    Env{GOBIN: "/tmp/go/bin"},
		runResults: map[string]fakeRunResult{
			runKey("go", "install", InstallTarget):     {out: []byte("ok")},
			runKey("/tmp/go/bin/claudeops", "version"): {err: errors.New("exec failed")},
		},
	}

	decision, err := New("0.1.0").withRunner(runner).Update(context.Background())
	if err != nil {
		t.Fatalf("update should succeed even if version check fails: %v", err)
	}
	if decision.InstalledNow != "" {
		t.Fatalf("expected empty InstalledNow, got %q", decision.InstalledNow)
	}
}

func TestDecideAutoWhenExecutableIsSymlinkToGoBin(t *testing.T) {
	runner := &fakeRunner{
		execPath: "/usr/local/bin/claudeops",
		goPath:   "/usr/bin/go",
		goEnv:    Env{GOBIN: "/home/user/go/bin"},
		symlinkMap: map[string]string{
			"/usr/local/bin/claudeops":    "/home/user/go/bin/claudeops",
			"/home/user/go/bin/claudeops": "/home/user/go/bin/claudeops",
		},
	}

	decision, err := New("0.1.0").withRunner(runner).Decide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !decision.CanAuto {
		t.Fatalf("expected automatic update (symlink resolves to GOBIN), got manual: %s", decision.Reason)
	}
}

func TestDecideManualWhenSymlinkResolvesToDifferentPath(t *testing.T) {
	runner := &fakeRunner{
		execPath: "/usr/local/bin/claudeops",
		goPath:   "/usr/bin/go",
		goEnv:    Env{GOBIN: "/home/user/go/bin"},
		symlinkMap: map[string]string{
			"/usr/local/bin/claudeops":    "/opt/custom/claudeops",
			"/home/user/go/bin/claudeops": "/home/user/go/bin/claudeops",
		},
	}

	decision, err := New("0.1.0").withRunner(runner).Decide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if decision.CanAuto {
		t.Fatal("expected manual update (symlink resolves to different path)")
	}
}

func TestDecideFallsBackToCleanWhenEvalSymlinksFails(t *testing.T) {
	runner := &fakeRunner{
		execPath:   "/tmp/go/bin/claudeops",
		goPath:     "/usr/bin/go",
		goEnv:      Env{GOBIN: "/tmp/go/bin"},
		symlinkErr: errors.New("permission denied"),
	}

	decision, err := New("0.1.0").withRunner(runner).Decide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !decision.CanAuto {
		t.Fatalf("expected auto update (paths match after Clean fallback), got manual: %s", decision.Reason)
	}
}

func TestUpdateRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runner := &fakeRunner{
		execPath: "/tmp/go/bin/claudeops",
		goPath:   "/usr/bin/go",
		goEnv:    Env{GOBIN: "/tmp/go/bin"},
		runResults: map[string]fakeRunResult{
			runKey("go", "install", InstallTarget): {
				err: ctx.Err(),
			},
		},
	}

	_, err := New("0.1.0").withRunner(runner).Update(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("expected context cancellation error, got: %s", err.Error())
	}
}

func TestParseGoEnvJSONHandlesEmptyInput(t *testing.T) {
	env, err := parseGoEnvJSON([]byte{})
	if err != nil {
		t.Fatal(err)
	}
	if env.GOBIN != "" || env.GOPATH != "" {
		t.Fatalf("expected zero Env, got %+v", env)
	}
}

func (u Updater) withRunner(r Runner) Updater {
	u.Runner = r
	return u
}

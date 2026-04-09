package update

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fakeRunner struct {
	execPath      string
	execErr       error
	goPath        string
	goPathErr     error
	goEnv         Env
	goEnvErr      error
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

func (u Updater) withRunner(r Runner) Updater {
	u.Runner = r
	return u
}

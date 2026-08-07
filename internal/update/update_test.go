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

// The binary a user downloaded to /usr/local/bin is not the one `go install`
// manages. It used to be told to run `go install`, which would have written a
// second copy into GOBIN and left this one at the old version. It now updates
// itself in place.
func TestDecideUsesBinaryReplacementOutsideGoBin(t *testing.T) {
	runner := &fakeRunner{
		execPath: "/usr/local/bin/claudeops",
		goPath:   "/usr/bin/go",
		goEnv: Env{
			GOPATH: "/tmp/go",
		},
	}

	decision, err := New("0.1.0").withRunner(runner).withWritable(nil).Decide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !decision.CanAuto {
		t.Fatalf("expected an automatic update, got manual: %s", decision.Reason)
	}
	if decision.Method != MethodBinary {
		t.Fatalf("method = %q, want %q", decision.Method, MethodBinary)
	}
}

// An install directory owned by root is the normal case for /usr/local/bin.
// It must be reported as needing manual action — never by escalating on the
// user's behalf.
func TestDecideManualWhenInstallDirIsNotWritable(t *testing.T) {
	runner := &fakeRunner{
		execPath: "/usr/local/bin/claudeops",
		goPath:   "/usr/bin/go",
		goEnv:    Env{GOPATH: "/tmp/go"},
	}

	decision, err := New("0.1.0").
		withRunner(runner).
		withWritable(errors.New("permission denied")).
		Decide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if decision.CanAuto {
		t.Fatal("expected manual update decision")
	}
	if decision.Method != MethodBinary {
		t.Fatalf("method = %q, want %q", decision.Method, MethodBinary)
	}
	if !strings.Contains(decision.Reason, "sudo") {
		t.Fatalf("reason should tell the user what to do, got: %s", decision.Reason)
	}
}

// No Go toolchain is exactly the situation of someone who installed a release
// archive. It is no longer a dead end.
func TestDecideFallsBackToBinaryWhenGoMissing(t *testing.T) {
	runner := &fakeRunner{
		execPath:  "/opt/claudeops/claudeops",
		goPathErr: errors.New("missing"),
	}

	decision, err := New("0.1.0").withRunner(runner).withWritable(nil).Decide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !decision.CanAuto {
		t.Fatalf("expected an automatic update without Go, got manual: %s", decision.Reason)
	}
	if decision.Method != MethodBinary {
		t.Fatalf("method = %q, want %q", decision.Method, MethodBinary)
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

// Since v0.14.0 `claudeops version` prints commit and build date under the
// version line. Only the first line is the contract; parsing the whole output
// would read the build date as the version and wrongly report a stale proxy.
func TestUpdateReadsOnlyTheFirstLineOfVersionOutput(t *testing.T) {
	runner := &fakeRunner{
		execPath: "/tmp/go/bin/claudeops",
		goPath:   "/usr/bin/go",
		goEnv: Env{
			GOBIN: "/tmp/go/bin",
		},
		runResults: map[string]fakeRunResult{
			runKey("go", "install", InstallTarget): {out: []byte("ok")},
			runKey("/tmp/go/bin/claudeops", "version"): {
				out: []byte("claudeops 0.2.0\ncommit: abc1234\nbuilt: 2026-08-06T11:00:00Z\n"),
			},
		},
	}

	decision, err := New("0.1.0").withRunner(runner).Update(context.Background())
	if err != nil {
		t.Fatalf("multi-line version output must not fail the staleness check: %v", err)
	}
	if decision.InstalledNow != "claudeops 0.2.0" {
		t.Fatalf("unexpected installed version: %q", decision.InstalledNow)
	}
}

func TestExtractSemver(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain version line", in: "claudeops 0.2.0", want: "0.2.0"},
		{name: "trailing metadata is ignored", in: "claudeops 0.2.0 (dirty)", want: "0.2.0"},
		{name: "empty", in: "", want: ""},
		{name: "single field", in: "claudeops", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractSemver(tt.in); got != tt.want {
				t.Fatalf("extractSemver(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
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

// A broken `go env` says nothing about whether this binary can be replaced,
// so it falls through to the path that does not need Go.
func TestDecideFallsBackToBinaryWhenGoEnvFails(t *testing.T) {
	runner := &fakeRunner{
		execPath: "/tmp/go/bin/claudeops",
		goPath:   "/usr/bin/go",
		goEnvErr: errors.New("go env failed"),
	}

	decision, err := New("0.1.0").withRunner(runner).withWritable(nil).Decide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !decision.CanAuto {
		t.Fatalf("expected an automatic update, got manual: %s", decision.Reason)
	}
	if decision.Method != MethodBinary {
		t.Fatalf("method = %q, want %q", decision.Method, MethodBinary)
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

func TestDecideFallsBackToBinaryWhenBothGobinAndGopathEmpty(t *testing.T) {
	runner := &fakeRunner{
		execPath: "/tmp/go/bin/claudeops",
		goPath:   "/usr/bin/go",
		goEnv:    Env{},
	}

	decision, err := New("0.1.0").withRunner(runner).withWritable(nil).Decide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !decision.CanAuto {
		t.Fatalf("expected an automatic update, got manual: %s", decision.Reason)
	}
	if decision.Method != MethodBinary {
		t.Fatalf("method = %q, want %q", decision.Method, MethodBinary)
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

// A binary on PATH that symlinks somewhere outside GOBIN is not something
// `go install` can update — it would write a second copy into GOBIN and leave
// this one running. Replacing the file is the only update that reaches the user.
func TestDecideUsesBinaryReplacementWhenSymlinkResolvesElsewhere(t *testing.T) {
	runner := &fakeRunner{
		execPath: "/usr/local/bin/claudeops",
		goPath:   "/usr/bin/go",
		goEnv:    Env{GOBIN: "/home/user/go/bin"},
		symlinkMap: map[string]string{
			"/usr/local/bin/claudeops":    "/opt/custom/claudeops",
			"/home/user/go/bin/claudeops": "/home/user/go/bin/claudeops",
		},
	}

	decision, err := New("0.1.0").withRunner(runner).withWritable(nil).Decide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if decision.Method != MethodBinary {
		t.Fatalf("method = %q, want %q", decision.Method, MethodBinary)
	}
	if !decision.CanAuto {
		t.Fatalf("expected an automatic binary update, got manual: %s", decision.Reason)
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

// withWritable pins the writability answer so a decision does not depend on
// whatever the test machine happens to have at /tmp/go/bin.
func (u Updater) withWritable(err error) Updater {
	u.IsWritable = func(string) error { return err }
	return u
}

// withFetcher pins BOTH version sources. Decide picks between them by method,
// so setting only one leaves the other reaching the real network — which is
// how these tests silently started querying GitHub.
func (u Updater) withFetcher(f LatestFetcher) Updater {
	u.Fetcher = f
	u.ReleaseFetcher = f
	return u
}

type fakeInstaller struct {
	version  string
	execPath string
	calls    int
	err      error
}

func (f *fakeInstaller) Install(_ context.Context, version, execPath string) error {
	f.calls++
	f.version = version
	f.execPath = execPath
	return f.err
}

// The whole point of the binary method: it must replace the file the user
// actually runs, at the version that was published.
func TestUpdateReplacesTheBinaryInPlace(t *testing.T) {
	runner := &fakeRunner{
		execPath:  "/usr/local/bin/claudeops",
		goPathErr: errors.New("no go"),
		runResults: map[string]fakeRunResult{
			runKey("/usr/local/bin/claudeops", "version"): {out: []byte("claudeops 0.2.0\ncommit: abc1234\n")},
		},
	}
	installer := &fakeInstaller{}

	u := New("0.1.0").withRunner(runner).withWritable(nil)
	u.Installer = installer
	u = u.withFetcher(staticFetcher{version: "v0.2.0"})

	decision, err := u.Update(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if installer.calls != 1 {
		t.Fatalf("installer called %d times, want 1", installer.calls)
	}
	if installer.version != "0.2.0" {
		t.Fatalf("installed version = %q, want 0.2.0", installer.version)
	}
	if installer.execPath != "/usr/local/bin/claudeops" {
		t.Fatalf("installed over %q, want /usr/local/bin/claudeops", installer.execPath)
	}
	// The version must be read back from the binary that was written, not
	// assumed from the tag it was published under.
	if decision.InstalledNow != "claudeops 0.2.0" {
		t.Fatalf("InstalledNow = %q", decision.InstalledNow)
	}
	if decision.InstalledPath != "/usr/local/bin/claudeops" {
		t.Fatalf("InstalledPath = %q", decision.InstalledPath)
	}
}

// A release whose archive carries a different version than its tag is a broken
// release. Reporting the tag would hide that behind a success message, and the
// next update would download the same archive again.
func TestUpdateBinaryReportsAVersionThatDisagreesWithItsTag(t *testing.T) {
	runner := &fakeRunner{
		execPath:  "/usr/local/bin/claudeops",
		goPathErr: errors.New("no go"),
		runResults: map[string]fakeRunResult{
			runKey("/usr/local/bin/claudeops", "version"): {out: []byte("claudeops 0.1.9\n")},
		},
	}

	u := New("0.1.0").withRunner(runner).withWritable(nil)
	u.Installer = &fakeInstaller{}
	u = u.withFetcher(staticFetcher{version: "v0.2.0"})

	_, err := u.Update(context.Background())
	if err == nil {
		t.Fatal("expected an error when the installed version disagrees with the tag")
	}
	if !strings.Contains(err.Error(), "0.1.9") || !strings.Contains(err.Error(), "0.2.0") {
		t.Fatalf("error should name both versions, got: %v", err)
	}
}

// A binary that cannot be executed to confirm its version is not a failure —
// the swap already succeeded. The CLI reports that it could not verify.
func TestUpdateBinarySucceedsWhenTheNewBinaryCannotBeRun(t *testing.T) {
	runner := &fakeRunner{
		execPath:  "/usr/local/bin/claudeops",
		goPathErr: errors.New("no go"),
	}

	u := New("0.1.0").withRunner(runner).withWritable(nil)
	u.Installer = &fakeInstaller{}
	u = u.withFetcher(staticFetcher{version: "v0.2.0"})

	decision, err := u.Update(context.Background())
	if err != nil {
		t.Fatalf("an unverifiable version must not fail the update: %v", err)
	}
	if decision.InstalledNow != "" {
		t.Fatalf("InstalledNow = %q, want empty so the CLI says it could not verify", decision.InstalledNow)
	}
}

// Without a resolved version there is no asset name to request. Guessing would
// mean downloading something arbitrary over the user's binary.
func TestUpdateBinaryRefusesWithoutAKnownVersion(t *testing.T) {
	runner := &fakeRunner{
		execPath:  "/usr/local/bin/claudeops",
		goPathErr: errors.New("no go"),
	}
	installer := &fakeInstaller{}

	u := New("0.1.0").withRunner(runner).withWritable(nil)
	u.Installer = installer
	u = u.withFetcher(failingFetcher{})

	_, err := u.Update(context.Background())
	if !errors.Is(err, ErrManual) {
		t.Fatalf("error = %v, want ErrManual", err)
	}
	if installer.calls != 0 {
		t.Fatalf("installer must not run without a version, called %d times", installer.calls)
	}
}

// A failed download leaves the user where they were, with somewhere to go.
func TestUpdateBinaryReportsInstallFailure(t *testing.T) {
	runner := &fakeRunner{
		execPath:  "/usr/local/bin/claudeops",
		goPathErr: errors.New("no go"),
	}
	installer := &fakeInstaller{err: ErrChecksumMismatch}

	u := New("0.1.0").withRunner(runner).withWritable(nil)
	u.Installer = installer
	u = u.withFetcher(staticFetcher{version: "v0.2.0"})

	_, err := u.Update(context.Background())
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("error = %v, want ErrChecksumMismatch", err)
	}
	if !strings.Contains(err.Error(), releasesPage) {
		t.Fatalf("error should point at the releases page, got: %v", err)
	}
}

type staticFetcher struct{ version string }

func (f staticFetcher) Latest(context.Context) (Release, error) {
	return Release{Version: f.version}, nil
}

type failingFetcher struct{}

func (failingFetcher) Latest(context.Context) (Release, error) {
	return Release{}, errors.New("proxy unreachable")
}

func TestSemverLT(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{"lower patch", "0.7.0", "0.7.1", true},
		{"lower minor", "0.6.9", "0.7.0", true},
		{"lower major", "0.9.9", "1.0.0", true},
		{"equal is not less", "0.7.0", "0.7.0", false},
		{"higher patch", "0.7.1", "0.7.0", false},
		{"v prefix tolerated", "v0.7.0", "v0.7.1", true},
		{"prerelease keeps base precedence", "0.7.0-rc1", "0.7.1", true},
		// A release candidate precedes the release it is a candidate for.
		// Treating them as equal strands every RC tester: the GA release
		// compares as "not newer", so the update is refused as a downgrade.
		{"rc precedes its own release", "0.14.0-rc.1", "0.14.0", true},
		{"release does not precede its rc", "0.14.0", "0.14.0-rc.1", false},
		{"rc does not precede an earlier release", "0.14.0-rc.1", "0.13.9", false},
		{"two prereleases compare as equal bases", "0.14.0-rc.1", "0.14.0-rc.2", false},
		// A version string that cannot be parsed must never be reported as
		// older: Sscanf leaves 0.0.0 behind, which would make any real
		// version look newer and fabricate a stale-proxy failure.
		{"unparseable a is not older", "dev", "0.7.0", false},
		{"unparseable b is not newer", "0.7.0", "dev", false},
		{"both unparseable", "dev", "dev", false},
		{"truncated a is not older", "0.7", "0.7.0", false},
		{"empty a is not older", "", "0.7.0", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := semverLT(tt.a, tt.b); got != tt.want {
				t.Errorf("semverLT(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// A binary built from source is the developer's own work in progress. It looks
// outdated to Decide (buildinfo reports defaultVersion for it), so without a
// guard `claudeops update` would download a release archive and rename it over
// the very build being tested.
func TestDecideRefusesToOverwriteASourceBuild(t *testing.T) {
	runner := &fakeRunner{
		execPath: "/home/user/src/claudeops-tui/claudeops",
		goPath:   "/usr/bin/go",
		goEnv:    Env{GOBIN: "/home/user/go/bin"},
	}

	u := New("0.1.0").withRunner(runner).withWritable(nil)
	u.SourceBuild = true
	decision, err := u.Decide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if decision.CanAuto {
		t.Fatal("a source build must not be replaced automatically")
	}
	if !strings.Contains(decision.Reason, "built from source") {
		t.Fatalf("reason should say why, got: %q", decision.Reason)
	}
}

// The same install, not built from source, still updates.
func TestDecideUpdatesAReleaseBuildInThatSamePlace(t *testing.T) {
	runner := &fakeRunner{
		execPath: "/home/user/src/claudeops-tui/claudeops",
		goPath:   "/usr/bin/go",
		goEnv:    Env{GOBIN: "/home/user/go/bin"},
	}

	decision, err := New("0.1.0").withRunner(runner).withWritable(nil).Decide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !decision.CanAuto {
		t.Fatalf("expected an automatic update, got manual: %s", decision.Reason)
	}
}

// The proxy and GitHub Releases disagree routinely: a tag exists the moment it
// is pushed, its archives minutes later or never. The release index is the
// authority for both methods — `go install` runs with GOPROXY=direct, so it
// installs from tags too and is not limited to what the proxy has cached.
func TestDecidePrefersTheReleaseIndexForBothMethods(t *testing.T) {
	tests := []struct {
		name       string
		execPath   string
		wantMethod Method
	}{
		{name: "go install", execPath: "/tmp/go/bin/claudeops", wantMethod: MethodGoInstall},
		{name: "binary replacement", execPath: "/usr/local/bin/claudeops", wantMethod: MethodBinary},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{
				execPath: tt.execPath,
				goPath:   "/usr/bin/go",
				goEnv:    Env{GOBIN: "/tmp/go/bin"},
			}

			u := New("0.1.0").withRunner(runner).withWritable(nil)
			u.Fetcher = staticFetcher{version: "v0.13.1"}        // proxy lagging
			u.ReleaseFetcher = staticFetcher{version: "v0.14.1"} // actually published

			decision, err := u.Decide(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if decision.Method != tt.wantMethod {
				t.Fatalf("method = %q, want %q", decision.Method, tt.wantMethod)
			}
			if decision.LatestVersion != "0.14.1" {
				t.Fatalf("LatestVersion = %q, want 0.14.1", decision.LatestVersion)
			}
			if decision.UpToDate {
				t.Fatal("a newer release exists; this must not report up to date")
			}
		})
	}
}

// A machine that can reach the proxy but not GitHub must still learn that an
// update exists, rather than being told nothing at all.
func TestDecideFallsBackToTheProxyWhenTheReleaseIndexIsUnreachable(t *testing.T) {
	runner := &fakeRunner{
		execPath: "/tmp/go/bin/claudeops",
		goPath:   "/usr/bin/go",
		goEnv:    Env{GOBIN: "/tmp/go/bin"},
	}

	u := New("0.1.0").withRunner(runner).withWritable(nil)
	u.Fetcher = staticFetcher{version: "v0.13.1"}
	u.ReleaseFetcher = failingFetcher{}

	decision, err := u.Decide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if decision.LatestVersion != "0.13.1" {
		t.Fatalf("LatestVersion = %q, want the proxy's answer 0.13.1", decision.LatestVersion)
	}
}

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

// ErrStaleRelease reports that the published version is older than this build.
var ErrStaleRelease = errors.New("published release is older than the running build")

var ErrManual = errors.New("manual update required")

type Env struct {
	GOBIN  string
	GOPATH string
}

type Runner interface {
	Executable() (string, error)
	EvalSymlinks(path string) (string, error)
	LookPath(file string) (string, error)
	GoEnv(ctx context.Context) (Env, error)
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type OSRunner struct{}

func (OSRunner) Executable() (string, error) {
	return os.Executable()
}

func (OSRunner) EvalSymlinks(path string) (string, error) {
	return filepath.EvalSymlinks(path)
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
	cmd := exec.CommandContext(ctx, name, args...)
	// For `go install`, bypass the module proxy so we always get the latest
	// tag directly from VCS — the proxy can serve a stale "latest" for minutes.
	if name == "go" && len(args) > 0 && args[0] == "install" {
		cmd.Env = append(os.Environ(), "GOPROXY=direct,https://proxy.golang.org,direct")
	}
	return cmd.CombinedOutput()
}

// Method is how this installation updates itself.
type Method string

const (
	// MethodGoInstall re-runs `go install`, which is correct only when the
	// running binary is the one go install manages.
	MethodGoInstall Method = "go install"
	// MethodBinary replaces the running executable with the published release
	// archive. This is how anyone who downloaded a binary updates.
	MethodBinary Method = "binary"
)

type Decision struct {
	CanAuto        bool
	Reason         string
	InstallCommand string
	CurrentVersion string
	ExecutablePath string
	ExpectedPath   string
	InstalledNow   string

	// InstalledPath is the file a binary update actually wrote. It differs from
	// ExecutablePath when the executable is a symlink: the update follows it and
	// rewrites the target, so reporting ExecutablePath would name a file that
	// was not modified.
	InstalledPath string

	// Method is how an automatic update would be performed. It is meaningful
	// even when CanAuto is false: it says which path was chosen and therefore
	// what Reason is talking about.
	Method Method

	// LatestVersion is what the module proxy reports, without the leading "v".
	// Empty when the proxy could not be reached — that is not fatal, it just
	// means the install has to go ahead blind, as it always used to.
	LatestVersion string
	// UpToDate is true when the published version is not newer than this one.
	UpToDate bool
	// Downgrade is true when the published version is OLDER. Installing then
	// would replace a working binary with fewer features, which is exactly what
	// happens to anyone running a build made between releases.
	Downgrade bool
}

type Updater struct {
	Runner  Runner
	Version string
	Target  string
	Binary  string
	// Fetcher resolves the newest published version. Nil uses the public proxy.
	Fetcher LatestFetcher
	// Installer replaces the running binary from a published release archive.
	// Nil uses BinaryInstaller against the real GitHub release downloads.
	Installer BinaryUpdater
	// IsWritable reports whether the running binary can be replaced in place.
	// Nil probes the real filesystem.
	IsWritable func(execPath string) error

	// ReleaseFetcher resolves what MethodBinary can install. It asks GitHub
	// Releases, where the archives live, rather than the module proxy, which
	// only knows that a tag exists. Nil falls back to Fetcher.
	ReleaseFetcher LatestFetcher

	// SourceBuild marks a binary built from a working tree rather than
	// installed from a release. Such a binary is someone's work in progress,
	// and it always looks outdated — its version falls back to whatever the
	// source tree claims — so replacing it would quietly destroy the build
	// under test. Callers set this from buildinfo.FromSource().
	SourceBuild bool
}

// BinaryUpdater installs a published release over the running executable.
type BinaryUpdater interface {
	Install(ctx context.Context, version, execPath string) error
}

func New(version string) Updater {
	return Updater{
		Runner:         OSRunner{},
		Version:        version,
		Target:         InstallTarget,
		Binary:         "claudeops",
		Fetcher:        ProxyFetcher{},
		ReleaseFetcher: GitHubFetcher{},
	}
}

// resolveLatest fills in what is published, using the source the chosen method
// would actually install from.
//
// The two disagree routinely, and each is only authoritative for its own
// method: the proxy indexes git tags, which exist before their archives are
// built and for releases that never produced any, while GitHub Releases knows
// nothing about what `go install` can fetch. Asking the wrong one is how a
// binary install gets offered a version it can only 404 on.
//
// A source that cannot be reached is not an error. It leaves LatestVersion
// empty, and the caller decides what that means for its method.
func (u Updater) resolveLatest(ctx context.Context, decision *Decision) {
	fetcher := u.Fetcher
	if decision.Method == MethodBinary && u.ReleaseFetcher != nil {
		fetcher = u.ReleaseFetcher
	}
	if fetcher == nil {
		return
	}

	rel, err := fetcher.Latest(ctx)
	if err != nil {
		return
	}
	latest := strings.TrimPrefix(rel.Version, "v")
	decision.LatestVersion = latest
	switch {
	case semverLT(u.Version, latest):
		// newer is available; both flags stay false
	case latest == u.Version:
		decision.UpToDate = true
	default:
		decision.UpToDate = true
		decision.Downgrade = true
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
	if decision.ExecutablePath == "" {
		decision.Reason = "Could not determine current executable path"
		u.resolveLatest(ctx, &decision)
		return decision, nil
	}

	// `go install` is the right tool for exactly one installation: the binary it
	// would itself overwrite. Anywhere else it writes a second copy into GOBIN
	// and leaves this one untouched, so the update reports success and changes
	// nothing the user runs.
	if expected, ok := u.goInstallTarget(ctx); ok {
		decision.ExpectedPath = expected
		if resolveOrClean(u.Runner, decision.ExecutablePath) == resolveOrClean(u.Runner, expected) {
			decision.Method = MethodGoInstall
			u.resolveLatest(ctx, &decision)
			decision.CanAuto = true
			return decision, nil
		}
	}

	// Everything else — a downloaded release archive, a binary copied onto
	// PATH, a machine with no Go at all — updates by replacing this file with
	// the published release for its platform.
	decision.Method = MethodBinary
	decision.InstallCommand = "claudeops update"
	u.resolveLatest(ctx, &decision)

	// `go install` refused to touch anything outside GOBIN, which incidentally
	// protected a developer's own build. Binary replacement has no such limit,
	// so the protection has to be stated rather than inherited.
	if u.SourceBuild {
		decision.InstallCommand = releasesPage
		decision.Reason = fmt.Sprintf(
			"%s was built from source, not installed from a release; "+
				"replacing it would overwrite your own build",
			decision.ExecutablePath)
		return decision, nil
	}

	if err := u.writable()(decision.ExecutablePath); err != nil {
		decision.InstallCommand = releasesPage
		// Name the directory the update would actually write to. For a symlinked
		// install that is the target's directory, and pointing at the symlink's
		// instead would send the user to fix permissions on the wrong one.
		decision.Reason = fmt.Sprintf(
			"%s is not writable by this user; re-run with sudo, or reinstall claudeops somewhere you own",
			filepath.Dir(installTarget(decision.ExecutablePath)))
		return decision, nil
	}

	decision.CanAuto = true
	return decision, nil
}

// releasesPage is where a human goes when no automatic path is available.
const releasesPage = ReleasesPage

// ReleasesPage is where a human goes when no automatic path is available.
const ReleasesPage = "https://github.com/fullfran/claudeops-tui/releases/latest"

// goInstallTarget reports the path `go install` would write, when Go is
// available at all. A missing toolchain is not a failure any more — it just
// means this installation updates the other way.
func (u Updater) goInstallTarget(ctx context.Context) (string, bool) {
	if _, err := u.Runner.LookPath("go"); err != nil {
		return "", false
	}
	env, err := u.Runner.GoEnv(ctx)
	if err != nil {
		return "", false
	}
	path := expectedBinaryPath(env, u.Binary)
	return path, path != ""
}

func (u Updater) writable() func(string) error {
	if u.IsWritable != nil {
		return u.IsWritable
	}
	return Writable
}

func (u Updater) installer() BinaryUpdater {
	if u.Installer != nil {
		return u.Installer
	}
	return BinaryInstaller{}
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

	// Stop before touching the binary. Installing to arrive at the version you
	// already have is a slow no-op; installing an older one is destructive, and
	// the old post-install check could only report that after the fact.
	if decision.Downgrade {
		return decision, fmt.Errorf(
			"%w: the newest published version is %s, older than the %s you are running\n"+
				"installing would remove whatever this build has that %s does not",
			ErrStaleRelease, decision.LatestVersion, u.Version, decision.LatestVersion)
	}
	if decision.UpToDate {
		return decision, nil
	}

	if decision.Method == MethodBinary {
		return u.updateBinary(ctx, decision)
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
		// `claudeops version` prints commit and build date beneath the version
		// line. Only the first line is the contract we parse — keeping the rest
		// would make the build date look like the version.
		decision.InstalledNow = firstLine(string(versionOut))
	}

	// Guard against the module proxy serving a stale (older) version.
	// extractSemver parses "claudeops X.Y.Z" → "X.Y.Z".
	if installedVer := extractSemver(decision.InstalledNow); installedVer != "" {
		if semverLT(installedVer, u.Version) {
			return decision, fmt.Errorf(
				"update installed version %s which is older than current %s\n"+
					"the module proxy cached a stale release — retry with:\n"+
					"  GOPROXY=direct %s",
				installedVer, u.Version, decision.InstallCommand)
		}
	}

	return decision, nil
}

// updateBinary replaces the running executable with the published release.
//
// It needs to know which version to fetch, and the only answer it has is the
// one Decide resolved. Without it there is no asset name to request, so this
// stops rather than guessing.
func (u Updater) updateBinary(ctx context.Context, decision Decision) (Decision, error) {
	if decision.LatestVersion == "" {
		return decision, fmt.Errorf(
			"%w: could not reach the release index to find out what to download\n"+
				"download manually: %s", ErrManual, releasesPage)
	}

	// Report the file that is actually rewritten. For a symlinked install the
	// update follows the link, so naming the link would name an untouched file.
	decision.InstalledPath = installTarget(decision.ExecutablePath)

	if err := u.installer().Install(ctx, decision.LatestVersion, decision.ExecutablePath); err != nil {
		return decision, fmt.Errorf(
			"automatic update failed: %w\n"+
				"if the tag was pushed only moments ago its archives may still be building; "+
				"otherwise download manually: %s",
			err, releasesPage)
	}

	// Ask the new binary what it is rather than assuming it matches the tag it
	// was published under. A mismatch means the release is not what it claims,
	// and reporting the tag would hide that behind a success message — and the
	// next update would download the very same archive again.
	if out, err := u.Runner.Run(ctx, decision.InstalledPath, "version"); err == nil {
		decision.InstalledNow = firstLine(string(out))
	}
	if installed := extractSemver(decision.InstalledNow); installed != "" && installed != decision.LatestVersion {
		return decision, fmt.Errorf(
			"installed %s but release v%s was requested\n"+
				"the published archive does not match its tag; report this and reinstall from %s",
			installed, decision.LatestVersion, releasesPage)
	}

	return decision, nil
}

// extractSemver pulls the version string out of "claudeops X.Y.Z". It reads the
// second field rather than the last so anything appended to the line later is
// ignored instead of being mistaken for the version.
func extractSemver(s string) string {
	parts := strings.Fields(s)
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

// firstLine returns the first non-empty line of s, trimmed.
func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}

// semverLT returns true when a < b (simple major.minor.patch comparison).
// A version string that does not yield all three components is treated as
// incomparable and never reported as older: Sscanf would otherwise leave the
// zero value behind, making an unparseable version look like 0.0.0 and any
// real version look newer than it.
func semverLT(a, b string) bool {
	av, aok := parseSemver(a)
	bv, bok := parseSemver(b)
	if !aok || !bok {
		return false
	}
	for i := range av {
		if av[i] != bv[i] {
			return av[i] < bv[i]
		}
	}
	// Same base version. A release candidate precedes the release it is a
	// candidate for, so 0.14.0-rc.1 < 0.14.0 — without this every RC tester is
	// stranded, because the GA release compares as "not newer" and the update
	// is then refused as a downgrade.
	//
	// Two prereleases of the same base are left equal. Ordering rc.2 above rc.1
	// needs the full semver identifier rules, and nothing here depends on it.
	return hasPrerelease(a) && !hasPrerelease(b)
}

func hasPrerelease(v string) bool {
	return strings.Contains(strings.TrimPrefix(v, "v"), "-")
}

// parseSemver splits "vX.Y.Z" (and "X.Y.Z-suffix") into its numeric parts.
// It reports false unless all three components were read.
func parseSemver(v string) ([3]int, bool) {
	v = strings.TrimPrefix(v, "v")
	var maj, min, pat int
	// A trailing suffix such as "-rc1" leaves n == 3 and is intentionally
	// ignored, so prereleases still compare on their base version.
	if n, _ := fmt.Sscanf(v, "%d.%d.%d", &maj, &min, &pat); n < 3 {
		return [3]int{}, false
	}
	return [3]int{maj, min, pat}, true
}

func resolveOrClean(r Runner, path string) string {
	resolved, err := r.EvalSymlinks(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return resolved
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

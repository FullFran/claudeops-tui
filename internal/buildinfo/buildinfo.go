// Package buildinfo carries the identity of the running binary: which version
// it is, which commit it was built from, and when.
//
// There are three ways a claudeops binary comes into existence, and each one
// knows a different amount about itself:
//
//   - A release build. GoReleaser injects version, commit and date with
//     -ldflags, so all three are exact. This is the authoritative case.
//   - `go install github.com/fullfran/claudeops-tui/cmd/claudeops@vX.Y.Z`.
//     No ldflags, but the toolchain records the module version in the embedded
//     build info, so the version is still exact.
//   - `go build` from a source tree. Neither is available, so the version falls
//     back to defaultVersion below — what this source tree claims to be.
//
// defaultVersion is therefore the one value still bumped by hand, in the
// release commit. The release workflow refuses to publish a tag that disagrees
// with it, so the claim cannot drift away from the tag unnoticed.
package buildinfo

import (
	"runtime/debug"
	"strings"
)

// defaultVersion is the version this source tree claims to be. Bump it in the
// release commit; .github/workflows/release.yml asserts it against the tag.
const defaultVersion = "0.13.1"

// unknownValue is reported for build metadata no source could supply.
const unknownValue = "unknown"

// Injected at link time by GoReleaser. Never read these directly — the
// accessors below apply the fallback chain.
var (
	version string
	commit  string
	date    string
)

// Version reports the semantic version without a leading "v".
//
// The "v" is stripped because every consumer downstream — the self-update
// check in internal/update, the TUI header — compares bare X.Y.Z strings.
func Version() string { return resolveVersion(version, moduleVersion()) }

// Commit reports the short commit the binary was built from, or "unknown".
func Commit() string { return resolveCommit(commit, vcsSetting("vcs.revision")) }

// Date reports the build timestamp, or "unknown".
func Date() string { return resolveDate(date, vcsSetting("vcs.time")) }

func resolveVersion(ldflags, module string) string {
	if v := normalize(ldflags); v != "" {
		return v
	}
	// "(devel)" is what the toolchain records for a build that is not pinned to
	// a module version, which tells us nothing the source tree does not already.
	if v := normalize(module); v != "" && v != "(devel)" {
		return v
	}
	return defaultVersion
}

func resolveCommit(ldflags, vcs string) string {
	if c := strings.TrimSpace(ldflags); c != "" {
		return c
	}
	if c := strings.TrimSpace(vcs); c != "" {
		if len(c) > 7 {
			return c[:7]
		}
		return c
	}
	return unknownValue
}

func resolveDate(ldflags, vcs string) string {
	if d := strings.TrimSpace(ldflags); d != "" {
		return d
	}
	if d := strings.TrimSpace(vcs); d != "" {
		return d
	}
	return unknownValue
}

// normalize trims surrounding whitespace and the conventional "v" prefix.
func normalize(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

func moduleVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return info.Main.Version
}

func vcsSetting(key string) string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range info.Settings {
		if s.Key == key {
			return s.Value
		}
	}
	return ""
}

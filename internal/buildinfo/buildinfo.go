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
	"regexp"
	"runtime/debug"
	"strings"
)

// defaultVersion is the version this source tree claims to be. Bump it in the
// release commit; .github/workflows/release.yml asserts it against the tag.
const defaultVersion = "0.14.0"

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

// FromSource reports whether this binary was built from a source tree rather
// than installed from a published release.
//
// Version() cannot answer this: it deliberately reports defaultVersion for a
// source build, which is indistinguishable from a real release of that number.
// The self-updater needs the difference, because replacing a developer's own
// build with a published archive destroys the thing they were testing.
func FromSource() bool { return isSourceBuild(version, moduleVersion()) }

func isSourceBuild(ldflags, module string) bool {
	if normalize(ldflags) != "" {
		return false // only a release build carries ldflags
	}
	return !isReleaseVersion(normalize(module))
}

func resolveVersion(ldflags, module string) string {
	if v := normalize(ldflags); v != "" {
		return v
	}
	if v := normalize(module); isReleaseVersion(v) {
		return v
	}
	return defaultVersion
}

// pseudoVersion matches the timestamp-and-commit tail the Go toolchain appends
// when it derives a version from VCS state rather than a tag.
var pseudoVersion = regexp.MustCompile(`[0-9]{14}-[0-9a-f]{12}`)

// isReleaseVersion reports whether v is a version someone actually tagged.
//
// Only `go install ...@vX.Y.Z` records one. A `go build` inside a git checkout
// records a pseudo-version instead — `0.13.2-0.20260806113558-8abf581c650b`,
// possibly with `+dirty` — which is derived from the commit, not published.
// Reporting that would be actively harmful: it parses as newer than the latest
// release, so `claudeops update` reads a source build as being ahead of every
// published version and refuses to install one, calling it a downgrade.
//
// "(devel)" is the same story with less detail. All of them fall back to the
// version this source tree claims to be.
func isReleaseVersion(v string) bool {
	switch {
	case v == "", v == "(devel)":
		return false
	// Build metadata (+dirty, +incompatible) never appears on a plain tag.
	case strings.Contains(v, "+"):
		return false
	case pseudoVersion.MatchString(v):
		return false
	}
	return true
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

# Releasing claudeops

Releases are cut by pushing a version tag. GitHub Actions is the only publishing
authority — never run `goreleaser release` against this repo from a laptop.

## The one manual step

`internal/buildinfo.defaultVersion` is the version this source tree claims to
be. It is what a `go install ...@latest` build reports, and the release workflow
refuses to publish a tag that disagrees with it.

So a release is: bump that constant, merge, tag.

## Cutting a release

```bash
# 1. main is green and up to date
git checkout main && git pull
make ci

# 2. Bump internal/buildinfo/buildinfo.go → defaultVersion = "0.14.0"
#    then commit it on a branch and merge through a PR as usual:
#      chore: release v0.14.0

# 3. Prove the release builds before creating the tag
make release-check                 # validates .goreleaser.yaml
make version-check TAG=v0.14.0     # source version must match the tag
make snapshot                      # builds every artifact, publishes nothing

# 4. Tag and push — this is what publishes
git checkout main && git pull
git tag -a v0.14.0 -m "v0.14.0"
git push origin v0.14.0
```

The tag push runs `.github/workflows/release.yml`: version check → `go test
-race ./...` → GoReleaser → GitHub Release with archives and `checksums.txt`.

## What ships

Six archives plus a checksum file:

```
claudeops_0.14.0_linux_amd64.tar.gz
claudeops_0.14.0_linux_arm64.tar.gz
claudeops_0.14.0_darwin_amd64.tar.gz
claudeops_0.14.0_darwin_arm64.tar.gz
claudeops_0.14.0_windows_amd64.zip
claudeops_0.14.0_windows_arm64.zip
checksums.txt
```

Each archive carries the binary, `README.md` and `LICENSE`. Builds are
`CGO_ENABLED=0 -trimpath`, which is what lets one Linux runner cross-compile all
six — including `modernc.org/sqlite`, which is pure Go.

## Release candidates

`prerelease: auto` in `.goreleaser.yaml` means a tag with a suffix publishes as
a prerelease automatically:

```bash
git tag -a v0.14.0-rc.1 -m "v0.14.0 release candidate 1"
git push origin v0.14.0-rc.1
```

Worth using whenever a release touches ingestion, the store schema, or pricing —
anything that could damage an existing `~/.claudeops/claudeops.db`.

`defaultVersion` stays `0.14.0` for an `-rc.1` tag: the version check compares
base versions only, because a candidate is cut from the same source tree as the
release it rehearses. The published binary still reports the full
`0.14.0-rc.1`, which comes from the tag via ldflags.

## Versioning

While the project is pre-1.0:

| Change                          | Bump                |
| ------------------------------- | ------------------- |
| Bug fix, no behavior change      | patch `0.13.0 → 0.13.1` |
| New capability, compatible       | minor `0.13.1 → 0.14.0` |
| Breaking change to CLI, config, DB layout, or hook contract | minor, and say so loudly in the release notes |

After 1.0, breaking changes take a major bump.

## Which commits appear in the notes

GoReleaser builds the changelog from commit subjects and drops `docs:`, `test:`,
`chore:`, `ci:`, `style:`, `refactor:` and merge commits. Release notes are for
someone deciding whether to upgrade — if a change matters to them, it needs a
`feat:` or `fix:` subject.

## If a release goes wrong

A published Release can be deleted and the tag re-pushed, but anyone who already
ran `go install @latest` has the bad version, and the Go module proxy caches
tags immutably — the proxy will keep serving that version forever. Prefer
rolling forward with a patch release over trying to erase one.

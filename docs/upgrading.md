# Upgrading

Behavior changes that need action from you, newest first. Everything not listed
here is backward compatible: config keys fall back to defaults and the store
migrates itself on open.

## October 2026 Model Catalog & Pricing Update

The pricing engine now supports context-dependent tiers and scheduled rate cutoffs:

- **Claude 5.5 models**: Added `claude-sonnet-5-5` (with explicit 1h cache-write rate €3.68) and `claude-haiku-5-5` with whole-request tiered pricing (base rates up to 100,000 prompt tokens; higher tier above 100,000 tokens).
- **GPT-6 & 6.1 models**: Standard short (≤272,000 prompt tokens) and long (>272,000 prompt tokens) context tiers for `gpt-6-astra`, `gpt-6-sol`, `gpt-6-luna`, `gpt-6.1-sol`, and `gpt-6.1-terra`.
- **Scheduled transitions**: `gemini-4-argon` (effective 2026-12-01 UTC) and `gemini-3.8-flash` (effective 2027-01-01 UTC) automatically switch from promotional base rates to standard rates based on the event timestamp.

### Backward compatibility & customizations

Existing `pricing.toml` files preserve all user customizations:
- Only untouched models matching the exact old seed entries are enriched with the new tier and schedule metadata.
- If you customized any price for an existing model, your custom rates and configurations remain untouched.
- Malformed or invalid configurations (such as unparseable dates or negative thresholds) are rejected during validation without altering your existing pricing file.
- Historical stored costs in your database are unchanged; no re-ingestion is required.

## `CODEX_HOME` now means the Codex home directory

`CodexRoot()` used to return `$CODEX_HOME` verbatim as the rollouts root, which
contradicted its own documentation and Codex's own layout. It now resolves
`$CODEX_HOME/sessions`, matching the unset default of `~/.codex/sessions`.

If you worked around the old behavior by setting `CODEX_HOME=~/.codex/sessions`,
change it:

```bash
# before (worked around the old bug)
export CODEX_HOME=~/.codex/sessions

# now
export CODEX_HOME=~/.codex
```

Leaving the old value pointed at `~/.codex/sessions/sessions`, which does not
exist, so Codex ingestion silently finds nothing. Unset `CODEX_HOME` entirely if
you use the default location.

## Codex users should re-ingest

Codex rollout files carry no session id inside their events, so claudeops
synthesizes one. It previously derived event uuids without the per-file
identity, which collapsed every rollout file onto a single session and made
lines at equal byte offsets in different files collide on the same uuid. The
identity now comes from the rollout filename.

Historical Codex rows in your database were written under the old scheme. Fix
them:

```bash
claudeops reingest
```

This clears events, sessions, projects, file offsets and source watermarks, then
rebuilds them from the source files. Tasks and config are preserved. Pass
`--yes` to skip the confirmation prompt.

## Credentials handling

`~/.claude/.credentials.json` is shared with Claude Code, and refreshes are now
safer:

- **Real file locking.** The whole load → refresh → save sequence runs under an
  exclusive advisory lock, so two claudeops processes (or claudeops and Claude
  Code) cannot interleave a rotation. The lock lives on a sidecar
  `~/.claude/.credentials.json.lock` file, because saving replaces the
  credentials file by rename and a lock on the original inode would be dropped.
  You may see that `.lock` file appear; it is ours and safe to delete when
  nothing is running.
- **Millisecond `expiresAt`.** Claude Code writes `expiresAt` in epoch
  milliseconds. claudeops now detects the unit on read and writes it back in the
  same unit, instead of assuming seconds and treating every token as expired.
- **Unknown fields survive.** Fields inside `claudeAiOauth` that claudeops does
  not model (`scopes`, `subscriptionType`, `rateLimitTier`, …) are round-tripped
  verbatim, so a refresh no longer strips them from the file Claude Code reads.

No action needed — but if an older claudeops rewrote your credentials file and
Claude Code started asking you to log in again, `claude /login` restores it.

## `make lint` changed meaning

| Target | Before | Now |
|---|---|---|
| `make lint` | `gofmt -l` + `go vet` | `golangci-lint run` (advisory) |
| `make fmt-check` | — | fails when a tracked Go file needs formatting |
| `make vet` | — | `go vet ./...` |
| `make ci` | — | `fmt-check` + `vet` + `build` + `race`, then advisory `lint` |

If your scripts called `make lint` for the old formatting-and-vet gate, call
`make fmt-check vet` instead — or `make ci`, which mirrors the CI workflow.
`golangci-lint` must be installed for `make lint`; `make ci` runs it advisorily
so a missing binary does not fail the build.

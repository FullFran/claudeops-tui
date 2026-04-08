# Exploration: claudeops-mvp

## Current State

Greenfield repo. No code, no go.mod. The "system" today is the user's `~/.claude/` directory tree, populated by Claude Code CLI (verified version `2.1.96`):

- `~/.claude/projects/<cwd-slug>/<session-uuid>.jsonl` — append-only event logs, one file per Claude Code session. Slug encodes the cwd of the session (e.g. `-home-franblakia-fullfran-ClaudeOps-TUI`).
- Each line is a typed JSON event. Relevant types observed:
  - `permission-mode`, `file-history-snapshot`, `attachment` → noise, ignore.
  - `user` → user prompts (no tokens).
  - `assistant` → contains `message.usage` with `input_tokens`, `cache_creation_input_tokens`, `cache_read_input_tokens`, `output_tokens`, `server_tool_use.{web_search_requests, web_fetch_requests}`. Also `model` (e.g. `claude-opus-4-6`), `sessionId`, `cwd`, ISO `timestamp`, `uuid`, `parentUuid`.
- `~/.claude/.credentials.json` (mode 0600) → contains `claudeAiOauth.accessToken` (`sk-ant-oat01-…`), `refreshToken` (`sk-ant-ort01-…`), and `expiresAt`.
- No local cache of subscription usage anywhere under `~/.claude/`. Confirmed by directory inspection.

The undocumented endpoint `GET https://api.anthropic.com/api/oauth/usage` (header `anthropic-beta: oauth-2025-04-20`, Bearer with the OAuth access token) returns the EXACT data Claude Code's `/usage` slash command renders:

```json
{
  "five_hour":      {"utilization": 6.0,  "resets_at": "..."},
  "seven_day":      {"utilization": 35.0, "resets_at": "..."},
  "seven_day_opus": {"utilization": 12.0, "resets_at": "..."}
}
```

No public/community Go tool calls this endpoint today. Existing tools (Maciek-roboblog/Claude-Code-Usage-Monitor, ccusage) parse jsonl and estimate against hardcoded plan limits — they show token counts, not real subscription %. **This is the differentiator for claudeops.**

## Affected Areas

Greenfield, so "affected" = "to be created":

- `go.mod`, `go.sum` — module `github.com/fullfran/claudeops-tui`, Go 1.22+.
- `cmd/claudeops/main.go` — entrypoint. Routes between TUI (default) and CLI subcommands (`task start|stop|list`, `collector run`, `config`).
- `internal/parser/` — JSONL line parser. Strongly typed `Event` interface with `AssistantEvent` carrying full token breakdown.
- `internal/collector/` — fsnotify watcher on `~/.claude/projects/`, per-file offset tracking persisted in SQLite, incremental tail.
- `internal/store/` — `modernc.org/sqlite` (pure Go, no CGO). Tables: `events`, `sessions`, `projects`, `tasks`, `task_events` (many-to-many), `file_offsets`, `config`.
- `internal/pricing/` — loads `~/.claudeops/pricing.toml`, calculates € per event using the four token-class breakdown.
- `internal/usage/` — HTTP client for `/api/oauth/usage`, reads token from `~/.claude/.credentials.json`, refreshes via `console.anthropic.com/v1/oauth/token` when expired, caches response ~60s. Detects API-key-only credentials and degrades gracefully.
- `internal/tasks/` — read/write `~/.claudeops/current-task.json`, correlate against incoming events by `(sessionId, timestamp ∈ [start, stop])`.
- `internal/tui/` — Bubbletea model. Single dashboard view in MVP.
- `configs/pricing.toml` — seed file with current Opus 4.6, Sonnet 4.6, Haiku 4.5 prices.
- `docs/` — plan, architecture, JSONL format reference, OAuth endpoint reference (in parallel, requested by user).

## Approaches

### A. Single binary, one process, on-demand collector
The TUI process also runs the collector goroutine internally. No daemon.
- **Pros**: Zero install friction, no systemd unit, no orphan process. Simpler mental model. Sufficient for "I open the TUI when I want to look".
- **Cons**: Data only ingests while TUI is open. Misses sessions that happen when the TUI is closed → gaps.
- **Effort**: Low.

### B. Long-running daemon + thin TUI client
`claudeops collector run` as a daemon (systemd user unit / launchd) writing to SQLite; the TUI is just a reader.
- **Pros**: Continuous capture, no gaps. Multiple TUI clients can attach. Cleanest separation of concerns.
- **Cons**: Install/setup friction (systemd unit per dev), daemon lifecycle to debug, race conditions on SQLite if not handled with WAL.
- **Effort**: Medium.

### C. Hybrid — TUI runs collector if no daemon detected, else attaches as reader
TUI on startup checks for a running daemon (PID file). If absent, spawns collector goroutine in-process. If present, reads only.
- **Pros**: Works out of the box (Approach A behavior), upgrades cleanly to daemon mode (Approach B) when user installs the unit. No forced choice.
- **Cons**: Slightly more code paths to test. Two write modes to reason about.
- **Effort**: Medium.

### D. Backfill on start, no live watcher
TUI on start replays all jsonl from last offset. No fsnotify, no continuous capture.
- **Pros**: Dead simple. No goroutines, no watcher edge cases.
- **Cons**: Breaks the "live dashboard" promise. The user explicitly asked for a TUI that *monitors*. Reject.
- **Effort**: Very low.

## Recommendation

**Approach C (Hybrid)** for the MVP, but ship it phased:

1. **MVP (this change)**: Implement Approach A first — collector embedded in the TUI process. This validates the parser, store, pricing, usage client, and TUI rendering with the smallest surface area. Document the "gap" caveat in `docs/limitations.md`.
2. **Immediately after MVP** (still Fase 1, separate change): Add `claudeops collector run` daemon mode and a PID-file detection so the same binary works either way. Write the systemd user unit as a doc, not as an installer.

This staging keeps the MVP tight (one process, one entrypoint) while leaving a clean upgrade path. Approach B as MVP would inflate scope. Approach C as MVP would ship two execution modes before either is proven.

Concrete MVP execution mode: **single process, embedded collector goroutine, single-view dashboard**.

## Risks

1. **Undocumented `/api/oauth/usage` endpoint**: Anthropic can change or remove it without notice. **Mitigation**: feature flag `usage.enabled` in config; graceful degradation to "weekly % unavailable" with a clear message; fallback path that reads token totals from the parsed jsonl events as an approximate denominator if the user opts in.
2. **OAuth token refresh**: writing back to `~/.claude/.credentials.json` is shared state with Claude Code itself. A race could corrupt credentials. **Mitigation**: atomic write (temp file + `os.Rename`), file lock (`flock`), preserve mode 0600. NEVER write if the parsed JSON is partial.
3. **JSONL format drift**: Claude Code is on a fast release train (`2.1.96` today). Event shapes can change between minor versions. **Mitigation**: parser is permissive — unknown event types are logged and skipped, never crash the collector. Pin a `min_supported_version` and surface a warning if `version` field exceeds known range.
4. **SQLite concurrent writers**: when we add daemon mode in the next change, two writers will exist briefly. **Mitigation**: WAL mode from day one; single-writer discipline enforced via the store package (only one `*Store.Open` per process can write).
5. **Pricing table goes stale**: € calculations will be wrong when Anthropic changes prices. **Mitigation**: ship `pricing.toml` editable, surface "pricing last updated: {date}" in the dashboard footer, document the source URL in comments.
6. **Task correlation false positives**: a task that started 3h ago is still "active" and tags every new event from that session — even unrelated ones. **Mitigation**: max-age cap (default 4h, configurable), explicit `task stop` command, and a TUI hint when a task has been active too long.
7. **fsnotify on Linux has watcher limits**: `fs.inotify.max_user_watches` can be exceeded if the user has many sessions. **Mitigation**: watch only the `projects/` parent directory, not each subdir individually; rely on rename/create events to discover new session files.

## Ready for Proposal

**Yes.** All open questions are resolved:
- Stack confirmed (Go + Bubbletea + SQLite modernc + fsnotify).
- Module path confirmed (`github.com/fullfran/claudeops-tui`).
- Usage data source confirmed (real OAuth endpoint, not estimation).
- Pricing strategy confirmed (editable TOML).
- Task correlation strategy confirmed (sidecar JSON + sessionId/timestamp window).
- MVP scope locked (parser, collector, store, pricing, usage, tasks, single dashboard view).
- Out of scope for this change: multi-device sync, alerts, burn-rate prediction, daemon mode (deferred to subsequent Fase 1 changes).

The orchestrator can proceed directly to `sdd-propose` with change name `claudeops-mvp`.

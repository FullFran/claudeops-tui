# Proposal: ClaudeOps MVP — Local Usage TUI

## Intent

Devs using Claude Code (Pro/Max plans) have no unified view of: real subscription % consumed (session/weekly/per-model), cost in € per session/project, and which task burned which tokens. Existing tools either estimate against guessed limits (Maciek, ccusage) or live in IDE plugins. None correlate usage to *tasks*. ClaudeOps gives a dev (and a 3-person team, later) a local TUI showing **real** Anthropic data plus task-level attribution.

## Scope

### In Scope
- Parse `~/.claude/projects/*.jsonl` incrementally (fsnotify + persisted offsets)
- Persist events, sessions, projects, tasks in local SQLite (modernc, no CGO)
- Editable `pricing.toml` and per-event € calculation with token-class breakdown
- HTTP client for `GET https://api.anthropic.com/api/oauth/usage` with OAuth refresh
- `claudeops task start|stop|list` CLI + sidecar `~/.claudeops/current-task.json`
- Bubbletea single-view dashboard: 5h%, 7d%, 7d-opus%, today's €, top sessions, top projects, active task
- `docs/` folder with plan, JSONL format reference, OAuth endpoint reference

### Out of Scope
- Multi-device sync (Fase 2)
- Daemon mode / `collector run` separate process (next change in Fase 1)
- Burn-rate prediction, alerts at thresholds (Fase 2)
- Team backend, web dashboard, multi-tenant (Fase 3)
- Auto-learning quota when rate-limited (Fase 2)

## Capabilities

### New Capabilities
- `jsonl-ingestion`: parse Claude Code session JSONL, classify events, tail incrementally with persisted offsets
- `usage-store`: SQLite schema and queries for events, sessions, projects, tasks, offsets, config
- `pricing`: load pricing TOML, compute € per event using 4 token classes
- `subscription-usage`: call undocumented `/api/oauth/usage`, manage OAuth token refresh, cache response, degrade when API key only
- `task-tracking`: sidecar JSON for current task, correlate events by `(sessionId, timestamp window)`, max-age cap
- `dashboard-tui`: Bubbletea model rendering one consolidated view with live refresh

### Modified Capabilities
- None (greenfield)

## Approach

Single binary `claudeops`. TUI process embeds the collector goroutine (Approach A from exploration). Parser is a permissive line-by-line decoder; unknown event types are skipped. Store uses WAL mode and a single-writer discipline. Usage client owns credential I/O via atomic writes (temp + rename, mode 0600, file lock). Task correlation runs at write time inside the collector, not at query time, to keep dashboard reads cheap.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `go.mod` | New | module `github.com/fullfran/claudeops-tui`, Go 1.22+ |
| `cmd/claudeops/` | New | entrypoint + subcommand router |
| `internal/parser` | New | typed event decoder |
| `internal/collector` | New | fsnotify watcher + offset tail |
| `internal/store` | New | SQLite schema + queries |
| `internal/pricing` | New | TOML loader + calculator |
| `internal/usage` | New | OAuth endpoint client + refresh |
| `internal/tasks` | New | sidecar + correlation |
| `internal/tui` | New | Bubbletea dashboard |
| `configs/pricing.toml` | New | seed prices |
| `docs/` | New | plan, JSONL ref, OAuth ref |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| `/api/oauth/usage` removed/changed by Anthropic | Med | Feature flag, graceful degrade message, fallback to jsonl-only mode |
| OAuth credential file race with Claude Code itself | Low | Atomic write + flock + 0600 + abort on partial parse |
| JSONL format drift across CLI versions | High | Permissive parser, log+skip unknowns, surface version warning |
| Pricing table goes stale | High | Editable TOML, footer "updated:" date, source URL in comments |
| Task correlation false positives | Med | Max-age cap (4h default), explicit `task stop`, TUI hint |

## Rollback Plan

Single-binary, local-only, no shared state. Rollback = `rm ~/.claudeops/ && rm <binary>`. SQLite and config live in `~/.claudeops/`; nothing touches `~/.claude/` except *reads* of `projects/` and atomic credential refresh. If credential refresh corrupts `~/.claude/.credentials.json`, restore via `claude /login` (Claude Code re-issues tokens).

## Dependencies

- Go 1.22+
- `github.com/charmbracelet/bubbletea`, `bubbles`, `lipgloss`
- `modernc.org/sqlite`
- `github.com/fsnotify/fsnotify`
- `github.com/BurntSushi/toml`

## Success Criteria

- [ ] `claudeops` launches, ingests existing jsonl, shows non-zero numbers in <2s on a populated `~/.claude/projects/`
- [ ] Dashboard shows real `five_hour`, `seven_day`, `seven_day_opus` values matching Claude Code's `/usage` output
- [ ] `claudeops task start "X"` followed by Claude Code activity attributes events to that task within the time window
- [ ] € total for today matches manual calculation against `pricing.toml`
- [ ] `go test -race ./...` is clean
- [ ] Works on a fresh clone via `go build ./cmd/claudeops` with no CGO toolchain

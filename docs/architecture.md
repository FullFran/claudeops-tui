# Architecture

## Package map

```
cmd/claudeops/main.go            entrypoint + subcommand router (default → TUI)
internal/
  parser/    typed JSONL line decoder
  collector/ fsnotify watcher + per-file tail with persisted offsets
  store/     SQLite (modernc) schema + queries; single writer
  pricing/   TOML loader + per-event cost calculator
  usage/     /api/oauth/usage client + OAuth refresh + atomic creds I/O
  tasks/     sidecar current-task.json + correlation
  tui/       Bubbletea model/view/update
  config/    paths, env, defaults
configs/pricing.toml             embedded via go:embed, copied on first run
```

## Data flow

```
                    fsnotify events
                          │
~/.claude/projects ──→ collector ──→ parser ──→ ingestCh ──→ store ──→ SQLite (WAL)
                          │            │                                  ▲
                          │            └─ pricing.Calculate ──────────────┤
                          │                                               │
                          │      tasks.Resolve(sessionId, ts) ────────────┤
                          ▼
                     offset persistence
                                                                          │
TUI ◀─── store.AggregatesForToday() ──────────────────────────────────────┤
TUI ◀─── usage.Get() ─→ HTTP /api/oauth/usage (60s cache) ────────────────┘
                              │
                              └─→ refresh via console.anthropic.com/v1/oauth/token
```

## Concurrency model

- One collector goroutine drives `fsnotify`
- One tail goroutine per active session file
- One store writer goroutine drains a buffered ingest channel (1024)
- Bubbletea's runtime owns its goroutines and ticks every 2s for refresh
- Single-writer rule: only the store writer issues `INSERT`. Backpressure flows naturally through the channel.

## Key decisions (rationale lives in `openspec/changes/claudeops-mvp/design.md`)

| # | Decision |
|---|----------|
| 1 | `modernc.org/sqlite` (pure Go) over CGO driver — single-binary build |
| 2 | Embedded collector in TUI process (Approach A) — smallest MVP |
| 3 | Single-writer store + buffered channel — no DB busy races |
| 4 | Task correlation at write-time, not query-time — O(1) reads |
| 5 | Permissive parser, skip unknown event types — JSONL format drift resilience |
| 6 | OAuth refresh inside the usage client, atomic temp+rename — credential safety |
| 7 | Bubbletea single-model dashboard — no premature router |
| 8 | `go test` table-driven + `teatest` for TUI snapshots |

## SQLite schema (DDL summary)

`projects(id, cwd UNIQUE, name)` → `sessions(id, project_id FK, first_seen, last_seen)` → `events(uuid, session_id FK, ts, type, model, in_tokens, out_tokens, cache_read_tokens, cache_create_tokens, cost_eur, task_id FK)`. Plus `tasks(id, name, started_at, ended_at, max_age_seconds)`, `file_offsets(path, offset, size)`, `config(key, value)`. WAL mode. Indexes on `events(ts)`, `events(session_id)`, `events(task_id)`.

Full DDL: `openspec/changes/claudeops-mvp/design.md`.

## Where things live at runtime

| Path | Owner | Purpose |
|---|---|---|
| `~/.claude/projects/*/` | Claude Code | source data — read only |
| `~/.claude/.credentials.json` | Claude Code (shared) | OAuth tokens — atomic refresh only |
| `~/.claudeops/claudeops.db` | claudeops | local store |
| `~/.claudeops/pricing.toml` | claudeops | editable price table |
| `~/.claudeops/current-task.json` | claudeops | sidecar for task tracking |
| `~/.claudeops/config.toml` | claudeops | data dir, claude dir, sync endpoint (Fase 2) |

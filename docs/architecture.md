# Architecture

## Package map

```
cmd/claudeops/main.go            entrypoint + subcommand router (default → TUI)
internal/
  source/    ingestion seam: LineParser, LineContext, Record, Sink, StoreSink
  parser/    typed Claude JSONL line decoder
  codex/     Codex rollout parser + CODEX_HOME resolution
  opencode/  poller for opencode's SQLite database
  collector/ fsnotify watcher with persisted byte offsets
  store/     SQLite (modernc) schema + queries
  pricing/   TOML loader + per-event cost calculator
  usage/     /api/oauth/usage client + OAuth refresh + locked atomic creds I/O
  provider/  pluggable live-quota adapters (Codex, Copilot, Gemini, generic)
  live/      live Claude Code session discovery (Classroom tab)
  hooks/     Claude Code hook install/uninstall/status/handle
  export/    OTLP metric push + Claude Code OTel env var management
  insights/  computed usage insights
  tasks/     sidecar current-task.json + correlation
  tui/       Bubbletea model/view/update
  mcpserver/ MCP protocol handler over stdio (read-only store)
  config/    paths, settings, defaults
  update/    self-update
internal/pricing/pricing.seed.toml   embedded via go:embed, copied on first run
```

## Data flow

```
~/.claude/projects/*.jsonl ──┐
$CODEX_HOME/sessions/**  ────┤ fsnotify + persisted byte offsets
                             ▼
                        collector ──→ LineParser ──→ []source.Record ──┐
                             │                                          │
opencode.db ──5s poll──→ opencode.Ingester ────────────────────────────┤
                                                                        ▼
                                                              source.StoreSink
                                                                        │
                                              pricing.Calculate ────────┤
                                     tasks.Resolve(sessionId, ts) ──────┤
                                                                        ▼
                                                          store.Insert ──→ SQLite (WAL)
                                                                              │
TUI ◀─── store aggregates (2s tick) ──────────────────────────────────────────┤
TUI ◀─── usage.Get() ─→ HTTP /api/oauth/usage (cache_ttl_seconds, default 300) ┤
TUI ◀─── provider.Registry ─→ Codex / Copilot / Gemini / generic quotas       │
TUI ◀─── live.Sessions() ─→ ~/.claude/projects mtimes + ~/.claudeops/live     │
MCP ◀─── read-only store ─────────────────────────────────────────────────────┘
                              │
usage refresh ────────────────┴─→ POST console.anthropic.com/v1/oauth/token
```

## Concurrency model

- `cmdTUI` starts one goroutine per enabled line-based source collector (claude,
  codex), plus one for the opencode poller.
- Each collector runs **one** fsnotify event loop. Events mark files dirty; a
  500ms ticker flushes the dirty set by re-reading each file sequentially from
  its persisted offset. There is no goroutine per file.
- Every collector goroutine calls `store.Insert` **directly**. There is no
  ingest channel and no dedicated writer goroutine. Concurrent writes are
  serialized by SQLite itself: WAL mode plus `busy_timeout(5000)`.
- Bubbletea owns its own runtime and ticks every 2s; async work is dispatched as
  commands returning `Msg` values.
- `usage.Client` holds a mutex across its whole fetch/refresh path, so N
  concurrent `Get()` calls trigger at most one network refresh. Failures are
  negative-cached for the `Retry-After` window.
- Credential read-modify-write runs under an exclusive advisory lock on
  `~/.claude/.credentials.json.lock`, which also guards against a second
  claudeops process or Claude Code rotating the token concurrently.

## Key decisions

| # | Decision |
|---|----------|
| 1 | `modernc.org/sqlite` (pure Go) over CGO driver — single-binary build |
| 2 | Collector embedded in the TUI process — no daemon to install or supervise |
| 3 | SQLite (WAL + `busy_timeout`) as the write serializer — no in-process queue to tune or drain |
| 4 | Task correlation at write-time, not query-time — O(1) reads |
| 5 | Permissive parsers, skip unknown line types — JSONL format drift resilience |
| 6 | Idempotent upsert keyed by event uuid — re-ingestion is safe and order-independent |
| 7 | OAuth refresh inside the usage client: sidecar lock + atomic temp+rename — credential safety on a file we share |
| 8 | `LineParser`/`Sink` seam so a new source is a parser, not a new pipeline |
| 9 | Bubbletea single-model dashboard — no premature router |
| 10 | `go test` table-driven with subtests |

## SQLite schema (DDL summary)

`projects(id, cwd UNIQUE, name)` → `sessions(id, project_id FK, first_seen, last_seen, source)` → `events(uuid PK, session_id FK, ts, type, model, in_tokens, out_tokens, cache_read_tokens, cache_create_tokens, cost_eur, task_id FK, source)`. Plus `tasks(id, name, started_at, ended_at, max_age_seconds)`, `file_offsets(path, offset, size)`, `source_watermarks(source, position)`, `config(key, value)`. WAL mode, foreign keys on. Indexes on `events(ts)`, `events(session_id)`, `events(task_id)`, `events(source)`.

Full DDL: `internal/store/schema.go`.

### Event idempotency

Events are keyed by `uuid`, which for Claude assistant events is the dedup key
`message.id[:requestId]` — Claude Code writes one line per content block of a
single API call, and they share input/cache counts while `output_tokens` grows
monotonically. The insert is therefore:

```sql
INSERT INTO events (...) VALUES (...)
ON CONFLICT(uuid) DO UPDATE SET
    in_tokens = excluded.in_tokens, out_tokens = excluded.out_tokens,
    cache_read_tokens = excluded.cache_read_tokens,
    cache_create_tokens = excluded.cache_create_tokens,
    cost_eur = excluded.cost_eur
  WHERE excluded.out_tokens > events.out_tokens
```

The row with the largest `out_tokens` (and its consistently computed cost) wins.
A duplicate that cannot change the row or the session bounds is detected before
the transaction opens, so it produces no WAL frame.

## Where things live at runtime

| Path | Owner | Purpose |
|---|---|---|
| `~/.claude/projects/*/` | Claude Code | source data — read only |
| `~/.codex/sessions/**` | Codex CLI | source data — read only (parent dir overridable with `CODEX_HOME`) |
| `~/.local/share/opencode/opencode.db` | opencode | source data — read only |
| `~/.claude/.credentials.json` | Claude Code (shared) | OAuth tokens — locked, atomic refresh only |
| `~/.claude/.credentials.json.lock` | claudeops | advisory lock sidecar |
| `~/.claude/settings.json` | Claude Code (shared) | claudeops manages only its hook entries and OTel env vars |
| `~/.claudeops/claudeops.db` | claudeops | local store |
| `~/.claudeops/pricing.toml` | claudeops | editable price table |
| `~/.claudeops/config.toml` | claudeops | widgets, thresholds, tab visibility, usage TTL, export, sources |
| `~/.claudeops/providers.toml` | claudeops | optional user-defined quota providers |
| `~/.claudeops/current-task.json` | claudeops | sidecar for task tracking |
| `~/.claudeops/live/` | claudeops | hook-written live session sidecars |

# Design: ClaudeOps MVP

## Technical Approach

Single Go binary `claudeops`. The TUI process owns one collector goroutine, one store, one usage client, and one Bubbletea program. All concurrency is fan-in to the store via a single buffered channel — the store is the only writer. Reads (TUI aggregates, CLI subcommands) hit SQLite directly with separate `*sql.DB` handles in WAL mode.

## Architecture Decisions

| # | Choice | Alternatives | Rationale |
|---|--------|--------------|-----------|
| 1 | `modernc.org/sqlite` (pure Go) | `mattn/go-sqlite3` (CGO) | No C toolchain, single-binary build, cross-compile friendly. Perf gap is irrelevant at our scale. |
| 2 | Embedded collector (no daemon) | Separate `collector run` process | Smallest MVP surface. Daemon mode is the next change in Fase 1. |
| 3 | Single-writer store + buffered channel | Mutex-guarded shared store | Eliminates `database/busy` races and serializes batch boundaries cleanly. |
| 4 | Task correlation at write-time | At query-time | Dashboard reads stay O(1). `task_id` is a column on `events`, not a join. |
| 5 | Permissive parser (skip unknown types) | Strict schema validation | JSONL format drifts between Claude Code releases. Resilience > correctness on noise. |
| 6 | OAuth refresh inside the usage client | External daemon | Self-contained; no extra moving parts. flock + atomic rename keeps `~/.claude/.credentials.json` safe. |
| 7 | Bubbletea single-model dashboard | Multi-view router | MVP. Routing comes when there's >1 view to route between. |
| 8 | `go test` + table-driven tests, `teatest` for TUI | testify everywhere | Stdlib first. testify only when struct equality is painful. |

## Data Flow

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

## Package Layout

```
cmd/claudeops/main.go            # subcommand router (default → TUI)
internal/
  parser/    parser.go events.go parser_test.go
  collector/ collector.go watcher.go offsets.go collector_test.go
  store/     store.go schema.go queries.go migrations.go store_test.go
  pricing/   pricing.go pricing_test.go
  usage/     client.go credentials.go refresh.go client_test.go
  tasks/     tasks.go correlate.go tasks_test.go
  tui/       model.go view.go update.go tui_test.go
  config/    config.go paths.go
configs/pricing.toml             # embedded via go:embed, copied on first run
```

## SQLite Schema (DDL)

```sql
CREATE TABLE projects (
  id INTEGER PRIMARY KEY, cwd TEXT UNIQUE NOT NULL, name TEXT NOT NULL
);
CREATE TABLE sessions (
  id TEXT PRIMARY KEY, project_id INTEGER NOT NULL REFERENCES projects(id),
  first_seen TEXT NOT NULL, last_seen TEXT NOT NULL
);
CREATE TABLE events (
  uuid TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id),
  ts TEXT NOT NULL, type TEXT NOT NULL, model TEXT,
  in_tokens INTEGER, out_tokens INTEGER,
  cache_read_tokens INTEGER, cache_create_tokens INTEGER,
  cost_eur REAL, task_id TEXT REFERENCES tasks(id)
);
CREATE INDEX idx_events_ts ON events(ts);
CREATE INDEX idx_events_session ON events(session_id);
CREATE INDEX idx_events_task ON events(task_id);
CREATE TABLE tasks (
  id TEXT PRIMARY KEY, name TEXT NOT NULL,
  started_at TEXT NOT NULL, ended_at TEXT, max_age_seconds INTEGER NOT NULL
);
CREATE TABLE file_offsets (path TEXT PRIMARY KEY, offset INTEGER NOT NULL, size INTEGER NOT NULL);
CREATE TABLE config (key TEXT PRIMARY KEY, value TEXT NOT NULL);
PRAGMA journal_mode=WAL;
```

## Key Interfaces

```go
package parser
type Event interface{ Kind() string; Timestamp() time.Time; SessionID() string }
type AssistantEvent struct {
    UUID, Session, Model string
    TS time.Time
    InputTokens, OutputTokens, CacheReadTokens, CacheCreateTokens int
}
func ParseLine(b []byte) (Event, error)

package store
type Store interface {
    Insert(ctx context.Context, ev parser.Event, costEUR *float64, taskID *string) error
    SaveOffset(path string, off, size int64) error
    LoadOffsets() (map[string]int64, error)
    AggregatesForToday(ctx context.Context) (Aggregates, error)
    TopSessionsByCost(ctx context.Context, n int) ([]SessionAgg, error)
    Close() error
}

package usage
type Snapshot struct{ FiveHour, SevenDay, SevenDayOpus Bucket }
type Bucket struct{ Utilization float64; ResetsAt time.Time }
type Client interface{ Get(ctx context.Context) (Snapshot, error) }

package tasks
type Tracker interface {
    Start(name string) (Task, error)
    Stop() error
    Current() (*Task, bool)
    Resolve(sessionID string, ts time.Time) (taskID *string)
}
```

## Concurrency Model

- **Goroutines**: (1) `collector.Watch` — fsnotify loop; (2) `collector.Tail(file)` — one per active file, reads new bytes on event or 250ms tick; (3) `store.writer` — drains `ingestCh` (buffered 1024), batches inserts every 100ms or 200 events; (4) `tui.tickCmd` — Bubbletea 2s refresh.
- **Single writer rule**: only `store.writer` calls `INSERT`. Tail goroutines push to `ingestCh`. Backpressure: channel send is blocking, so a slow store throttles tails — acceptable.
- **Shutdown**: `context.Cancel` propagates; `store.writer` flushes remaining batch then closes the DB.

## OAuth Refresh Sequence

```
usage.Get():
  1. read credentials (flock shared)
  2. if expiresAt < now()+30s → refresh()
  3. GET /api/oauth/usage with bearer
  4. on 401 → refresh() once and retry
  5. cache result 60s

refresh():
  1. flock exclusive ~/.claude/.credentials.json
  2. POST refresh_token to console.anthropic.com/v1/oauth/token
  3. write temp file mode 0600 → fsync → os.Rename → release flock
  4. on any error: leave original untouched, return ErrAuthExpired
```

## Task Correlation Algorithm

```
on each event ev:
  cur := tasksTracker.Current()
  if cur == nil: taskID = nil
  else if ev.TS > cur.StartedAt + cur.MaxAge:
    tasksTracker.Stop()  // auto-expire
    taskID = nil
  else:
    taskID = &cur.ID
```

Resolution is O(1) per event because the tracker holds the current task in memory; only `start`/`stop` touch the sidecar JSON file (and emit a tasks-row write).

## Testing Strategy

| Layer | What | How |
|---|---|---|
| Unit | parser, pricing, tasks resolver, OAuth credential atomic write | table-driven `go test`, fixtures in `testdata/` |
| Integration | collector → store end-to-end, schema migrations, aggregate queries | tmpfs SQLite + synthetic JSONL files, fsnotify driven |
| TUI | Bubbletea model snapshot | `teatest` (charmbracelet/x/exp/teatest) for golden output |
| Network | usage client (refresh + 401 retry + degrade) | `httptest.Server` |
| CLI | `task start/stop/list` | exec the binary in tmp HOME |

`go test -race ./...` MUST be clean.

## Migration / Rollout

No migration. Greenfield. First run creates `~/.claudeops/{claudeops.db, pricing.toml}`.

## Open Questions

- [ ] None — all decisions are resolved by exploration + verified `/api/oauth/usage` shape.

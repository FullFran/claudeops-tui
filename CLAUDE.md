# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

```bash
# Build binary (pure Go, no CGO required)
CGO_ENABLED=0 go build -o claudeops ./cmd/claudeops

# Install from source
go install github.com/fullfran/claudeops-tui/cmd/claudeops@latest
```

## Testing

Strict TDD is enabled for this project. Tests are table-driven with subtests.

```bash
go test ./...                              # All tests
go test -run TestName ./internal/package   # Single test
go test -cover ./...                       # With coverage
go test -race ./...                        # Race detector (must be clean)
```

Test patterns: temp databases via `t.TempDir()`, helper constructors like `newTestModel(t)` with `t.Cleanup()`, Bubbletea tests send `tea.KeyMsg`/`tea.WindowSizeMsg` and inspect model state.

## Linting

```bash
gofmt -w .
go vet ./...
golangci-lint run
```

## Architecture

Entry point: `cmd/claudeops/main.go` — subcommand router with variable function pointers (testable). Default command launches the TUI.

### Data Flow

```
~/.claude/projects/*.jsonl
  → collector (fsnotify + per-file tail with offset persistence)
  → parser.ParseLine() → typed events (permissive, soft-fails on unknown types)
  → pricing.Calculate() → cost in EUR
  → tasks.Resolve() → optional task attribution
  → store.Insert() via buffered channel (cap 1024, single-writer goroutine)
  → SQLite (WAL mode)
  → TUI reads aggregates on 2s tick + usage.Get() for Anthropic subscription %
```

### Key Packages

- **parser** — JSONL line decoder. Permissive: unknown event types are logged once and skipped, never error. Supports format drift.
- **collector** — fsnotify watcher + per-file tail goroutines. `IngestExisting()` for warm start (uses stored byte offsets). `Watch()` for live.
- **store** — SQLite with single-writer discipline. Only the writer goroutine calls INSERT. `INSERT ... ON CONFLICT(uuid) DO NOTHING` for idempotency. `Open()` for read-write, `OpenReadOnly()` for MCP server.
- **pricing** — TOML-based per-model token pricing. Embedded seed in `configs/pricing.toml`, copied to `~/.claudeops/` on first run.
- **usage** — OAuth client for `/api/oauth/usage`. Atomic credential read/write with flock on `~/.claude/.credentials.json`. Retry-After backoff on refresh. Falls back to cached data on failure.
- **tasks** — Sidecar `current-task.json` for task attribution. `Resolve(sessionID, ts)` correlates events to active task.
- **tui** — Bubbletea multi-tab dashboard (7 tabs). Single Model struct with view modes (Normal → DayBrowse → DayDetail → SessionBrowse → SessionDetail). Viewport-based scrolling. Help overlay and task input modal.
- **mcpserver** — MCP protocol handler over stdio. 7 tools. Uses read-only store.
- **config** — TOML settings in `~/.claudeops/config.toml`. `Paths` struct handles data dir creation.

### Concurrency Model

- Collector goroutine: fsnotify loop spawns per-file tail goroutines
- Store writer goroutine: drains buffered `ingestCh`, batches inserts
- Bubbletea loop: `tea.NewProgram()` with tick-based refresh
- Channel-based backpressure between collector and store

### Bubbletea Conventions

- Tab enum: `TabDashboard`, `TabSessions`, etc.
- View modes: `viewNormal`, `viewDayBrowse`, etc.
- Message types: suffix `Msg` (`refreshMsg`, `dayDetailMsg`)
- Commands: `refreshCmd()`, `loadDayDetailCmd()` — async operations return results as messages
- Style objects: suffix `Style` (`titleStyle`, `headerStyle`)

## Commits

Conventional commits only: `type(scope): subject`. Types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`. Link issues: `(#N)`. Atomic — one logical change per commit. No AI attribution.

## File Locations at Runtime

- `~/.claudeops/claudeops.db` — SQLite database (WAL mode)
- `~/.claudeops/pricing.toml` — User-editable pricing config
- `~/.claudeops/config.toml` — TUI settings
- `~/.claudeops/current-task.json` — Active task sidecar
- `~/.claude/.credentials.json` — OAuth tokens (shared with Claude Code)

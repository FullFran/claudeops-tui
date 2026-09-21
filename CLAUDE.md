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
make fmt          # gofmt -w on tracked Go files
make fmt-check    # fails when a tracked file needs formatting
make vet          # go vet ./...
make lint         # golangci-lint run (advisory, not enforced in CI)
make ci           # fmt-check + vet + build + race, then advisory lint
```

`make ci` mirrors the CI workflow. `make lint` is golangci-lint only — the old
gofmt+vet behavior now lives in `make fmt-check` and `make vet`.

## Architecture

Entry point: `cmd/claudeops/main.go` — subcommand router with variable function pointers (testable). Default command launches the TUI. Subcommands: `task`, `ingest`, `reingest`, `update`, `hooks`, `push`, `otel-config`, `statusline`, `agy`, `mcp`, `version`, `help`.

### Data Flow

```
~/.claude/projects/*.jsonl            (claude)
$CODEX_HOME/sessions/**/*.jsonl       (codex, default ~/.codex/sessions)
$XDG_DATA_HOME/opencode/opencode.db   (opencode, default ~/.local/share)
~/.gemini/antigravity-cli/conversations/*.db  (agy, one SQLite per conversation)
  → collector (fsnotify watcher + 500ms debounce tick, byte offsets in SQLite)
    or opencode.Ingester (5s SQLite poll with a persisted watermark)
    or agy.Ingester (5s poll fanned out across every conversation file, a
                      per-conversation watermark, protobuf usage decoding)
  → source.LineParser (parser.ClaudeLineParser / codex.Parser) → []source.Record
  → source.StoreSink.Emit: pricing.Calculator.CostForCacheTTL() → cost in EUR,
                           overridden by Record.ReportedCostUSD when positive
                           tasks.Resolve()     → optional task attribution
  → store.Insert() (direct call, no queue) → SQLite (WAL)
  → TUI reads aggregates on a 2s tick + usage.Get() for Anthropic subscription %
    + provider.Registry for other services' quotas + live.Sessions() for Classroom
```

### Key Packages

- **parser** — Claude JSONL line decoder. Permissive: unknown event types decode to `UnknownEvent` and the adapter drops them, never errors. Supports format drift.
- **codex** — Codex rollout (`rollout-*.jsonl`) parser. Token deltas come from `last_token_usage`, falling back to subtracting cumulative `total_token_usage`. `CodexRoot()` resolves `$CODEX_HOME/sessions` (default `~/.codex/sessions`).
- **opencode** — poller for opencode's SQLite database. Not a Collector: it polls every 5s and persists a watermark in `source_watermarks`. `DefaultDBPath()` honors `$XDG_DATA_HOME`. Reads go through `mode=ro`, except when both WAL sidecars are absent — then `immutable=1`, because a plain read-only open recreates `-wal`/`-shm` inside opencode's data directory and fails outright when that directory is not writable. A poll that keeps failing is reported by `supervisePollErrors`; `Watch` itself never returns, so nothing else would notice.
- **agy** — poller for the Google Antigravity CLI, which keeps one SQLite database per conversation under `~/.gemini/antigravity-cli/conversations` (`DefaultRoot()`) instead of opencode's single shared DB, so a poll fans out across every conversation file and tracks a watermark per conversation (`source_watermarks` key `agy:<conversation-uuid>`), one file's failure never blocking the rest. Usage lives in a protobuf blob in `gen_metadata.data`, decoded by a small hand-written wire reader (`wire.go`, `decoder.go`) against field numbers pinned from a real agy 1.2.7 install — there is no public `.proto` and community docs describe an older, incompatible schema. A call's timestamp is resolved by joining its usage message id (or `last_step_index` kv) against the `steps` table; an unresolved row in a file written in the last two minutes is deferred to the next poll instead of guessed. `NormalizeModel` strips agy's `-exp-*` and reasoning-effort suffixes so calls price at the base model's rate. Also owns the status-line quota snapshot (`quota.go`) and agy's `settings.json` editing (`settings.go`), both consumed by `cmd/claudeops/agy.go`.
- **source** — the ingestion seam: `LineParser`, `LineContext`, `Record`, `Sink`. `StoreSink` applies pricing and task attribution, then calls `store.Insert`. `Record.ReportedCostUSD` carries what a source says it was actually billed; a positive value replaces the table-derived estimate, a zero does not (opencode records 0 for calls a subscription already covered, and taking that literally would erase the equivalent-value figure the dashboard is built on).
- **collector** — fsnotify watcher over one source root. `IngestExisting()` for warm start (resumes from stored byte offsets); `Watch()` ingests existing data, then runs a single event loop that marks files dirty and flushes them on a 500ms ticker. There is no per-file tail goroutine.
- **store** — SQLite (`modernc.org/sqlite`, WAL, `busy_timeout(5000)`). Events upsert with `ON CONFLICT(uuid) DO UPDATE ... WHERE excluded.out_tokens > events.out_tokens`, so re-ingestion is idempotent and the row carrying the final streaming output count wins. `Open()` for read-write, `OpenReadOnly()` for the MCP server. `ResetIngestedData()` backs `claudeops reingest`.
- **pricing** — TOML-based per-model token pricing. Embedded seed in `internal/pricing/pricing.seed.toml` (via `go:embed`), copied to `~/.claudeops/pricing.toml` on first run; missing seed models are merged into existing installs without overwriting user-customized values.
- **usage** — OAuth client for `/api/oauth/usage`. The load→refresh→save sequence runs under an exclusive advisory lock on a sidecar `~/.claude/.credentials.json.lock` file (the credentials file itself is replaced by rename, which would drop a lock taken on its inode). Writes are temp file + fsync + rename with mode 0600, and unknown `claudeAiOauth` fields are round-tripped verbatim. `expiresAt` is read in whatever unit the file uses (Claude Code writes milliseconds) and written back in the same unit. Concurrent `Get()` calls are single-flighted; 429/5xx and transport failures are negative-cached with `Retry-After` backoff.
- **provider** — pluggable live-quota adapters beyond Anthropic (Codex, Copilot, Gemini, Antigravity) plus user-defined HTTP providers from `~/.claudeops/providers.toml`. A provider whose credentials are absent is skipped silently. `Antigravity` is the exception: it has no credential or live endpoint of its own — it just reads the quota snapshot `claudeops agy statusline` last wrote to `~/.claudeops/antigravity-quota.json`, dropping any bucket whose reset time has already passed.
- **live** — discovers active Claude Code sessions by scanning `~/.claude/projects` mtimes, overlaid with hook-written sidecars in `~/.claudeops/live`. Feeds the Classroom tab.
- **hooks** — installs and removes claudeops entries for the `SessionStart`, `UserPromptSubmit`, `Stop` and `SessionEnd` events in `~/.claude/settings.json`, and handles those events on stdin.
- **export** — OTLP metric push (`claudeops push`) and management of the Claude Code OTel env vars in `~/.claude/settings.json` (`claudeops otel-config`).
- **insights** — 5 computed insights: cache efficiency, model mix, cost trend, session efficiency, peak hours.
- **tasks** — Sidecar `current-task.json` for task attribution. `Resolve(_ string, ts)` attributes an event to whichever task was active at `ts`; the session id parameter is accepted for call-site symmetry but ignored.
- **tui** — Bubbletea multi-tab dashboard (8 tabs). Single Model struct with view modes (Normal → DayBrowse → DayDetail → SessionBrowse → SessionDetail). Viewport-based scrolling. Help overlay, task input modal, and inline settings editing.
- **mcpserver** — MCP protocol handler over stdio. 7 tools. Uses read-only store.
- **config** — TOML settings in `~/.claudeops/config.toml`. `Paths` resolves every file claudeops reads or writes and creates the data dir.
- **update** — self-update path behind `claudeops update`.

### Concurrency Model

- `cmdTUI` starts one goroutine per enabled line-based source collector (claude, codex), plus two each for the opencode and agy pollers: one running `Watch`, one watching `ConsecutiveFailures`. `Watch` never returns on a failing poll, so the first cannot see one.
- Each collector runs a single fsnotify loop; dirty files are re-read sequentially on a 500ms ticker. There is no per-file tail goroutine.
- **All of those goroutines call `store.Insert` directly.** There is no ingest channel and no dedicated writer goroutine. Write serialization is SQLite's: WAL plus `busy_timeout(5000)`. Keep inserts short and never hold a transaction across I/O.
- Bubbletea owns its own runtime and ticks every 2s (`tea.Tick`); async work is dispatched as commands that return `Msg` values.
- `usage.Client` serializes its whole fetch/refresh path behind a mutex, so N ticks cause at most one refresh.

### Bubbletea Conventions

- Tab enum: `TabDashboard`, `TabSessions`, `TabProjects`, `TabModels`, `TabTasks`, `TabInsights`, `TabClassroom`, `TabSettings`.
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
- `~/.claudeops/providers.toml` — Optional user-defined quota providers
- `~/.claudeops/current-task.json` — Active task sidecar
- `~/.claudeops/live/` — Hook-written live session sidecars (Classroom tab)
- `~/.claudeops/antigravity-quota.json` — Antigravity (agy) quota snapshot, written by `claudeops agy statusline`, mode 0600
- `~/.claude/projects/*.jsonl` — Claude Code source data (read-only)
- `~/.gemini/antigravity-cli/conversations/*.db` — agy source data, one SQLite per conversation (read-only)
- `~/.gemini/antigravity-cli/conversation_summaries.db` — agy's conversation-to-workspace map, used for project attribution (read-only)
- `~/.gemini/antigravity-cli/settings.json` — agy settings; `claudeops agy setup|remove` edits only its `statusLine` key
- `~/.claude/.credentials.json` — OAuth tokens (shared with Claude Code); `.credentials.json.lock` is our advisory lock sidecar
- `~/.claude/settings.json` — Claude Code settings; claudeops manages its hook entries and OTel env vars here

## Specs

`openspec/changes/claudeops-mvp/` is the original MVP change and is **stale**:
`tasks.md` shows 0 of 59 tasks checked while the binary is 0.7.0, and nothing
has been archived. Treat the code as the source of truth; do not cite those
artifacts as current behavior.

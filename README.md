# claudeops-tui

Local TUI to track Claude Code usage, costs, and tasks. Single binary, no daemon, no SaaS.

Shows **real** subscription % from Anthropic's `/api/oauth/usage` endpoint — not estimates against guessed plan limits like every other tool out there.

## What it does

- Parses `~/.claude/projects/*.jsonl` incrementally (fsnotify + persisted offsets)
- Stores events in local SQLite (`modernc.org/sqlite`, no CGO)
- Computes per-event cost in € using a **four-class** token breakdown (input, output, cache_read, cache_create) — collapsing them ruins the math
- Calls Anthropic's undocumented `GET /api/oauth/usage` for the real session/weekly/per-model usage that Claude Code's own `/usage` command uses, with OAuth token refresh
- Tracks tasks via `claudeops task start "name"` and attributes events to them by `(sessionId, timestamp window)`
- Renders one consolidated Bubbletea dashboard

## Install

```bash
go install github.com/fullfran/claudeops-tui/cmd/claudeops@latest
```

### Update

Same command — `@latest` always pulls the newest version:

```bash
go install github.com/fullfran/claudeops-tui/cmd/claudeops@latest
```

If `claudeops` is still "command not found" after installation, your Go bin directory is probably not on `PATH` yet:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

Add that line to your shell config (`~/.bashrc`, `~/.zshrc`, etc.), reload the shell, and verify:

```bash
which claudeops
claudeops version
```

Or build locally:

```bash
git clone https://github.com/fullfran/claudeops-tui
cd claudeops-tui
CGO_ENABLED=0 go build -o claudeops ./cmd/claudeops
./claudeops
```

## Usage

```bash
claudeops                       # launch the TUI dashboard (default)
claudeops task start "refactor parser"
claudeops task stop
claudeops task list
claudeops ingest                # one-shot ingest of existing JSONL
claudeops version
```

### Keyboard shortcuts

Press `?` inside the TUI for the full keybinding reference. Highlights:

| Key | Action |
|-----|--------|
| `1`–`6` | switch tab (Dashboard, Sessions, Projects, Models, Tasks, Settings) |
| `enter` | browse daily breakdown (Dashboard) / drill into day detail |
| `j` / `k` | navigate lists (day browser, settings) |
| `space` | toggle setting (Settings tab) |
| `esc` | go back one level |
| `n` / `S` | new task / stop task |
| `r` | force refresh |
| `?` | help overlay |
| `q` | quit |

## Files

| Path | Purpose |
|---|---|
| `~/.claudeops/claudeops.db` | local SQLite store (WAL mode) |
| `~/.claudeops/pricing.toml` | editable price table (seed shipped, edit when Anthropic changes prices) |
| `~/.claudeops/config.toml` | dashboard widgets, thresholds, tab visibility (auto-created on first run) |
| `~/.claudeops/current-task.json` | sidecar for the active task |
| `~/.claude/projects/*.jsonl` | source data — read only |
| `~/.claude/.credentials.json` | OAuth tokens — read always, written only during token refresh, atomic + flock + 0600 |

## Status

MVP. See [`docs/plan.md`](./docs/plan.md) for what's IN, what's OUT, and the deferred Fase 2/3 work (daemon mode, alerts, multi-device sync).

## Caveats

- The `/api/oauth/usage` endpoint is **undocumented**. Anthropic can change or remove it without notice. ClaudeOps degrades gracefully ("subscription % unavailable") instead of faking numbers.
- Pricing in TOML goes stale when Anthropic updates prices. Edit `~/.claudeops/pricing.toml`.
- The collector lives inside the TUI process. If the TUI is closed, ingestion pauses. Daemon mode is the next change in Fase 1.

## Documentation

- [`docs/plan.md`](./docs/plan.md) — vision, scope, phases
- [`docs/architecture.md`](./docs/architecture.md) — package map, data flow, decisions
- [`docs/jsonl-format.md`](./docs/jsonl-format.md) — observed event shapes
- [`docs/oauth-usage-endpoint.md`](./docs/oauth-usage-endpoint.md) — endpoint reference
- [`docs/limitations.md`](./docs/limitations.md) — what's broken, fragile, or missing
- [`openspec/changes/claudeops-mvp/`](./openspec/changes/claudeops-mvp/) — full SDD artifacts (proposal, specs, design, tasks)

## License

MIT.

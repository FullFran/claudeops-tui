# claudeops-tui

Local TUI to track Claude Code usage, costs, and tasks. Single binary, no daemon, no SaaS.

Shows **real** subscription % from Anthropic's `/api/oauth/usage` endpoint — not estimates against guessed plan limits like every other tool out there.

## What it does

- Parses `~/.claude/projects/*.jsonl` incrementally (fsnotify + persisted offsets)
- Stores events in local SQLite (`modernc.org/sqlite`, no CGO)
- Computes per-event cost in € using a **four-class** token breakdown (input, output, cache_read, cache_create) — collapsing them ruins the math
- Calls Anthropic's undocumented `GET /api/oauth/usage` for the real session/weekly/per-model usage that Claude Code's own `/usage` command uses, with OAuth token refresh
- Tracks tasks via `claudeops task start "name"` and attributes events to them by `(sessionId, timestamp window)`
- **Session drill-down** — navigate into any session to see per-model costs, hourly activity, token breakdown with cache hit ratio, and duration
- **Daily drill-down** — browse daily aggregates with hourly charts and per-model breakdown
- Renders one consolidated Bubbletea dashboard with 7 tabs

## Architecture

```mermaid
graph LR
    A["~/.claude/projects/*.jsonl"] -->|fsnotify + offsets| B[Collector]
    B -->|parse + cost calc| C[(SQLite)]
    D["Anthropic /api/oauth/usage"] -->|OAuth + 5min cache| E[Usage Client]
    C --> F[TUI Dashboard]
    E --> F
    F --> G[Dashboard]
    F --> H[Sessions]
    F --> I[Projects]
    F --> J[Models]
    F --> K[Tasks]
    F --> L[Calendar]
    F --> M[Settings]
```

### Navigation flow

```mermaid
stateDiagram-v2
    [*] --> Normal : launch
    Normal --> DayBrowse : enter (Dashboard tab)
    DayBrowse --> DayDetail : enter on day
    DayDetail --> DayBrowse : esc
    DayBrowse --> Normal : esc

    Normal --> SessionBrowse : enter (Sessions tab)
    SessionBrowse --> SessionDetail : enter on session
    SessionDetail --> SessionBrowse : esc
    SessionBrowse --> Normal : esc

    Normal --> Normal : tab / 1-7 (switch tabs)
```

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
| `1`–`7` | switch tab (Dashboard, Sessions, Projects, Models, Tasks, Calendar, Settings) |
| `enter` | browse daily breakdown (Dashboard) / browse sessions (Sessions) / drill into detail |
| `j` / `k` | navigate lists (day browser, session browser, settings) |
| `space` | toggle setting (Settings tab) |
| `esc` | go back one level (detail → browse → tab) |
| `n` / `S` | new task / stop task |
| `r` | force refresh |
| `?` | help overlay |
| `q` | quit |

### Session drill-down

From the **Sessions** tab, press `enter` to open the session browser. Use `j`/`k` to navigate sessions — a preview card shows cost, events, tokens, and duration. Press `enter` again to see the full detail view:

- **Per-model cost breakdown** with percentage of total
- **Hourly activity chart** showing when cost was incurred
- **Token breakdown** — input, output, cache read, cache create
- **Cache hit ratio** — how effectively the session used prompt caching
- **Duration** — first to last event timestamps

## Files

| Path | Purpose |
|---|---|
| `~/.claudeops/claudeops.db` | local SQLite store (WAL mode) |
| `~/.claudeops/pricing.toml` | editable price table (seed shipped, edit when Anthropic changes prices) |
| `~/.claudeops/config.toml` | dashboard widgets, thresholds, tab visibility, usage polling interval (auto-created on first run) |
| `~/.claudeops/current-task.json` | sidecar for the active task |
| `~/.claude/projects/*.jsonl` | source data — read only |
| `~/.claude/.credentials.json` | OAuth tokens — read always, written only during token refresh, atomic + flock + 0600 |

## Configuration

`~/.claudeops/config.toml` is auto-created on first run. Key sections:

```toml
[dashboard]
show_subscription = true    # toggle subscription % widget
show_burn_rate = true       # hourly burn rate
show_cache_hit_ratio = true # cache efficiency

[dashboard.thresholds]
daily_warn_eur = 20         # yellow threshold
daily_alert_eur = 50        # red threshold

[usage]
cache_ttl_seconds = 300     # how often to poll Anthropic's usage endpoint (default: 5min)

[tabs]
sessions = true             # toggle entire tabs on/off
calendar = true
```

## Roadmap

See [epic #9](https://github.com/FullFran/claudeops-tui/issues/9) for the work pattern analysis roadmap:

```mermaid
graph TD
    P1["Phase 1: Session Drill-Down ✅"]
    P2["Phase 2: Aggregate Insights"]
    P3["Phase 3: MCP Server"]
    P4["Phase 4: Active Logging"]

    P1 --> P2
    P2 --> P3
    P3 --> P4

    style P1 fill:#2d6a4f,color:#fff
```

| Phase | Status | What |
|-------|--------|------|
| **1. Session Drill-Down** | Done | Navigate into sessions, see per-model costs, hourly charts, cache ratios |
| **2. Aggregate Insights** | Planned | Cache efficiency scores, model mix analysis, cost trends |
| **3. MCP Server** | Planned | Expose data via MCP so Claude can analyze your usage conversationally |
| **4. Active Logging** | Planned | Intent tagging, outcome tracking, tool usage patterns |

See also [`docs/plan.md`](./docs/plan.md) for the original scope and deferred Fase 2/3 work (daemon mode, alerts, multi-device sync).

## Status

MVP with interactive drill-downs.

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

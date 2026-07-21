# Plan — ClaudeOps TUI

## Vision

Devs using Claude Code (Pro/Max) lack a unified, **honest** view of usage:
- Real subscription % (5h, 7d, 7d-opus) — not estimated
- Cost in € per session, project, and task
- Which task burned which tokens — task-level attribution that no other tool does today

ClaudeOps is local-first, single-binary, no SaaS.

## Why

1. The community tools (Maciek-roboblog, ccusage, statusline scripts) **estimate** quotas against hardcoded plan limits and show token counts, not real subscription %.
2. Anthropic exposes the real data via an undocumented OAuth endpoint (`GET /api/oauth/usage`) that the official Claude Code `/usage` command already uses. We can call it too.
3. Nobody correlates usage to *tasks*. That's the differentiator.

## Shipped (as of 0.7.0)

- JSONL ingestion for Claude Code (parser + collector + persisted byte offsets)
- Codex rollout ingestion and an opencode SQLite poller, auto-detected when present
- SQLite store (modernc, no CGO), WAL, idempotent upsert keyed by event uuid
- Pricing in editable TOML, 4-class token cost, embedded seed + `make update-pricing`
- OAuth usage client with locked atomic credential refresh and graceful degrade
- Live quota for other services (Codex, Copilot, Gemini, user-defined HTTP providers)
- Task tracking + time-window attribution
- Bubbletea dashboard with 8 tabs, day/session drill-downs, and inline settings editing
- Computed insights (cache efficiency, model mix, cost trend, session efficiency, peak hours)
- Classroom: live view of running Claude Code sessions, backed by Claude Code hooks
- MCP server over stdio with 7 read-only tools
- OTLP metric push and management of Claude Code's own OTel env vars
- CLI: `task`, `ingest`, `reingest`, `update`, `hooks`, `push`, `otel-config`, `mcp`, `version`

## Not shipped

- **Daemon mode** (`claudeops collector run` + systemd unit + PID file). Live ingestion still requires the TUI to be open; `claudeops ingest` from cron is the workaround.
- **Threshold alerts.** Daily spend is color-coded against `daily_warn_eur` / `daily_alert_eur`, but nothing notifies you.
- **Multi-device sync** and per-device hostname/user attribution.
- **Auto-learning of the weekly cap** when rate-limited.
- **Calendar tab.** The config keys survive but have no effect; the 7th tab is Classroom.
- Fase 3 ideas: € per feature shipped, tokens per PR, GitHub PR ↔ task integration, optional web dashboard.

## Stack

- Go 1.25
- `bubbletea`, `bubbles`, `lipgloss` (Charm)
- `modernc.org/sqlite` (pure Go, no CGO)
- `fsnotify`
- `BurntSushi/toml`
- `mark3labs/mcp-go`

Module path: `github.com/fullfran/claudeops-tui`. Binary: `claudeops`.

## Spec status

`openspec/changes/claudeops-mvp/` holds the original MVP change (proposal,
exploration, design, six capability specs, tasks). It was **never updated or
archived**: `tasks.md` still shows 0 of 59 tasks checked, and everything after
the MVP — multi-source ingestion, MCP, insights, hooks, export, providers — has
no spec at all. There is no authoritative current spec in this repository. The
code is the source of truth; read those artifacts as history only.

# Plan — ClaudeOps TUI

## Vision

Devs using Claude Code (Pro/Max) lack a unified, **honest** view of usage:
- Real subscription % (5h, 7d, 7d-opus) — not estimated
- Cost in € per session, project, and task
- Which task burned which tokens — task-level attribution that no other tool does today

ClaudeOps is local-first, single-binary, no SaaS. For 1 dev now. For 3 devs in Fase 2.

## Why now

1. The community tools (Maciek-roboblog, ccusage, statusline scripts) **estimate** quotas against hardcoded plan limits and show token counts, not real subscription %.
2. Anthropic exposes the real data via an undocumented OAuth endpoint (`GET /api/oauth/usage`) that the official Claude Code `/usage` command already uses. We can call it too.
3. Nobody correlates usage to *tasks*. That's the differentiator.

## Phases

### Fase 1 — MVP (this change: `claudeops-mvp`)

Single binary. TUI process embeds collector goroutine. SQLite local. Real OAuth usage. Editable pricing TOML. Task tracking via sidecar JSON. One dashboard view.

**In scope** (locked):
- JSONL ingestion (parser + collector + offsets)
- SQLite store (modernc, no CGO)
- Pricing in editable TOML, 4-class token cost
- OAuth usage client + auto-refresh + graceful degrade
- Task tracking + correlation
- Bubbletea single-view dashboard
- `claudeops task start|stop|list` CLI
- `docs/` and `openspec/` artifacts

**Out of scope** (deferred):
- Daemon mode (`collector run`) — next change in Fase 1
- Burn-rate prediction, alerts at thresholds — Fase 2
- Multi-device sync — Fase 2
- Auto-learning quota when rate-limited — Fase 2

### Fase 2 — Daemon, alerts, multi-device

- `claudeops collector run` as systemd user unit; TUI auto-detects via PID file
- Burn-rate calculation + threshold alerts (70/85/95)
- Sync to a shared Postgres for the 3-dev team (manual `claudeops sync push/pull`)
- Auto-learning of weekly cap when rate-limited
- Per-device hostname/user attribution

### Fase 3 — Differential

- € per feature shipped, tokens per PR, IA efficiency score
- GitHub integration (PR ↔ task)
- Optional web dashboard

## Stack (locked)

- Go 1.22+
- `bubbletea`, `bubbles`, `lipgloss` (Charm)
- `modernc.org/sqlite` (pure Go, no CGO)
- `fsnotify`
- `BurntSushi/toml`

Module path: `github.com/fullfran/claudeops-tui`. Binary: `claudeops`.

## Status

| Phase | Status |
|---|---|
| Exploration | ✅ Done |
| Proposal | ✅ Done |
| Specs (6 capabilities) | ✅ Done |
| Design | ✅ Done |
| Tasks | ✅ Done |
| Implementation | ⏳ Pending user go-ahead |
| Verification | — |
| Archive | — |

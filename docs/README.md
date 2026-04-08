# ClaudeOps TUI — Documentation

Local TUI to track Claude Code usage, costs, and tasks. Greenfield Go + Bubbletea project.

## Index

- [plan.md](./plan.md) — vision, phases, MVP scope, what's IN and OUT
- [architecture.md](./architecture.md) — package layout, data flow, key decisions
- [jsonl-format.md](./jsonl-format.md) — observed `~/.claude/projects/*.jsonl` event shapes
- [oauth-usage-endpoint.md](./oauth-usage-endpoint.md) — undocumented `/api/oauth/usage` reference
- [limitations.md](./limitations.md) — known gaps, risks, fragile dependencies

## Authoritative SDD artifacts

The single source of truth for the change in flight lives in OpenSpec format under [`openspec/changes/claudeops-mvp/`](../openspec/changes/claudeops-mvp/):

- `proposal.md` — intent, scope, rollback
- `specs/<capability>/spec.md` — Given/When/Then requirements (6 capabilities)
- `design.md` — technical design with diagrams and DDL
- `tasks.md` — TDD task checklist

This `docs/` folder is the **human-friendly** view. If `docs/` and `openspec/` ever disagree, **OpenSpec wins**.

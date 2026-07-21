# ClaudeOps TUI — Documentation

Local TUI to track Claude Code, Codex and opencode usage, costs, and tasks.

## Index

- [architecture.md](./architecture.md) — package map, data flow, concurrency, schema
- [upgrading.md](./upgrading.md) — behavior changes that need action from you
- [providers.md](./providers.md) — built-in and user-defined live quota providers
- [jsonl-format.md](./jsonl-format.md) — observed `~/.claude/projects/*.jsonl` event shapes
- [oauth-usage-endpoint.md](./oauth-usage-endpoint.md) — undocumented `/api/oauth/usage` reference
- [limitations.md](./limitations.md) — known gaps, risks, fragile dependencies
- [plan.md](./plan.md) — original vision, what shipped, what did not

## On specs

[`openspec/changes/claudeops-mvp/`](../openspec/changes/claudeops-mvp/) contains
the original MVP change. It has not been maintained: `tasks.md` shows 0 of 59
tasks checked against a 0.7.0 binary, nothing is archived, and no capability
added after the MVP is specified there.

**There is no authoritative current spec in this repository.** When this folder
and `openspec/` disagree, neither wins automatically — check the code.

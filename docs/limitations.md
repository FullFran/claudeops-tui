# Known Limitations & Risks

Honest list of where ClaudeOps falls short or depends on fragile foundations. Read before using.

## MVP gaps (by design — deferred to later phases)

| Gap | Why | Resolved in |
|---|---|---|
| Data only ingested while TUI is open | MVP embeds collector in TUI process; no daemon | Next change in Fase 1 (`claudeops collector run`) |
| No burn-rate prediction or threshold alerts | Out of MVP scope | Fase 2 |
| No multi-device sync | Out of MVP scope | Fase 2 |
| Single dashboard view | No router yet, MVP only needs one screen | Fase 2+ |
| No per-device hostname/user attribution | Single-dev local-only MVP | Fase 2 (when sync arrives) |

## Fragile dependencies

### `/api/oauth/usage` is undocumented

Anthropic can rename, change, or remove this endpoint without notice. **Mitigation**: feature flag `usage.enabled`, graceful degradation to "subscription % unavailable" with a clear message — never fake numbers. We will NOT fall back to estimation against guessed quotas (that's what every other tool does and it's why they mislead).

### JSONL format drift

Claude Code is on a fast release train. Event shapes change between minor versions. **Mitigation**: permissive parser (unknown event types are skipped, never crash); supported version range surfaced in the dashboard footer with a one-line warning when exceeded.

### Pricing table goes stale

Prices live in `~/.claudeops/pricing.toml`, seeded by us. When Anthropic changes prices, your numbers go wrong silently. **Mitigation**: dashboard footer shows `pricing updated: <date>`; the seed file documents the source URL in comments. Edit the TOML to refresh.

### Shared credentials file

`~/.claude/.credentials.json` is owned by Claude Code. ClaudeOps reads it always and writes it during OAuth refresh. **Mitigation**: exclusive `flock` + atomic temp+rename + mode 0600 preserved + abort on partial parse. The file is either the old version or the new version, never truncated.

## Task correlation false positives

If you `claudeops task start "X"` and forget to `task stop`, every event in any session you touch within `max_age` (default 4h) gets attributed to task X — even unrelated work. **Mitigation**: max-age cap (4h default, configurable), explicit auto-stop when expired, dashboard hint when a task has been active too long.

## Concurrency / SQLite

When daemon mode lands (next change), two writers could briefly coexist. **Mitigation**: WAL mode from day one + single-writer discipline enforced inside the store package + a PID file convention so the TUI defers to the daemon when one is running.

## What ClaudeOps is NOT

- **Not a SaaS.** No telemetry leaves your machine. No backend to host. No auth to manage.
- **Not a billing tool for Anthropic.** € numbers are your local estimate against an editable price table. The authoritative bill is in your Anthropic console.
- **Not a quota enforcer.** Rate limiting happens at Anthropic. ClaudeOps shows you what you've used; it does not throttle your requests.
- **Not a Claude Code replacement.** It's a sidecar observer.

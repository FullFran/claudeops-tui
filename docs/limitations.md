# Known Limitations & Risks

Honest list of where ClaudeOps falls short or depends on fragile foundations. Read before using.

## Gaps

| Gap | Why |
|---|---|
| Live ingestion only happens while the TUI is open | The collector runs inside the TUI process; there is no daemon. Run `claudeops ingest` (e.g. from cron) to catch up without opening the dashboard — offsets are persisted, so nothing is double-counted. |
| No threshold alerts | The dashboard color-codes daily spend against `daily_warn_eur` / `daily_alert_eur`, but nothing notifies you |
| No multi-device sync | Everything is local to one machine; there is no per-device hostname or user attribution |
| Burn rate is descriptive, not predictive | The widget reports cost/hour over the last 4h; it does not forecast quota exhaustion |
| Calendar tab never shipped | `[tabs] calendar` and the `[calendar]` config section are still written to `config.toml` but have no effect. The 7th tab is Classroom. |

## Fragile dependencies

### `/api/oauth/usage` is undocumented

Anthropic can rename, change, or remove this endpoint without notice.
**Mitigation**: graceful degradation to "subscription % unavailable" with a
clear message — never fake numbers. We will NOT fall back to estimation against
guessed quotas (that's what every other tool does and it's why they mislead).
There is no on/off config flag: the dashboard widget is toggled with
`[dashboard] show_subscription`, and the poll interval with
`[usage] cache_ttl_seconds` (default 300).

### JSONL format drift

Claude Code and Codex are on fast release trains, and event shapes change
between minor versions. **Mitigation**: permissive parsers — an unrecognized
line type is counted and skipped, never fatal. `claudeops ingest` reports
`unknown` and `parse_errors` counts so drift is visible instead of silent.

### opencode schema drift

The opencode source reads that tool's SQLite database directly. A schema change
there breaks ingestion for that source only; the poller surfaces the failure and
keeps its watermark so nothing is lost once the parser is fixed.

### Pricing table goes stale

Prices live in `~/.claudeops/pricing.toml`, seeded by us. When Anthropic changes
prices, your numbers go wrong silently. **Mitigation**: the dashboard footer
shows `pricing updated: <date>`; unknown models leave `cost_eur` NULL and raise
a one-time warning in the status line rather than guessing a price. Edit the
TOML to refresh, or run `make update-pricing` in a checkout to refresh the
embedded LiteLLM snapshot.

### Shared credentials file

`~/.claude/.credentials.json` is owned by Claude Code. ClaudeOps reads it always
and writes it during OAuth refresh. **Mitigation**: the whole read-modify-write
runs under an exclusive advisory lock (on the sidecar
`~/.claude/.credentials.json.lock`, because saving replaces the file by rename),
then writes temp file + fsync + rename with mode 0600. Unknown `claudeAiOauth`
fields are preserved verbatim. The file is either the old version or the new
version, never truncated.

## Task correlation is time-based, not session-based

`Resolve` ignores the session id: while a task is active, **every** event
ingested within its `max_age` window is attributed to it, no matter which
session, project, or tool produced it. If you `claudeops task start "X"` and
forget to `task stop`, unrelated work lands on task X. **Mitigation**: a max-age
cap (4h by default, changeable via `max_age_seconds` in
`~/.claudeops/current-task.json`) after which the task auto-stops on the next
resolve.

## Concurrency / SQLite

Several goroutines (one per source, plus the opencode poller) call
`store.Insert` directly — there is no queue and no single writer goroutine.
Serialization is SQLite's: WAL mode plus `busy_timeout(5000)`. That holds within
one process and across processes on a local filesystem. Two claudeops instances
on the same database will contend rather than corrupt, but running the TUI and a
long `reingest` at the same time is asking for `SQLITE_BUSY`. Don't. The MCP
server opens the database read-only, so it is safe to run alongside the TUI.

## What ClaudeOps is NOT

- **Not a SaaS.** Nothing leaves your machine unless you explicitly configure `[export]` to push metrics to your own OTLP endpoint.
- **Not a billing tool for Anthropic.** € numbers are your local estimate against an editable price table. The authoritative bill is in your Anthropic console.
- **Not a quota enforcer.** Rate limiting happens at Anthropic. ClaudeOps shows you what you've used; it does not throttle your requests.
- **Not a Claude Code replacement.** It's a sidecar observer.

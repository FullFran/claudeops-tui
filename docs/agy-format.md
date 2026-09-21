# agy (Google Antigravity CLI) Data Format Reference

Notes from inspecting real conversation databases on agy `1.2.7`, cross-checked
against the [documented status-line payload](https://antigravity.google/docs/cli/statusline).
There is no public schema for the SQLite tables or the protobuf blobs inside
them — **treat this as observed, not authoritative**, and see
[Drift warning](#drift-warning) below before trusting an unfamiliar field.

Implementation: `internal/agy` (`wire.go`, `decoder.go`, `ingester.go`).

## File location

```
~/.gemini/antigravity-cli/conversations/<conversation-uuid>.db   # one SQLite file per conversation
~/.gemini/antigravity-cli/conversation_summaries.db               # conversation → workspace map
```

Unlike opencode's single shared database, agy writes one SQLite file per
conversation. `internal/agy.Ingester` lists every `<uuid>.db` under
`conversations/`, keeps a poll watermark per conversation (`source_watermarks`
key `agy:<conversation-uuid>`), and reads each file `mode=ro` — falling back to
`immutable=1` when neither `-wal` nor `-shm` sidecar exists, the same rule
opencode's poller uses and for the same reason: a plain read-only open would
otherwise try to create those sidecars itself.

## Tables used

| Table | Columns used | Role |
|---|---|---|
| `gen_metadata` | `idx`, `data` | one row per model call; `data` is the protobuf blob decoded below |
| `steps` | `idx`, `metadata` | timestamps, joined to `gen_metadata` rows (see [Timestamp resolution](#timestamp-resolution)) |
| `conversation_summaries.db` → `conversation_summaries` | `conversation_id`, `workspace_uris` | conversation → project attribution |

`gen_metadata` also has a `size` column and `steps` has `step_type`,
`status`, and others; a `trajectory_meta` table also exists. None of these
are read — they carry nothing this package needs.

## `gen_metadata.data` (protobuf)

No `.proto` file ships with agy, so `internal/agy/wire.go` parses the wire
format generically (field number + wire type + raw payload) and
`internal/agy/decoder.go` pins the field numbers below as the single source of
truth. Field numbers verified against agy 1.2.7:

| Path | Type | Meaning |
|---|---|---|
| `1` | message | the generation message (everything else nests inside it) |
| `1.19` | string | raw model id, e.g. `gemini-3.8-flash-exp-a` |
| `1.4` | message | usage submessage (see below) |
| `1.20` | repeated `{1: key, 2: value}` | key/value pairs; only `last_step_index` is read |

Usage submessage (`1.4` here, and also reachable from a step at `9`, see
below):

| Field | Meaning |
|---|---|
| `2` | uncached input tokens |
| `3` | output tokens **total** — already the sum of thinking + response (fields `9` + `10`); the split order between them is unverified, and this package does not need it |
| `4` | cache-write tokens — inferred from the field's position, never observed populated in real data |
| `5` | cache-read tokens |
| `7` | message id, `"bot-<uuid>"` — the join key to `steps.metadata` |
| `11` | response id — decoded by nothing; unused |

`Call.LastStepIndex` (from kv key `last_step_index`) is the decimal step index
this call's usage was attributed to, used as the timestamp fallback below.

A row whose top-level field `1` or its usage field `1.4` is absent decodes as
"no usage message" (`ok=false`, no error) and is skipped — most rows in a
conversation are not model calls. A row that fails to parse as protobuf at all
is a malformed row: skipped, warned once, `parse_errors` incremented, and the
rest of the file is still read.

## Timestamp resolution

`steps.metadata` is also a protobuf blob, decoded by the same generic parser:

| Field | Meaning |
|---|---|
| `1` | `Timestamp` submessage: `1` = seconds, `2` = nanos |
| `9` | the same usage submessage shape as `gen_metadata`'s `1.4` — read only for its message id (`9.7`) |

For each `gen_metadata` row, `internal/agy.resolveTimestamp` tries, in order:

1. The `steps` row whose usage message id (`9.7`) equals the call's message id
   (`1.4.7`).
2. The `steps` row named by the call's `last_step_index` kv.
3. Unresolved.

An unresolved row in a conversation file whose mtime is inside the last two
minutes is assumed to still be mid-write, so it is deferred to the next poll
rather than dated wrong. Past that window it falls back to the file's mtime,
with a one-time warning per conversation.

## Project attribution

`conversation_summaries.db`'s `workspace_uris` column is not pinned to one
shape by the verified schema — it may hold a JSON array, a single URI, or a
comma/newline-separated list — so `firstFileURIPath` tries all three and takes
the first `file://` URI it finds, URL-decoded to a filesystem path. A
conversation with no resolvable workspace falls back to a synthetic
`agy:<conversation-id>` cwd, exactly like opencode's Ingester does for a
session with no directory.

## Model normalization

`internal/agy.NormalizeModel` strips two agy-specific decorations before
pricing looks the model up (`internal/pricing`'s own normalizer already
strips cross-vendor ones like `-thinking`, and touching it would affect every
other source):

| Suffix | Example | Stripped to |
|---|---|---|
| `-exp` or `-exp-<alnum>` | `gemini-3.8-flash-exp-a` | `gemini-3.8-flash` |
| `-high` / `-medium` / `-low` / `-minimal` (reasoning effort) | `gemini-3.1-pro-high` | `gemini-3.1-pro` |

Both are stripped repeatedly to a fixed point, so a suffix nested inside an
exp tag (`foo-exp-high`) also resolves. Pricing an `-exp` variant or a
specific reasoning-effort call at its base model's rate is an equivalent-value
inference — agy publishes no separate price for either.

As of this writing, the bare forms resolve in `pricing.toml`
(`gemini-3.8-flash`, `gemini-3.1-pro`, `claude-sonnet-4-6`, `gpt-oss-120b`);
`gemini-3.8-flash-exp-a` itself does not, which is exactly why the suffix is
stripped before the lookup.

## Status-line quota payload

Separate from the conversation databases: agy pushes quota data to whatever
command its `settings.json` names as `statusLine`, documented at
<https://antigravity.google/docs/cli/statusline>. `claudeops agy statusline`
reads this from stdin. Only two fields are decoded — `plan_tier` and `quota`
(a map of bucket name → `{remaining_fraction, reset_time}`) — everything else
the payload carries (`cwd`, `email`, `session_id`, `transcript_path`, `model`,
`context_window`, `product`, …) is deliberately left unparsed, so none of it
can reach the persisted snapshot even by accident. See
[`docs/providers.md`](./providers.md) for how that snapshot is served.

## What is not read

- Transcripts (`brain/<id>/.system_generated/logs/transcript*.jsonl`) — no
  model or token data lives there.
- `trajectory_meta` and every `steps` column besides `idx`/`metadata`.
- Antigravity IDE data under `~/.gemini/antigravity/` (as distinct from the
  CLI's `~/.gemini/antigravity-cli/`) — its format is unverified and out of
  scope.
- Conversation content of any kind. Only usage metadata, model id, timestamps,
  and the workspace path are read; message text is never touched.

## Drift warning

This mapping was verified against a real agy **1.2.7** install on
**2026-09-21**, using local conversation data plus the primary status-line
documentation linked above. Community documentation of an older schema
(tokscale issue #1184) describes different field numbers for usage and
timestamps (`1+2` for input, time at `chatModel.#9.#4`) — that layout is
**not** what this package implements, and using it would misdecode current
data. If a future agy release changes the wire format, expect `parse_errors`
to rise in `claudeops ingest` output; that is the signal to re-verify this
document against a fresh install before trusting it again.

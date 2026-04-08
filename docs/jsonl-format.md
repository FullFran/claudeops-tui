# Claude Code JSONL Format Reference

Notes from inspecting real session files on Claude Code `2.1.96` under `~/.claude/projects/`. Treat this as **observed**, not authoritative — Anthropic does not document this format.

## File location

```
~/.claude/projects/<cwd-slug>/<session-uuid>.jsonl
```

`<cwd-slug>` is the absolute working directory with `/` replaced by `-`, e.g. `/home/franblakia/fullfran/ClaudeOps-TUI` → `-home-franblakia-fullfran-ClaudeOps-TUI`.

Files are append-only. One JSON object per line. Lines may be very long (full assistant messages).

## Event types observed

| `type` | Notes | Use |
|---|---|---|
| `permission-mode` | Header on session start | ignore |
| `file-history-snapshot` | Tracked file backups | ignore |
| `attachment` | deferred-tools, mcp-instructions, companion intro | ignore |
| `user` | User prompt; `message.content` is a string or block array; **no token usage** | track for prompt count; no cost |
| `assistant` | Assistant reply; **carries `message.usage`** with the token breakdown | **the gold** — every cost calculation comes from here |

The parser MUST be permissive: future Claude Code releases will introduce new types. Unknown types are returned as `UnknownEvent` and skipped, never crash.

## Universal fields

Every event line carries (at minimum):

- `type`
- `sessionId` (UUID, matches the filename)
- `cwd` (absolute path)
- `timestamp` (RFC3339 with millisecond precision, e.g. `2026-04-08T13:10:56.766Z`)
- `uuid` (event UUID)
- `parentUuid` (chain pointer, may be `null`)
- `userType` (e.g. `external`)
- `version` (Claude Code CLI version, e.g. `2.1.96`)

## Assistant event shape (the important one)

```json
{
  "type": "assistant",
  "sessionId": "6e6b9dd1-...",
  "cwd": "/home/franblakia/fullfran/ClaudeOps-TUI",
  "timestamp": "2026-04-08T13:10:57.123Z",
  "uuid": "...",
  "parentUuid": "...",
  "version": "2.1.96",
  "message": {
    "role": "assistant",
    "model": "claude-opus-4-6",
    "content": [ ... ],
    "usage": {
      "input_tokens": 5,
      "cache_creation_input_tokens": 20780,
      "cache_read_input_tokens": 15718,
      "output_tokens": 1101,
      "cache_creation": {
        "ephemeral_5m_input_tokens": 0,
        "ephemeral_1h_input_tokens": 20780
      },
      "server_tool_use": {
        "web_search_requests": 0,
        "web_fetch_requests": 0
      }
    }
  }
}
```

### The four token classes — DO NOT collapse them

| Class | Cost ratio (vs input) | Why |
|---|---|---|
| `input_tokens` | 1.0 | base price |
| `output_tokens` | ~5.0 | normal output rate |
| `cache_read_input_tokens` | ~0.10 | cache hit, very cheap |
| `cache_creation_input_tokens` | ~1.25 | cache write, slight premium |

Treating cache reads as full input → overcost by ~10×. Ignoring cache creation → undercost. The pricing module MUST split them per model.

## Models observed

- `claude-opus-4-6`
- `claude-sonnet-4-6`
- `claude-haiku-4-5-20251001`

Pricing seed in `configs/pricing.toml` ships with these. Unknown models leave `cost_eur` NULL and warn once.

# Anthropic OAuth Usage Endpoint (undocumented)

This is the endpoint Claude Code's `/usage` slash command uses internally. **Anthropic has not documented it publicly.** Use at your own risk — see "Caveats" below.

## Endpoint

```
GET https://api.anthropic.com/api/oauth/usage
```

## Headers

```
Authorization: Bearer <claudeAiOauth.accessToken from ~/.claude/.credentials.json>
anthropic-beta: oauth-2025-04-20
User-Agent: claudeops/<version>
```

## Response shape

```json
{
  "five_hour":      { "utilization": 6.0,  "resets_at": "2026-04-08T18:59:59Z" },
  "seven_day":      { "utilization": 35.0, "resets_at": "2026-04-14T16:59:59Z" },
  "seven_day_opus": { "utilization": 12.0, "resets_at": "2026-04-14T17:59:59Z" }
}
```

- `utilization`: float in `[0, 100]`, the % already consumed in the bucket
- `resets_at`: RFC3339 UTC timestamp when the bucket rolls over

The three buckets map directly to what Claude Code's `/usage` shows: current 5h session, weekly across all models, weekly per-model (Opus has its own cap on Max plans).

## Authentication

Tokens live in `~/.claude/.credentials.json` (mode 0600), written by Claude Code on `claude /login`:

```json
{
  "claudeAiOauth": {
    "accessToken":  "sk-ant-oat01-…",
    "refreshToken": "sk-ant-ort01-…",
    "expiresAt":    1759700000
  }
}
```

### Refresh flow

When `expiresAt` is in the past (or the API returns 401), refresh against:

```
POST https://console.anthropic.com/v1/oauth/token
Content-Type: application/json

{ "grant_type": "refresh_token", "refresh_token": "sk-ant-ort01-…" }
```

The response replaces `accessToken`, `refreshToken`, and `expiresAt`. Write back to the credentials file **atomically**:

1. Acquire exclusive `flock` on `~/.claude/.credentials.json`
2. Write new content to `~/.claude/.credentials.json.tmp` with mode 0600
3. `fsync`
4. `os.Rename` over the original
5. Release the lock

If anything fails mid-refresh, the original file is left untouched. Never write a partial file — Claude Code is reading the same file.

## Detection: API key vs OAuth

If `~/.claude/.credentials.json` lacks the `claudeAiOauth` block (the user is using `ANTHROPIC_API_KEY` env or a workspace key), the OAuth endpoint returns 401. Detect this **before** the call and return `ErrUsageUnavailable`. The dashboard shows `subscription % unavailable (API key mode)` — do not fake numbers.

## Caching

Cache the response for 60 seconds in-process. The dashboard refreshes every 2s; without caching we'd hammer the endpoint.

## Caveats

1. **Undocumented.** Anthropic can change or remove this endpoint without notice. Gate behind `usage.enabled` config flag and degrade gracefully.
2. **Pro/Max only.** Workspace API keys cannot use it.
3. **Shared credentials file.** Claude Code itself reads/writes the same file. Atomic writes are non-negotiable.
4. **Two open feature requests** ask Anthropic to expose this officially: `anthropics/claude-code#44328`, `anthropics/claude-code#32796`. Until then, this is reverse-engineering.

## Source attribution

Endpoint and response shape verified via community research (codelynx.dev statusline writeup, cross-referenced against the issues above and direct inspection of `~/.claude/.credentials.json` on a real install).

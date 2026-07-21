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
User-Agent: claudeops/0.1
```

(The `User-Agent` is a fixed client identifier, not the binary's version.)

## Response shape

```json
{
  "five_hour":        { "utilization": 6.0,  "resets_at": "2026-04-08T18:59:59Z" },
  "seven_day":        { "utilization": 35.0, "resets_at": "2026-04-14T16:59:59Z" },
  "seven_day_opus":   { "utilization": 12.0, "resets_at": "2026-04-14T17:59:59Z" },
  "seven_day_sonnet": { "utilization": 21.0, "resets_at": "2026-04-14T17:59:59Z" },
  "extra_usage": {
    "is_enabled": true,
    "monthly_limit": 100.0,
    "used_credits": 12.5,
    "utilization": 12.5
  }
}
```

- `utilization`: float in `[0, 100]`, the % already consumed in the bucket
- `resets_at`: RFC3339 UTC timestamp when the bucket rolls over
- **Any bucket may be `null`** for plans that do not have that quota — the
  per-model buckets in particular. Callers must nil-check every one.
- `extra_usage` describes the optional pay-as-you-go credit pool and is also
  optional.

The buckets map directly to what Claude Code's `/usage` shows: current 5h session, weekly across all models, weekly per-model (Opus has its own cap on Max plans).

## Authentication

Tokens live in `~/.claude/.credentials.json` (mode 0600), written by Claude Code on `claude /login`:

```json
{
  "claudeAiOauth": {
    "accessToken":  "sk-ant-oat01-…",
    "refreshToken": "sk-ant-ort01-…",
    "expiresAt":    1759700000000,
    "scopes":       ["user:inference", "user:profile"],
    "subscriptionType": "max"
  }
}
```

`expiresAt` is **epoch milliseconds** as written by current Claude Code (13
digits). Older claudeops builds wrote epoch seconds, so a reader must detect the
unit rather than assume one: values at or above `1e11` are milliseconds. Write
back in the same unit the file already used — handing Claude Code a value in the
wrong unit makes it treat the token as expired (seconds read as ms) or valid
forever (ms read as seconds).

Fields inside `claudeAiOauth` beyond the three above (`scopes`,
`subscriptionType`, `rateLimitTier`, …) belong to Claude Code. Round-trip them
verbatim; stripping them on refresh corrupts the file for its owner.

### Refresh flow

When `expiresAt` is in the past (or within a 30s skew, or the API returns 401), refresh against:

```
POST https://console.anthropic.com/v1/oauth/token
Content-Type: application/json
anthropic-beta: oauth-2025-04-20

{ "grant_type": "refresh_token", "refresh_token": "sk-ant-ort01-…" }
```

The response (`access_token`, `refresh_token`, `expires_in` seconds) replaces `accessToken`, `refreshToken`, and `expiresAt`. The whole load → refresh → save sequence must be serialized:

1. Acquire an exclusive advisory lock on the sidecar `~/.claude/.credentials.json.lock`
   — **not** on the credentials file itself, because step 5 replaces that file
   by rename and a lock held on the original inode would be silently dropped
2. Load the credentials and re-check expiry (another process may have rotated
   them while you waited for the lock)
3. Write new content to a temp file in the same directory, mode 0600
4. `fsync`
5. `os.Rename` over the original
6. Release the lock

If anything fails mid-refresh, the original file is left untouched. Never write a partial file — Claude Code is reading the same file.

Only HTTP 400/401/403 from the token endpoint mean the grant is really gone
(re-login required). Every other status is transient and must be backed off, not
reported as an expired login.

## Detection: API key vs OAuth

If `~/.claude/.credentials.json` lacks the `claudeAiOauth` block (the user is using `ANTHROPIC_API_KEY` env or a workspace key), the OAuth endpoint returns 401. Detect this **before** the call and return `ErrUsageUnavailable`. The dashboard shows `subscription % unavailable (API key mode)` — do not fake numbers.

## Caching and backoff

The response is cached in-process for `[usage] cache_ttl_seconds` from
`config.toml` — **300 seconds by default**. The dashboard refreshes every 2s;
without caching we would hammer an endpoint we share with Claude Code itself.
Concurrent calls are single-flighted, so a burst of ticks causes at most one
request.

On HTTP 429, any 5xx, or a transport failure, the error is negative-cached for
the `Retry-After` window (or 5 minutes when the header is absent) and every
caller in that window gets the same answer without touching the network.

## Caveats

1. **Undocumented.** Anthropic can change or remove this endpoint without notice. Degrade gracefully; never substitute an estimate.
2. **Pro/Max only.** Workspace API keys cannot use it.
3. **Shared credentials file.** Claude Code itself reads/writes the same file. Locking and atomic writes are non-negotiable.
4. **Two open feature requests** ask Anthropic to expose this officially: `anthropics/claude-code#44328`, `anthropics/claude-code#32796`. Until then, this is reverse-engineering.

## Source attribution

Endpoint and response shape verified via community research (codelynx.dev statusline writeup, cross-referenced against the issues above and direct inspection of `~/.claude/.credentials.json` on a real install).

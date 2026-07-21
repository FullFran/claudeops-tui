# Subscription providers

ClaudeOps tracks live subscription/quota usage for multiple AI coding services,
inspired by [CodexBar](https://github.com/steipete/CodexBar). Each provider is
detected automatically: if its credentials are present on disk, its usage shows
up under **Subscription usage** on the Dashboard; if not, it is skipped silently.

## Built-in providers

| Provider | Endpoint | Credential source |
| --- | --- | --- |
| **Claude** (Anthropic) | `api.anthropic.com/api/oauth/usage` | `~/.claude/.credentials.json` (OAuth) |
| **Codex** (ChatGPT) | `chatgpt.com/backend-api/wham/usage` | `$CODEX_HOME/auth.json` (default `~/.codex/auth.json`), **or** opencode's `openai` OAuth session |
| **Copilot** (GitHub) | `api.github.com/copilot_internal/user` | `~/.config/github-copilot/apps.json` |
| **Gemini** (Google) | `cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota` | `~/.gemini/oauth_creds.json`, **or** opencode's `google` OAuth session |

The Claude and Codex providers are verified end-to-end. Copilot and Gemini
follow the documented endpoints and are covered by fixture tests; validate them
against a live account before relying on the exact numbers.

Claude is not a registry provider: it is rendered by the dedicated
`internal/usage` client, with its own cache and refresh logic. Codex, Copilot,
Gemini and any custom provider go through `provider.Registry`.

Press `p` on the Dashboard to cycle the subscription focus (All → each provider
→ All) when more than one provider reports data.

## Polling, caching and backoff

The TUI ticks every 2s, so the registry caches aggressively:

- A successful snapshot is reused for **5 minutes**.
- A failing provider is skipped for **1 minute**, doubling per consecutive
  failure up to **15 minutes**, then retried.
- Failures are per provider: one broken endpoint renders its own error line and
  never blocks the others.

### Reusing an opencode session

If you authenticated a provider inside [opencode](https://github.com/sst/opencode)
rather than its native CLI, ClaudeOps reads that session from
`~/.local/share/opencode/auth.json` (honoring `$XDG_DATA_HOME`) as a fallback —
no second login required. Codex reuses the `openai` OAuth entry (with its
`accountId` sent as `chatgpt-account-id`), and Gemini reuses the `google` entry.
The native CLI credentials, when present, always take precedence.

## Custom providers — `~/.claudeops/providers.toml`

Any service that exposes usage/quota over an HTTP endpoint with a bearer token
or API key can be tracked without a code change. Drop a `providers.toml` file in
`~/.claudeops/` and declare each provider:

```toml
# OpenRouter — utilization derived from used / limit credits.
[[provider]]
name = "OpenRouter"
url = "https://openrouter.ai/api/v1/credits"
method = "GET"          # GET (default) or POST
auth_scheme = "Bearer"  # "Bearer" (default) or "token"
token_env = "OPENROUTER_API_KEY"   # read the token from this env var...
# token_file = "~/.openrouter/key" # ...or from a file (trimmed contents)

  [[provider.window]]
  label = "credits"
  used_path = "data.total_usage"     # numerator
  limit_path = "data.total_credits"  # denominator -> used/limit*100

# A provider that already reports a used-percent directly.
[[provider]]
name = "Acme"
url = "https://api.acme.example/usage"
token_env = "ACME_API_KEY"

  [[provider.window]]
  label = "monthly"
  util_path = "usage.percent_used"   # a value already in [0,100]
  reset_path = "usage.reset_at"      # optional RFC3339 reset time

# A provider that reports a remaining fraction in [0,1].
[[provider]]
name = "Widget"
url = "https://widget.example/v1/quota"
method = "POST"
body = "{}"
token_env = "WIDGET_TOKEN"

  [[provider.window]]
  label = "weekly"
  remain_path = "quota.remaining_fraction"  # (1 - x) * 100
```

### Field reference

Each `[[provider]]` block accepts:

- `name` — display label.
- `url` — the usage endpoint.
- `method` — `GET` (default) or `POST`.
- `auth_scheme` — `Bearer` (default) or `token` (GitHub-style).
- `token_env` — environment variable holding the token/API key (checked first).
- `token_file` — file whose trimmed contents are the token (fallback). `~` is expanded.
- `body` — optional request body for `POST`.
- `note` — optional static line shown under the windows.

Each `[[provider.window]]` maps response fields to one quota bar. Utilization
(0–100) is resolved from the first strategy present, in this order:

1. `util_path` — a value already in `[0,100]`.
2. `remain_path` — a remaining fraction in `[0,1]`; utilization is `(1 - x) * 100`.
3. `used_path` + `limit_path` — utilization is `used / limit * 100`.

`reset_path` is optional and parsed as RFC3339. Dot-paths index into arrays by
number, e.g. `data.0.balance`.

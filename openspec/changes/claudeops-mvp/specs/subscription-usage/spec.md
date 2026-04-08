# Delta for subscription-usage

## ADDED Requirements

### Requirement: Real-data usage retrieval via OAuth endpoint

The system MUST fetch subscription usage from `GET https://api.anthropic.com/api/oauth/usage` using the OAuth access token from `~/.claude/.credentials.json` and the header `anthropic-beta: oauth-2025-04-20`.

#### Scenario: Successful fetch
- GIVEN a valid OAuth access token in `~/.claude/.credentials.json`
- WHEN the usage client calls the endpoint
- THEN it returns `{five_hour, seven_day, seven_day_opus}` each with `utilization` (0–100) and `resets_at` (RFC3339)

#### Scenario: Response cached
- GIVEN a successful response was received less than 60 seconds ago
- WHEN the dashboard requests usage again
- THEN the cached response is returned without an HTTP call

### Requirement: Automatic OAuth token refresh

When the access token is expired or returns 401, the client MUST refresh it via the refresh token and write the new credentials atomically.

#### Scenario: Token expired locally
- GIVEN `expiresAt` in the credentials file is in the past
- WHEN the usage client is invoked
- THEN it POSTs to the refresh endpoint with the refresh token, receives new credentials, writes them via temp file + rename preserving mode 0600, and retries the original request

#### Scenario: Refresh fails
- GIVEN the refresh token has been revoked
- WHEN refresh is attempted
- THEN no file is written, the client returns a typed `ErrAuthExpired`, and the dashboard shows "run `claude /login` to re-auth"

### Requirement: Graceful degradation when OAuth not present

The system MUST detect API-key-only credentials and SHALL NOT call the OAuth endpoint in that case.

#### Scenario: User authenticates with ANTHROPIC_API_KEY
- GIVEN `~/.claude/.credentials.json` lacks `claudeAiOauth` or only an `ANTHROPIC_API_KEY` env var is set
- WHEN the usage client is invoked
- THEN it returns `ErrUsageUnavailable` and the dashboard shows "subscription % unavailable (API key mode)" instead of fake numbers

### Requirement: Credential file safety

The client MUST NOT write a partial credentials file under any error condition and MUST hold an exclusive flock during read-modify-write.

#### Scenario: Refresh interrupted mid-write
- GIVEN the process is killed while refreshing credentials
- WHEN the user inspects `~/.claude/.credentials.json`
- THEN the file is either the old version or the new version, never a truncated one

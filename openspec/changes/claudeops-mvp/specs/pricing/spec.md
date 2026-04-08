# Delta for pricing

## ADDED Requirements

### Requirement: Editable pricing TOML

Prices MUST be loaded from `~/.claudeops/pricing.toml` and the file MUST be human-editable without recompilation.

#### Scenario: First run seeds pricing
- GIVEN no `pricing.toml` exists
- WHEN claudeops starts
- THEN the seed file shipped with the binary is copied to `~/.claudeops/pricing.toml` with mode 0644 and an `updated:` date

#### Scenario: User edits pricing
- GIVEN the user edits a price in `pricing.toml`
- WHEN claudeops next reads pricing (next dashboard refresh)
- THEN the new price applies to all subsequent cost calculations

### Requirement: Four-class token cost calculation

The calculator MUST compute cost using four distinct token classes per model: `input`, `output`, `cache_read`, `cache_create`. Treating cache reads as full input or ignoring cache creation MUST NOT happen.

#### Scenario: Mixed-class assistant message
- GIVEN an assistant event with input=5, cache_read=15718, cache_create=20780, output=1101 on model `claude-opus-4-6`
- WHEN cost is computed
- THEN the result equals `5*input_price + 15718*cache_read_price + 20780*cache_create_price + 1101*output_price` for that model

#### Scenario: Unknown model
- GIVEN an event for a model not present in `pricing.toml`
- WHEN cost is computed
- THEN the event is stored with `cost_eur = NULL` and a one-time warning is logged listing the missing model

### Requirement: Pricing freshness surfaced in TUI

The dashboard footer MUST display the `updated:` date from `pricing.toml`.

#### Scenario: Stale pricing visible to user
- GIVEN `pricing.toml` was last updated 90 days ago
- WHEN the dashboard renders
- THEN the footer shows `pricing updated: <date>` so the user knows to refresh

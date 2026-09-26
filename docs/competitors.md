# Competitor research

This is a primary-source comparison of tools adjacent to ClaudeOps: local usage
readers, quota/status utilities, agent-session observability, and hosted
observability platforms. It is a capability map, not an adoption or pricing
ranking. Sources were checked on **2026-09-22**; upstream capabilities and URLs
can change.

## Comparison matrix

| Product | Deployment and privacy | Agent coverage stated by the source | Differentiator | Limitation versus ClaudeOps | Primary source |
| --- | --- | --- | --- | --- | --- |
| **ccusage** | Local CLI/package; reads local agent data. | Claude Code plus Codex, OpenCode, Amp, Droid, Gemini CLI, Antigravity, and other listed CLIs. | Broad source-focused daily, session, block, model, JSON, and cost reports. | Reports are CLI/report oriented; the source does not describe ClaudeOps' single Go TUI, MCP server, task attribution, or its live provider registry. | [GitHub](https://github.com/ccusage/ccusage) |
| **OpenUsage** | Local-first Go dashboard with optional integrations; stores local data and offers a daemon. | Claude Code, Codex, Cursor, Copilot, Gemini CLI, Antigravity, OpenCode, and many other providers. | Large cross-provider coverage, tmux/status-line integrations, background collection, exports, and Prometheus metrics. | Broader provider surface is not the same as ClaudeOps' narrow, source-specific evidence model; setup and privacy tradeoffs vary by integration. | [GitHub](https://github.com/janekbaraniewski/openusage) |
| **New Relic Preflight** | Local-first by default; optional New Relic cloud mode for team rollups and alerts. | Claude Code, Copilot, Codex, Gemini CLI, Antigravity, Cursor, and other adapters; coverage is not uniform. | Agent actions, efficiency scores, anti-patterns, coaching, and New Relic dashboards. | More operational/behavioral observability than ClaudeOps' subscription-quota truth; cloud mode introduces an external telemetry destination. | [GitHub](https://github.com/newrelic-experimental/preflight) |
| **CodexBar** | Native macOS menu-bar app, with Linux desktop/CLI integrations; reuses provider sessions and local files. | Many providers including Codex, Claude, Cursor, Gemini, Copilot, Antigravity, and API services. | Polished multi-provider menu-bar status, reset countdowns, provider health, and optional spend scans. | Primarily a desktop quota/status surface; it does not present ClaudeOps' Go/Bubbletea workflow, local task attribution, or MCP analytics. | [GitHub](https://github.com/steipete/CodexBar) |
| **Agent Trail** | Local dashboard; npm or Docker; indexes local JSONL/SQLite into local SQLite. | Claude Code, Codex, OpenCode, OpenClaw, and Qoder. | Full session replay, tool inputs/outputs, subagent trees, and local-session search for agents. | Focuses on replay and session history rather than live subscription-provider quota and Antigravity status-line snapshots. | [GitHub](https://github.com/camtrik/agent-trail) |
| **AI Observer** | Self-hosted single binary; local DuckDB, dashboard, file watcher, or OTLP ingestion. | Claude Code, Gemini CLI, Codex CLI, GitHub Copilot, and OpenCode; file and OTLP coverage differs. | OpenTelemetry-native traces, logs, metrics, DuckDB analytics, and Parquet export. | Requires OTLP setup for some sources and does not claim ClaudeOps' direct Antigravity status-line provider or Anthropic subscription client. | [GitHub](https://github.com/tobilg/ai-observer) |
| **Langfuse Claude Code integration** | Langfuse Cloud or self-hosted; sends hook-captured traces to Langfuse when enabled. | Claude Code, with Langfuse documenting patterns for other coding agents separately. | Conversation, tool, timing, session, and token tracing in an AI engineering platform. | Opt-in hook plus credentials and a remote/self-hosted tracing system; not a local quota dashboard or multi-provider subscription reader. | [Integration guide](https://langfuse.com/integrations/developer-tools/claude-code) |
| **Datadog Agent Observability** | Hosted Datadog platform with local/browser experimentation entry points and SDK/OTel instrumentation. | Source lists Anthropic, OpenAI, Gemini, Vertex, Bedrock, and multiple agent frameworks; it is not a local CLI coverage list. | End-to-end traces, experiments, evaluations, annotations, governance, and correlation with application infrastructure. | Enterprise observability is a different deployment and data boundary from ClaudeOps' local-first TUI; it does not document ClaudeOps' account-specific quota adapters. | [Product page](https://www.datadoghq.com/product/ai/llm-observability) |
| **Anthropic Claude Code Usage API** | Anthropic organization API; access requires organization credentials and is not a local desktop tool. | Claude Code usage for organization actors, including subscription/API distinctions in the report. | Daily aggregated usage, model token breakdown, estimated cost, sessions, commits, PRs, tool actions, and pagination. | Organization-level reporting is not the local per-event ingestion, TUI, task window, or provider-agnostic quota view ClaudeOps provides. | [API reference](https://platform.claude.com/docs/en/api/admin/usage_report/retrieve_claude_code) |

### How to read the matrix

“Limitation versus ClaudeOps” means a gap relative to ClaudeOps' current shape,
not a claim that the product is inferior overall. The products use different
sources and trust boundaries. A local parser can expose richer per-session
detail than a hosted usage API, while a hosted platform can provide team
governance that a local binary intentionally does not.

## Five ClaudeOps improvements to prioritize

The first two are **parity** work: valuable capabilities that primary sources
show users expect elsewhere. The last three are **defensible differentiation**:
they build on ClaudeOps' existing local evidence and conservative quota posture.

1. **Parity — richer session replay.** Add an optional, explicitly redacted
   session detail view for tool calls, assistant turns, and subagent relationships,
   while preserving the current token and cache breakdown.
2. **Parity — background/headless collection.** Add a safe way to keep ingestion
   running when the TUI is closed, with explicit retention and source controls.
3. **Differentiation — evidence-labelled quota history.** Store timestamped
   snapshots with `VERIFIED`, `OBSERVED IN CLAUDEOPS`, and `UNKNOWN` provenance,
   including reset times and stale-state detection, without pretending to know
   provider accounting internals.
4. **Differentiation — cross-provider diagnostic correlation.** Correlate local
   agent calls, cache-token classes, task windows, status-line quota snapshots,
   and provider errors in one explainable timeline rather than collapsing them
   into a single spend number.
5. **Differentiation — privacy-preserving diagnostics.** Make JSON diagnostics,
   MCP responses, and exports redactable by default, document exactly which local
   paths are read, and provide bounded evidence bundles that exclude prompts,
   credentials, and raw transcripts.

These are improvement hypotheses, not commitments or claims that the listed
competitors implement every related feature. They should be validated against
user demand and source stability before implementation.

## Freshness and research limitations

- This matrix is a snapshot of public primary sources checked on 2026-09-22.
- Repository READMEs describe advertised capabilities, not independent accuracy,
  uptime, adoption, or security audits.
- No adoption, pricing, or market-share claims are made here.
- “Agent coverage” records what the cited source lists; integrations can have
  different capture depth, setup requirements, and platform support.
- Hosted products may change plans, retention, pricing, supported frameworks, or
  data processing terms without this repository changing.

## Primary sources

- [ClaudeOps](https://github.com/FullFran/claudeops-tui)
- [ccusage](https://github.com/ccusage/ccusage)
- [OpenUsage](https://github.com/janekbaraniewski/openusage)
- [New Relic Preflight](https://github.com/newrelic-experimental/preflight)
- [CodexBar](https://github.com/steipete/CodexBar)
- [Agent Trail](https://github.com/camtrik/agent-trail)
- [AI Observer](https://github.com/tobilg/ai-observer)
- [Langfuse Claude Code integration](https://langfuse.com/integrations/developer-tools/claude-code)
- [Datadog Agent Observability](https://www.datadoghq.com/product/ai/llm-observability)
- [Anthropic Claude Code Usage API](https://platform.claude.com/docs/en/api/admin/usage_report/retrieve_claude_code)

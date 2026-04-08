# Delta for jsonl-ingestion

## ADDED Requirements

### Requirement: Incremental session log tailing

The system MUST tail every Claude Code session JSONL file under `~/.claude/projects/` and emit each new line as a typed event without re-reading bytes already processed.

#### Scenario: Cold start with existing files
- GIVEN `~/.claude/projects/` contains N session files with M total lines and no persisted offsets
- WHEN the collector starts
- THEN every line is parsed once and an offset equal to file size is persisted per file

#### Scenario: Warm start after restart
- GIVEN persisted offsets exist for every known file
- WHEN the collector starts
- THEN only bytes after the persisted offset are read and the offset advances atomically per batch

#### Scenario: New session file appears
- GIVEN the collector is running
- WHEN a new `<uuid>.jsonl` is created in any project subdirectory
- THEN the watcher detects it within 1 second and begins tailing from offset 0

### Requirement: Permissive event classification

The parser MUST classify lines into known typed events and SHALL NOT crash on unknown event types.

#### Scenario: Known assistant event
- GIVEN a line with `"type":"assistant"` containing `message.usage`
- WHEN the parser decodes it
- THEN it returns an `AssistantEvent` with input/output/cache_read/cache_create token counts and model name

#### Scenario: Unknown event type
- GIVEN a line with a `type` not in the known set
- WHEN the parser decodes it
- THEN it returns an `UnknownEvent` carrying `type` and raw bytes, the collector logs once at debug level, and ingestion continues

#### Scenario: Malformed JSON line
- GIVEN a line that fails JSON parsing
- WHEN the parser decodes it
- THEN it returns a parse error, the collector increments a counter and skips the line, and the offset advances past it

### Requirement: CLI version awareness

The parser SHOULD record the Claude Code `version` field when present and surface a warning when the version exceeds a known-supported range.

#### Scenario: Supported version
- GIVEN events carry `version: "2.1.96"` and the supported range is `>=2.1.0,<2.2.0`
- WHEN ingested
- THEN no warning is emitted

#### Scenario: Unsupported version
- GIVEN events carry `version: "2.3.0"`
- WHEN ingested
- THEN the dashboard footer displays a one-line warning indicating the format may have drifted

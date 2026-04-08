# Delta for usage-store

## ADDED Requirements

### Requirement: Local SQLite persistence without CGO

The store MUST persist all ingested data in a local SQLite database using a pure-Go driver and MUST run with no C toolchain installed.

#### Scenario: Fresh install
- GIVEN no database file exists at `~/.claudeops/claudeops.db`
- WHEN the store opens
- THEN the file is created, schema migrations run to head, and WAL mode is enabled

#### Scenario: Existing database
- GIVEN the database exists at the current schema version
- WHEN the store opens
- THEN no migrations run and the store is immediately usable

### Requirement: Schema covers events, sessions, projects, tasks, offsets, config

The schema MUST include tables for `events`, `sessions`, `projects`, `tasks`, `task_events`, `file_offsets`, and `config`, with foreign keys enforced.

#### Scenario: Insert event resolves session and project
- GIVEN an `AssistantEvent` for a new `sessionId` in a new `cwd`
- WHEN the store writes it
- THEN rows are upserted into `projects` (by cwd), `sessions` (by sessionId, fk project), and `events` (fk session) inside one transaction

### Requirement: Single-writer discipline

The store MUST allow only one writer per process and MUST use transactions for multi-row writes.

#### Scenario: Concurrent reads during write
- GIVEN one writer goroutine is committing a batch
- WHEN multiple reader goroutines query aggregates
- THEN reads succeed without blocking writes (WAL) and never observe a partial batch

### Requirement: Aggregation queries for the dashboard

The store MUST expose aggregate queries for: today's total cost €, top N sessions by cost, top N projects by cost, and current task totals — each returning in under 50ms on a database with 100k events.

#### Scenario: Dashboard refresh
- GIVEN 100k events ranging over 30 days
- WHEN the dashboard requests today's aggregates
- THEN every aggregate query completes in under 50ms

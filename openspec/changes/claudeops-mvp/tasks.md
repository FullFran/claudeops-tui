# Tasks: ClaudeOps MVP

> Strict TDD: every GREEN task is preceded by a RED task. Run `go test -race ./...` after each phase.

## Phase 0: Bootstrap

- [ ] 0.1 `go mod init github.com/fullfran/claudeops-tui` (Go 1.22+)
- [ ] 0.2 Add deps: bubbletea, bubbles, lipgloss, modernc.org/sqlite, fsnotify, BurntSushi/toml
- [ ] 0.3 Create package skeletons: `cmd/claudeops`, `internal/{parser,collector,store,pricing,usage,tasks,tui,config}`, `configs/`
- [ ] 0.4 `internal/config/paths.go`: resolve `~/.claudeops/`, `~/.claude/`, ensure dirs
- [ ] 0.5 Add `golangci-lint.yml` (gofmt, govet, errcheck, staticcheck)

## Phase 1: Store

- [ ] 1.1 RED `store_test.go`: open creates DB+WAL, migrations idempotent
- [ ] 1.2 GREEN `schema.go` + `migrations.go` + `store.Open`
- [ ] 1.3 RED: insert AssistantEvent upserts project+session, sets cost+task_id
- [ ] 1.4 GREEN `Insert(ctx, ev, cost, taskID)` in one tx
- [ ] 1.5 RED: `LoadOffsets`/`SaveOffset` round-trip
- [ ] 1.6 GREEN offsets queries
- [ ] 1.7 RED: `AggregatesForToday`, `TopSessionsByCost(5)`, `TopProjectsByCost(5,7d)` <50ms on 100k seeded events
- [ ] 1.8 GREEN aggregate queries + indexes

## Phase 2: Pricing

- [ ] 2.1 RED `pricing_test.go`: TOML loads 4 classes per model; unknown model → cost nil + warn-once
- [ ] 2.2 GREEN `pricing.Load`, `Calculate(ev)`
- [ ] 2.3 GREEN `configs/pricing.toml` seed (Opus/Sonnet/Haiku 4.x) + `go:embed` + first-run copy

## Phase 3: Parser

- [ ] 3.1 RED `parser_test.go` with fixtures from real `~/.claude/projects/*.jsonl`: assistant event → token classes; user/attachment → typed; unknown → UnknownEvent (no error); malformed → error
- [ ] 3.2 GREEN `events.go` types + `ParseLine`
- [ ] 3.3 RED: version warning when `version` exceeds supported range
- [ ] 3.4 GREEN version-range check

## Phase 4: Collector

- [ ] 4.1 RED `collector_test.go`: cold start ingests all existing files; offsets persisted
- [ ] 4.2 GREEN `Watch` (fsnotify on `projects/`) + `Tail(file)` goroutine + offsets
- [ ] 4.3 RED: warm start ingests only new bytes
- [ ] 4.4 GREEN offset-aware tailing
- [ ] 4.5 RED: new session file detected within 1s of create
- [ ] 4.6 GREEN create-event handling
- [ ] 4.7 RED: malformed line increments counter, advances offset, no crash
- [ ] 4.8 GREEN error path

## Phase 5: Usage (OAuth)

- [ ] 5.1 RED `client_test.go` with `httptest`: GET returns Snapshot from JSON
- [ ] 5.2 GREEN `client.Get` + 60s cache
- [ ] 5.3 RED: 401 triggers refresh + retry once
- [ ] 5.4 GREEN refresh path
- [ ] 5.5 RED: refresh writes credentials atomically (temp+rename, mode 0600, flock)
- [ ] 5.6 GREEN `credentials.go` atomic write
- [ ] 5.7 RED: API-key-only creds → `ErrUsageUnavailable`
- [ ] 5.8 GREEN credential type detection

## Phase 6: Tasks

- [ ] 6.1 RED `tasks_test.go`: `Start` writes sidecar + DB row; `Stop` clears sidecar + sets ended_at
- [ ] 6.2 GREEN `Start`/`Stop`/`Current`
- [ ] 6.3 RED: `Resolve(sid, ts)` returns task within window; nil after max_age (auto-stop)
- [ ] 6.4 GREEN `Resolve` + auto-expire

## Phase 7: TUI

- [ ] 7.1 RED `tui_test.go` with `teatest`: empty DB renders `—` placeholders, no errors
- [ ] 7.2 GREEN `model.go`/`view.go`/`update.go` skeleton + tick cmd
- [ ] 7.3 RED: with seeded data renders all sections (5h/7d/7d-opus bars, today €, top sessions, top projects, active task, footer)
- [ ] 7.4 GREEN section renderers via lipgloss
- [ ] 7.5 RED: `q` and `Ctrl+C` exit cleanly
- [ ] 7.6 GREEN quit handling

## Phase 8: CLI

- [ ] 8.1 RED `main_test.go` (exec binary in tmp HOME): `task start/stop/list` happy paths
- [ ] 8.2 GREEN `cmd/claudeops/main.go` subcommand router (default → TUI)
- [ ] 8.3 GREEN `task list` table output

## Phase 9: Integration

- [ ] 9.1 RED end-to-end test: synthetic project dir + jsonl + active task → dashboard aggregates correct, task € matches manual calc
- [ ] 9.2 GREEN wire collector → store → tui in `main`
- [ ] 9.3 Run `go test -race ./...`; resolve any data races
- [ ] 9.4 `go build ./cmd/claudeops` on a clean machine (no CGO) succeeds

## Phase 10: Docs

- [ ] 10.1 `docs/plan.md` — MVP scope, phases, status
- [ ] 10.2 `docs/architecture.md` — package map, data flow, decisions
- [ ] 10.3 `docs/jsonl-format.md` — observed event types and fields
- [ ] 10.4 `docs/oauth-usage-endpoint.md` — endpoint, headers, response, refresh, caveats
- [ ] 10.5 `docs/limitations.md` — gaps when TUI is closed, undocumented endpoint risk
- [ ] 10.6 `README.md` — install, run, config locations, screenshot

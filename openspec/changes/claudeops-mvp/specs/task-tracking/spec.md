# Delta for task-tracking

## ADDED Requirements

### Requirement: Sidecar current-task file

`claudeops task start "<name>"` MUST write `~/.claudeops/current-task.json` containing `{id, name, started_at, max_age}`. `claudeops task stop` MUST remove it.

#### Scenario: Start task
- GIVEN no current task exists
- WHEN the user runs `claudeops task start "refactor parser"`
- THEN `current-task.json` is written with a fresh UUID, the given name, the current timestamp, and `max_age` from config (default 4h)

#### Scenario: Stop task
- GIVEN a current task exists
- WHEN the user runs `claudeops task stop`
- THEN the file is deleted and the task row in SQLite is marked `ended_at = now()`

#### Scenario: Start task while another is active
- GIVEN a current task exists
- WHEN the user runs `claudeops task start "..."`
- THEN the previous task is stopped first, then the new one is created — the user is informed of the implicit stop

### Requirement: Event-to-task correlation at write time

The collector MUST tag every ingested event with `task_id` when the event's `(sessionId, timestamp)` falls within an active task window.

#### Scenario: Event during active task
- GIVEN a task started at T0 with max_age 4h
- WHEN an event arrives with timestamp T0+1h on any sessionId
- THEN the event's `task_id` column is set to the task id

#### Scenario: Event after max_age
- GIVEN a task started at T0 with max_age 4h
- WHEN an event arrives at T0+5h
- THEN `task_id` is NULL, the task is auto-stopped, and the dashboard shows "task expired"

### Requirement: Task list and totals

`claudeops task list` MUST print all tasks with: name, duration, total tokens (4-class breakdown), total cost €, and event count.

#### Scenario: List tasks
- GIVEN three completed tasks with attributed events
- WHEN the user runs `claudeops task list`
- THEN a table is printed with one row per task in chronological order

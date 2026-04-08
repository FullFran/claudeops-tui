# Delta for dashboard-tui

## ADDED Requirements

### Requirement: Single-view dashboard

The TUI MUST render exactly one consolidated view in MVP, built with Bubbletea, showing all key metrics simultaneously.

#### Scenario: Default launch
- GIVEN claudeops is invoked with no subcommand
- WHEN the program starts
- THEN a Bubbletea program runs with one model and the user sees the dashboard within 2 seconds

### Requirement: Dashboard sections

The dashboard MUST display these sections:
- Header: `claudeops` + version + OAuth status
- Subscription usage: `5h%`, `7d%`, `7d-opus%` each as a labeled progress bar with `resets in <duration>`
- Today: total events, total tokens (4-class breakdown), total cost €
- Top sessions today (top 5 by cost): session id (short), project name, € cost
- Top projects (top 5 by cost over last 7d): project name, € cost
- Active task: name, elapsed, tokens, € (or "no active task")
- Footer: `pricing updated: <date>` and parser version warning if any

#### Scenario: Render with data
- GIVEN ingested events for the current day across multiple sessions and an active task
- WHEN the dashboard renders
- THEN every section above shows non-placeholder values

#### Scenario: Render with no data
- GIVEN an empty database
- WHEN the dashboard renders
- THEN sections show `—` placeholders, never error messages

### Requirement: Live refresh

The dashboard MUST refresh on a 2-second tick and SHOULD also refresh immediately when the collector signals new data.

#### Scenario: Tick refresh
- GIVEN the dashboard is visible
- WHEN 2 seconds pass
- THEN aggregate queries re-run and the view updates

### Requirement: Quit and minimal interaction

The dashboard MUST support `q` and `Ctrl+C` to quit. No other input is required in MVP.

#### Scenario: Quit
- GIVEN the dashboard is visible
- WHEN the user presses `q`
- THEN the program exits with status 0 and the terminal is restored

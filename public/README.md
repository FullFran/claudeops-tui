# Landing screenshots

Real VHS captures of the ClaudeOps TUI, taken against live `~/.claudeops` data
at 1300px wide, FontSize 16. Regenerate with `bash public/_tapes/generate.sh`.

## Tab captures

| File | Tab / view | Landing value |
| --- | --- | --- |
| `01-dashboard.png` | Dashboard | Hero shot: subscription %, today's cost, 14-day sparkline, burn rate, streak |
| `02-sessions.png` | Sessions | Per-session cost ranking |
| `03-projects.png` | Projects | Per-project cost ranking |
| `04-models.png` | Models | Per-model token & cost table (Claude + Codex + Gemini + more) |
| `05-tasks.png` | Tasks | Task history with costs |
| `06-insights.png` | Insights | Auto-computed observations (cache, model mix, cost trend, peak hours) |
| `07-classroom.png` | Classroom | Live grid of active Claude Code sessions |
| `08-settings.png` | Settings | Inline config: widget toggles, thresholds, visible tabs |

## Feature captures

| File | Feature | Landing value |
| --- | --- | --- |
| `09-help.png` | Keybindings overlay | Shows the full keyboard-driven UX |
| `10-day-browse.png` | Daily breakdown browser | Navigable 30-day cost list |
| `11-day-detail.png` | Day detail | Hourly activity bar chart + per-model split |
| `12-task-input.png` | New-task modal | Inline task tracking / attribution |
| `13-session-detail.png` | Session detail | Full per-session breakdown: card, hourly, models, tokens |

## Privacy status

`02-sessions`, `03-projects` and `13-session-detail` were **regenerated against an
anonymized copy** of the database: project names are mapped to generic labels
(`payments-api`, `web-platform`, `mobile-app`, …) while all costs, tokens and
timings stay real. `ClaudeOps-TUI` is kept as-is on purpose (dogfooding). No
client names remain in those three.

Clean out of the box (never had client data): `04-models`, `05-tasks`,
`06-insights`, `08-settings`, `09-help`, `10-day-browse`, `11-day-detail`,
`12-task-input`.

### ⚠️ Still need a manual blur before publishing

Two shots read live data straight from `~/.claude` (not the DB), so the anonymized
copy can't cover them without gutting the view:

| File | Exposure | Why not anonymized | Fix |
| --- | --- | --- | --- |
| `01-dashboard.png` | `okupas` ×2 in "Top sessions (7d)" | Running against the anon DB drops the real subscription-usage bars (needs `~/.claude` creds) | Blur 2 lines |
| `07-classroom.png` | `okupas`, `blakbill` on 2 live-session cards | Live sessions are read from `~/.claude/projects` on disk, not the DB | Blur 2 card labels |

### Regenerating anonymized shots

`generate.sh` uses the real DB. To reproduce the anonymized `02/03/13`, copy the
DB to a throwaway `$HOME/.claudeops`, remap `projects.name` (and `cwd`) to generic
labels, then run the tapes with `HOME=<that dir> ./claudeops`. The one-off script
used for this pass lived in the scratchpad, not the repo.

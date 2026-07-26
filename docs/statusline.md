# Status bar

`claudeops statusline` prints your current quota as one line, for tmux, Zellij, a
shell prompt, or anything else that can run a command.

```console
$ claudeops statusline
5h 6% · 7d 29%
```

It is the same data the Dashboard shows under **Subscription usage**, reduced to
something that fits between a CPU meter and a clock.

## Install

The status line ships with the CLI. There is nothing extra to install:

```bash
go install github.com/fullfran/claudeops-tui/cmd/claudeops@latest
```

The binary lands in `$(go env GOPATH)/bin` (usually `~/go/bin`), which must be on
your `PATH` for a status bar to find it. If you already run the TUI, you already
have it — check with `claudeops statusline --refresh`.

## Turning it on and off

The status line is on by default. To stop it rendering without touching your bar
config:

```console
$ claudeops statusline disable
statusline disabled (/home/you/.claudeops/config.toml)

$ claudeops statusline
                          # nothing, exit 0

$ claudeops statusline status
statusline: disabled
provider:   auto
config:     /home/you/.claudeops/config.toml

$ claudeops statusline enable
statusline enabled (/home/you/.claudeops/config.toml)
```

Disabled short-circuits before any work: no network, no cache read. Your bar can
keep calling the command and simply shows an empty segment, so there is nothing
to comment out and no reload to remember.

It is the `enabled` key under `[statusline]`, so editing the file by hand works
too. A config written before that key existed has no `[statusline]` section at
all; that counts as enabled, and nothing changes for you on upgrade.

Note that `enable`/`disable` rewrite `config.toml` the same way the TUI does when
you change a setting there: the file is regenerated from your merged settings, so
keys you never set appear with their defaults and keys no longer recognised are
dropped.

## Which quota it shows

By default it **follows the agent in the pane you are looking at**. A Claude Code
pane shows the Anthropic window; an opencode pane wired to OpenAI models shows
the Codex one. Showing every provider at once makes the number you care about
harder to find.

```console
$ claudeops statusline --provider auto     # follow the active pane (default)
$ claudeops statusline --provider claude   # pin one
$ claudeops statusline --provider all --labels
claude 5h 6% · 7d 29% │ codex 5h 12%
```

Detection reads the active pane's command. When that is a runtime — `node`,
`bun`, a shell — it walks one level into the process tree, because the process
name of an agent shipped as a Node program is just `node`.

Matching is per argument, on the basename, and exact. It is deliberately not a
substring search over the whole command line: a working directory named
`~/work/claude-tools` would otherwise make every pane look like Claude.

Configure the default and the mapping in `~/.claudeops/config.toml`:

```toml
[statusline]
provider = "auto"        # or "all", or a provider name

[statusline.agents]
claude   = "claude"
opencode = "codex"       # point at "claude" if your opencode uses Anthropic models
codex    = "codex"
crush    = "codex"
```

The mapping is a plain command-to-provider table, so adding an agent is a config
edit rather than a code change.

## Caching

The usage client caches in process with a five minute TTL, which is right for the
TUI because it is long lived. A status bar is the opposite: it spawns a fresh
process on every redraw, so that cache never survives to be used and every redraw
would hit the endpoint — roughly thirty requests a minute at a two second
interval.

So the snapshot is kept in `~/.claudeops/usage-cache.json` and served for `--ttl`
(one minute by default). Written atomically via temp file and rename, mode 0600
since it describes your account's quota.

Measured on one machine: **460 ms** for a live fetch, **5 ms** per call warm.

Registry providers (Codex, Copilot, Gemini, user-defined) share the same file.
Failures are never cached, so a service you are logged out of disappears from the
bar instead of sticking.

## Failure is quiet

A status bar is not where you want to discover that a token expired, so:

- fetch fails and a cache exists → the stale snapshot is served. A quota from a
  minute ago still says roughly where you stand.
- fetch fails with no cache → nothing is printed, exit 0.
- the cache file is corrupt or has no timestamp → treated as missing.
- a plan has no such quota, or a provider has no credentials → that group is
  omitted rather than rendered as zeroes.

Every one of these ends with an empty segment rather than an error in your bar.

## Forecasting

`--forecast` appends a warning when a window is on course to hit 100% **before it
resets**:

```console
$ claudeops statusline --forecast
5h 62% · 7d 29% ⚠5h out in 1h20m
```

It projects from **observed utilisation**, not from the euro burn rate the
dashboard shows. Those are different axes — one is Anthropic's accounting of the
window, the other your local estimate against an editable price table — and
converting between them would mean guessing the exchange rate, which is exactly
the estimation this project refuses to do.

So it measures instead. Every refresh appends a sample to the shared cache, and
the slope comes from what actually happened.

That means it says nothing until it has something to say:

- fewer than two samples, or less than ten minutes between them
- flat or falling usage
- a window already at 100%
- a reset that lands before exhaustion would

A number that shows up only when it is grounded is worth more than one that is
always there.

## Colour outside tmux

tmux colour escapes are `#[fg=...]`, which nothing else understands — emitted
into a Zellij bar or a shell prompt they show up as literal text next to the
number. So `--color` picks the syntax for where it is running:

| Value | Emits |
|---|---|
| `--color` | tmux syntax inside tmux, ANSI everywhere else |
| `--color=tmux` | tmux syntax, always |
| `--color=ansi` | 24-bit SGR, always |
| `--color=none` | no escapes (the default) |

Detection is `$TMUX`, so a bare `--color` is the right answer almost everywhere.
Force a mode when you are piping output somewhere that does not share the
environment — a daemon writing a file, say.

## Without tmux

Nothing here requires tmux. The pieces that touch it degrade rather than fail:

| Situation | Behaviour |
|---|---|
| Not running under tmux | `--provider auto` falls back to `claude`; pin a provider or set one in config to be explicit |
| tmux not installed at all | the same, the lookup simply finds nothing |
| `--color` outside tmux | ANSI, not tmux escapes |

Agent detection is the only feature that genuinely needs tmux, because it asks
which pane you are looking at. Outside tmux there is no such question, so set
`provider` in config to the service you actually use.

## tmux

```tmux
set -g status-right "#(claudeops statusline --color) │ %H:%M"
set -g status-interval 5
```

Guard it if the same config is shared across machines, so a host without
claudeops gets an empty segment rather than a shell error:

```tmux
set -g status-right "#(command -v claudeops >/dev/null && claudeops statusline --color) │ %H:%M"
```

To switch provider without editing config, keep the choice in a user option and
pass it through. An empty value means "use the configured default", which is
exactly what tmux sends for an unset option:

```tmux
set -g @quota "auto"
set -g status-right "#(claudeops statusline --color --provider '#{@quota}')"

bind Q run-shell "tmux set -g @quota \
  \"#{?#{==:#{@quota},auto},claude,#{?#{==:#{@quota},claude},codex,#{?#{==:#{@quota},codex},all,auto}}}\" \; \
  refresh-client -S \; display-message \"quota: #{@quota}\""
```

## Zellij

```kdl
format_right "{command_statusline}"

command_statusline {
    command  "claudeops"
    args     "statusline"
    interval "5"
}
```

Leave `--color` off here: the escapes are tmux syntax.

## Shell prompt

```bash
PS1='$(claudeops statusline) \w \$ '
```

The on-disk cache makes this cheap enough to run on every prompt.

## Flags

| Flag | Default | Purpose |
|---|---|---|
| `--provider` | config, then `auto` | a provider name, `all`, or `auto` to follow the active pane |
| `--format` | `compact` | `compact`, `plain` or `json` |
| `--color` | off | wrap compact output in tmux colour escapes |
| `--labels` | off | prefix each group with its provider name |
| `--prefix` | none | text before the output, emitted only when there is output |
| `--reset` | off | append the time left in the first window |
| `--forecast` | off | warn when a window is on course to run out before it resets |
| `--ttl` | `1m` | how long a cached snapshot is reused |
| `--refresh` | off | ignore the cache and fetch now |
| `--timeout` | `3s` | budget for a live fetch |
| `--warn-at` | `60` | utilisation that turns the segment amber |
| `--crit-at` | `85` | utilisation that turns the segment red |

## JSON

For anything that wants the numbers rather than a rendering:

```console
$ claudeops statusline --format json
[{"provider":"claude","windows":[{"label":"5h","utilization":6,"resets_at":"2026-07-26T03:23:00Z"}]}]
```

The shape is declared in `internal/statusline` with explicit tags rather than
reusing internal structs, so it does not drift when those change.

## Troubleshooting

Start with the doctor. The status line drops failures on purpose — a bar is no
place to learn that a token expired — so this is where that gets said out loud:

```console
$ claudeops statusline doctor
config      ~/.claudeops/config.toml
statusline  enabled
provider    auto  (active pane → claude)
cache       27s old

claude    stale   usage endpoint rate-limited (HTTP 429); serving cache from 28s ago
codex     ok      7d 0%  (plan: plus, via opencode)
copilot   absent  no credentials found
                  → sign in with the GitHub Copilot CLI, or your editor's Copilot extension
gemini    absent  no credentials found
                  → run `gemini auth`, or sign in to google through opencode

agent mapping
  claude     → claude
  opencode   → codex
```

The states are deliberately distinct:

| State | Meaning | Needs action |
|---|---|---|
| `ok` | answered with data | no |
| `stale` | refresh failed, cache still serving — the bar is fine | no |
| `empty` | authenticated, but this plan reports no quota | no |
| `absent` | no credentials; you do not use this service | no |
| `error` | nothing usable | yes |

It exits non-zero only for `error`, so `claudeops statusline doctor` can gate a
script without failing on a service you simply do not use.

`via <source>` names the credential store that answered. When a provider can
read more than one — Codex takes the Codex CLI's `auth.json` *or* an opencode
`openai` session — that is the difference between "run `codex login`" and "you
are already signed in, through opencode".

**Nothing prints.** By design when there is nothing to say. Run
`claudeops statusline --refresh --format plain` to see the unrendered state.

**Wrong provider in a pane.** Check what the pane actually runs — tmux reports
the foreground process, which for a wrapper script is `sh`. Add that agent's
binary name to `[statusline.agents]`.

**Stale numbers.** `--ttl 0` disables the cache; expect a request per redraw and
do not leave it that way.

## See also

- [`providers.md`](./providers.md) — which services are tracked and where their
  credentials come from
- [`oauth-usage-endpoint.md`](./oauth-usage-endpoint.md) — the Anthropic endpoint
  behind the Claude numbers

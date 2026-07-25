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

**Nothing prints.** By design when there is nothing to say. Run
`claudeops statusline --refresh --format plain` to see the unrendered state, and
`claudeops` (the TUI) to check the provider is detected at all.

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

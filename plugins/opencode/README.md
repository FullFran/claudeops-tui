# opencode plugin

Puts your subscription quota next to the opencode prompt, so the number is
where you are already looking.

```
› write the thing                              claude 5h 3% · 7d 29% │ codex 7d 0%
```

## Install

This is a **TUI plugin**, so it goes in `tui.json` — not `opencode.json`, and not
just dropped in the plugins directory:

```json
// ~/.config/opencode/tui.json
{ "plugin": ["/absolute/path/to/claudeops.tsx"] }
```

The `plugin` array in `opencode.json` is the *server* plugin registry. Putting
this there fails with `must default export an object with server()`, and copying
the file into `~/.config/opencode/plugins/` without declaring it does nothing at
all — that directory is only auto-loaded for server plugins.

The `@jsxImportSource` pragma on line 1 is load-bearing — a `.tsx` dropped into
the plugins directory is otherwise compiled with the runtime's default JSX
factory, which is React, and the elements here would fail to resolve. Keep it if
you copy the file around.

It needs `claudeops` on `PATH` — see the [status line docs](../../docs/statusline.md).

## Options

```json
{
  "plugin": [
    ["/path/to/claudeops.tsx", {
      "provider": "claude",
      "interval": 30,
      "warnAt": 60,
      "critAt": 85
    }]
  ]
}
```

| Option | Default | Meaning |
|---|---|---|
| `provider` | the CLI's config | which quota to show; `all` shows every one |
| `interval` | `30` | seconds between reads, floor of 5 |
| `warnAt` / `critAt` | `60` / `85` | utilisation at which the text turns amber, then red |
| `bin` | `claudeops` | path to the binary when it is not on `PATH` |

Colours come from your opencode theme (`warning`, `error`, `textMuted`), read on
each render so switching theme is picked up immediately.

## Why it shells out

The plugin runs `claudeops statusline --format json` rather than calling the
endpoint itself. The CLI owns the credentials, the provider selection and the
**shared on-disk cache** — so this costs a cache read, not a request, and it
cannot become a second poller against a rate limit that is already shared with
Claude Code. That mistake has been made once already; see the note on caching in
[`docs/statusline.md`](../../docs/statusline.md).

It also inherits every behaviour from the CLI for free: `statusline disable`
turns this off too, and `statusline doctor` explains an empty segment.

## Failure

Renders nothing. Not an error, not a placeholder — the prompt is not the place
to learn that a token expired. Ask `claudeops statusline doctor` instead.

## Tests

```sh
bun test plugins/opencode/format.test.ts
```

Covers the formatting rules. The plugin module itself cannot be imported outside
a running TUI, so the pure functions are duplicated in the test file on purpose.

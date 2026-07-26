/** @jsxImportSource @opentui/solid */
/**
 * claudeops — opencode TUI plugin
 *
 * Puts your subscription quota next to the prompt, so the number is where you
 * are already looking instead of in another window.
 *
 * It shells out to `claudeops statusline --format json` rather than reading the
 * endpoint itself. That is deliberate: the CLI owns the credentials, the
 * provider selection and — most importantly — the shared on-disk cache. Talking
 * to the endpoint from here would be a second poller against a rate limit that
 * is already shared with Claude Code, which is exactly the mistake that got an
 * account 429'd once already.
 *
 * Install:
 *   plugin: ["claudeops/plugins/opencode/claudeops.tsx"]   in opencode.json
 * or drop it in ~/.config/opencode/plugins/.
 *
 * The jsxImportSource pragma above is load-bearing. A .tsx file dropped into the
 * plugins directory is compiled with whatever default the runtime has, which is
 * React — the JSX here would fail to resolve. The pragma keeps the file
 * self-contained rather than depending on a tsconfig the user has to supply.
 */
import type { TuiPluginApi, TuiPluginModule } from "@opencode-ai/plugin/tui"
import { createSignal, onCleanup } from "solid-js"

/** One quota window as `claudeops statusline --format json` reports it. */
type Window = {
  label: string
  utilization: number
  resets_at?: string
}

type Group = {
  provider: string
  windows: Window[]
  note?: string
}

type Options = {
  /** Which quota to show. Defaults to the CLI's own config. */
  provider?: string
  /** Seconds between reads. The CLI serves a shared cache, so this is cheap. */
  interval?: number
  /** Utilisation at which the segment turns amber, then red. */
  warnAt?: number
  critAt?: number
  /** Path to the binary when it is not on PATH. */
  bin?: string
}

const DEFAULTS = {
  interval: 30,
  warnAt: 60,
  critAt: 85,
  bin: "claudeops",
} as const

/**
 * Read the quota. Returns null for every failure, because a status segment that
 * renders an error message is worse than one that renders nothing — the CLI
 * already has `statusline doctor` for the question "why is this empty".
 */
async function readQuota(opts: Options): Promise<Group[] | null> {
  const args = ["statusline", "--format", "json"]
  if (opts.provider) args.push("--provider", opts.provider)

  try {
    const proc = Bun.spawn([opts.bin ?? DEFAULTS.bin, ...args], {
      stdout: "pipe",
      stderr: "ignore", // diagnostics belong in `doctor`, not in the prompt
    })
    // A hung binary must not wedge the TUI.
    const timeout = setTimeout(() => proc.kill(), 5000)
    const text = await new Response(proc.stdout).text()
    clearTimeout(timeout)
    if ((await proc.exited) !== 0) return null

    const trimmed = text.trim()
    if (!trimmed) return null // disabled, or nothing to report
    const parsed = JSON.parse(trimmed)
    return Array.isArray(parsed) ? (parsed as Group[]) : null
  } catch {
    return null
  }
}

/** Highest utilisation across everything shown; drives the colour. */
function peak(groups: Group[]): number {
  let max = 0
  for (const g of groups) {
    for (const w of g.windows) {
      if (w.utilization > max) max = w.utilization
    }
  }
  return max
}

/**
 * Render as `5h 6% · 7d 29%`, prefixed by the provider when more than one is
 * shown. A single provider needs no label — you know which one you asked for.
 */
function format(groups: Group[]): string {
  const multi = groups.length > 1
  return groups
    .map((g) => {
      const windows = g.windows.map((w) => `${w.label} ${Math.round(w.utilization)}%`).join(" · ")
      return multi ? `${g.provider} ${windows}` : windows
    })
    .join(" │ ")
}

export default {
  id: "claudeops",

  tui: async (api: TuiPluginApi, options) => {
    const opts = (options ?? {}) as Options
    const interval = Math.max(5, opts.interval ?? DEFAULTS.interval) * 1000
    const warnAt = opts.warnAt ?? DEFAULTS.warnAt
    const critAt = opts.critAt ?? DEFAULTS.critAt

    const [groups, setGroups] = createSignal<Group[] | null>(null)

    let stopped = false
    const tick = async () => {
      if (stopped) return
      setGroups(await readQuota(opts))
    }

    void tick()
    const timer = setInterval(tick, interval)

    // The TUI outlives individual sessions; stop cleanly when it goes away.
    api.lifecycle.onDispose(() => {
      stopped = true
      clearInterval(timer)
    })
    onCleanup(() => clearInterval(timer))

    api.slots.register({
      slots: {
        session_prompt_right: (ctx) => {
          const data = groups()
          // Nothing to say: render nothing rather than a placeholder. The
          // prompt is not the place to explain a missing credential.
          if (!data || data.length === 0) return null

          // ctx.theme is the theme *controller*; the colours live on .current,
          // and reading it here rather than at registration means a theme
          // switch is picked up on the next render.
          const palette = ctx.theme.current
          const worst = peak(data)
          const colour =
            worst >= critAt ? palette.error : worst >= warnAt ? palette.warning : palette.textMuted

          return (
            <box paddingLeft={1}>
              <text fg={colour}>{format(data)}</text>
            </box>
          )
        },
      },
    })
  },
} satisfies TuiPluginModule

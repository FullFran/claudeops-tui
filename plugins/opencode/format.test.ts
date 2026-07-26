import { expect, test } from "bun:test"

type Window = { label: string; utilization: number }
type Group = { provider: string; windows: Window[]; note?: string }

// The pure formatting rules, kept next to the plugin so a change to either has
// to be a deliberate one. They are duplicated rather than imported because the
// plugin module pulls in the opencode TUI runtime, which cannot load outside a
// running TUI.
function peak(groups: Group[]): number {
  let max = 0
  for (const g of groups) for (const w of g.windows) if (w.utilization > max) max = w.utilization
  return max
}
function format(groups: Group[]): string {
  const multi = groups.length > 1
  return groups.map((g) => {
    const windows = g.windows.map((w) => `${w.label} ${Math.round(w.utilization)}%`).join(" · ")
    return multi ? `${g.provider} ${windows}` : windows
  }).join(" │ ")
}

test("a single provider carries no label: you know which one you asked for", () => {
  const g: Group[] = [{ provider: "claude", windows: [{ label: "5h", utilization: 6 }, { label: "7d", utilization: 29 }] }]
  expect(format(g)).toBe("5h 6% · 7d 29%")
})

test("several providers do, or they are indistinguishable", () => {
  const g: Group[] = [
    { provider: "claude", windows: [{ label: "5h", utilization: 6 }] },
    { provider: "codex", windows: [{ label: "7d", utilization: 12 }] },
  ]
  expect(format(g)).toBe("claude 5h 6% │ codex 7d 12%")
})

test("colour follows the worst window, not the first", () => {
  const g: Group[] = [
    { provider: "claude", windows: [{ label: "5h", utilization: 3 }] },
    { provider: "codex", windows: [{ label: "7d", utilization: 91 }] },
  ]
  expect(peak(g)).toBe(91)
})

test("rounds to whole percent, matching the tmux segment", () => {
  const g: Group[] = [{ provider: "claude", windows: [{ label: "5h", utilization: 42.6 }] }]
  expect(format(g)).toBe("5h 43%")
})

test("no windows invents nothing", () => {
  expect(format([{ provider: "claude", windows: [] }])).toBe("")
  expect(peak([])).toBe(0)
})

# Google AI Pro, Antigravity quota, and family sharing

This note separates Google's documented AI-credit rules from what ClaudeOps can
observe locally. It is intentionally conservative: a local quota percentage is
not evidence of Google's private accounting model or of how a family group is
charged.

## The short version

- **VERIFIED:** Google describes Google Antigravity as having a subscription-tier
  **baseline quota** and a separate **AI credits** balance for overage. Credits
  are deducted according to the model and request complexity, and Google says
  eligible AI credits can be shared and pooled with a Google One family group.
- **VERIFIED:** Google says family members can see total AI-credit usage, but not
  individual activity, on the family activity view. The plan manager controls
  buying credits and changing the plan.
- **OBSERVED IN CLAUDEOPS:** ClaudeOps can persist the quota buckets that
  Antigravity sends to its configured status-line command. It does not receive a
  family ledger or AI-credit balance from that payload.
- **UNKNOWN:** Google's allocation and weighting rules for the Antigravity
  baseline buckets. In particular, there is no direct evidence here that the
  baseline 5-hour bucket is family-pooled. Do not infer that from a percentage
  changing, from a family member's activity, or from local token counts.

## Baseline quota versus AI credits

Google's current Google AI Pro help page describes two tiers for Antigravity:

1. **Baseline quota** — time-bound usage limits associated with the subscription
   tier and specific to Antigravity.
2. **AI credits** — a separately managed balance that can extend use after the
   baseline quota, when overage is enabled. Google says the deduction varies by
   model and request complexity and can be used across supported Google products.

These are different measurements. A baseline percentage is not a credit balance,
and a local estimate of tokens is not an AI-credit charge. Google also states
that product limits depend on the feature, model, and complexity of the request;
limits can change.

## What family sharing does and does not establish

**VERIFIED from Google's documentation:**

- Eligible Google One family groups can share AI credits; the credits are pooled
  for supported features.
- The AI-credit activity view can show each family member's total credit usage,
  while not exposing individual activity details to the family group.
- Individual and family allocations can coexist and have separate refresh dates.
- Google AI Pro family members receive selected benefits, but the documentation
  does not say that every product quota is pooled in the same way.

**UNKNOWN for Antigravity baseline quota:** Google's family-sharing guidance does
not establish that Antigravity's subscription baseline buckets—such as a
5-hour or 7-day bucket—are shared or pooled across family members. This document
does not make that claim. Confirm it only from an explicit Google statement or a
controlled test using Google's own account UI and plan terms.

## Local evidence captured on 2026-09-22

**OBSERVED IN CLAUDEOPS:** one local reading reported these Antigravity quota
windows:

| Bucket | Reported utilization |
| --- | ---: |
| 5h | 100% |
| 7d | 25.64% |
| 3p-5h | 62.83% |
| 3p-7d | 20.94% |

The same local rolling-window inspection found **12 Flash calls**, with
**137,588 input/output tokens** and **158,896 cache-read tokens**. These figures
correlate with activity recorded in local Antigravity conversation databases.
They do not identify which Google quota bucket was charged, whether any family
member contributed usage, or how Google weights model complexity, cache reads,
agent actions, or other private signals. ClaudeOps therefore cannot
reverse-engineer Google's weighting from these observations.

ClaudeOps reads Antigravity's status-line payload and local conversation metadata;
it does not call a Google family-account API. The persisted quota snapshot is
local and bounded to quota metadata, plan tier, and reset information; message
content is not used for this diagnostic.

## Safe diagnostics

Run these locally, and redact account names, paths, timestamps, or raw payloads
before sharing output:

```bash
# Show whether ClaudeOps is wired as Antigravity's status line and the last reading.
claudeops agy status

# Force a fresh local status-line rendering as JSON (no token or prompt content).
claudeops statusline --provider antigravity --format json --refresh

# Diagnose provider state, cache age, and authentication/configuration failures.
claudeops statusline doctor
```

For the authoritative account view, use Antigravity's own **Settings** or
**Models** selector for baseline quota and Google One's **AI credits activity**
page for credit balance and credit activity. Do not paste credentials,
`~/.claudeops/antigravity-quota.json`, raw status-line input, or full conversation
database rows into an issue.

## Limitations and freshness

- The status-line snapshot is only as fresh as the last time Antigravity invoked
  the configured command. A missing snapshot means ClaudeOps has not received a
  reading; it does not mean the account has no quota.
- The payload has no documented family-member attribution. ClaudeOps cannot
  distinguish the plan manager's baseline use from another member's use.
- Local token counts are source observations and are not a billing statement.
- Google can change limits, model names, credit pricing, and UI behavior. The
  claims above reflect the linked pages as checked on 2026-09-22.
- The Google pages describe AI credits and selected family benefits; they do not
  expose the private algorithm that maps Antigravity work to baseline buckets.

## Primary sources

- [Manage your AI credits with Google One](https://support.google.com/googleone/answer/16287445?hl=en)
- [Use Google AI Pro benefits](https://support.google.com/googleone/answer/14534406?hl=en)
- [Gemini Apps limits and upgrades](https://support.google.com/gemini/answer/16275805?hl=en)
- [ClaudeOps repository](https://github.com/FullFran/claudeops-tui)
- [ClaudeOps Antigravity format notes](./agy-format.md)
- [ClaudeOps provider and status-line reference](./providers.md)

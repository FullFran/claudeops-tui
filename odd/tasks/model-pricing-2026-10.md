# Model pricing update
Objective: implement the approved first delivery (model catalog and accurate token estimates). Branch: feat/model-pricing-2026-10.
Delivery: feature-branch-chain (user selected); forecast ~430 authored lines, generated data excluded. Runner: go test ./...; go test -race ./...; build and formatting.

## Specs
S1. Original request: "tenemos que actualizar claudeops! han salido cosas nuevas! y nuevos modelos .... revisa y vamos a ver que hacemos".
S2. Authorized scope: "perfecto, haz primero la primera y seguimos con la segunda después. puedes usar subagentes agy y los probamos". The accepted first delivery is the new model catalog, verified prices, context/date-aware estimates, and preservation of local custom prices. Agent integrations are deferred. Do not modify the runtime database or user pricing files.
S3. Delegation preference: "usa bifrost". Latest implementation authorization: "pues, vamos a implementarlo!".
S4. Delivery selection: "Dividir sobre una rama de la funcionalidad (recomendado)". Work-unit commits stay local; publishing and merging require separate permission.

## Tasks
T1 | S1-S3 | delegated agy via Bifrost isolated worktree | in_progress | Add tested backward-compatible context/date pricing and ingestion timestamp wiring, with configuration round-trip and override preservation; commit pending.
T2 | S1-S3 | delegated agy via Bifrost isolated worktree | pending | Add verified model prices and schedules, tests and upgrade documentation; commit pending.
T3 | S1-S4 | parent + independent verifier for shared pricing compatibility | pending | Inspect candidate, assess risk, run functional/full/race/build checks and record reviewable commits; commit pending.

## Log
L1. User request verbatim: "tenemos que actualizar claudeops! han salido cosas nuevas! y nuevos modelos .... revisa y vamos a ver que hacemos".
L2. User approval verbatim: "perfecto, haz primero la primera y seguimos con la segunda después. puedes usar subagentes agy y los probamos". Later: "usa bifrost"; "pues, vamos a implementarlo!".
L3. Baseline b51abfaf045be2b560cfae62d84253151f82b985, clean main. Read-only Bifrost task bf_20261028_214501_4bf773ed verified actual repository cwd. No implementation before this document.
L4. Official sources fetched October 28 2026: https://platform.claude.com/docs/en/about-claude/pricing ; https://developers.openai.com/api/docs/pricing ; https://ai.google.dev/gemini-api/docs/pricing ; https://ai.google.dev/gemini-api/docs/models . USD rates converted with existing factor 0.92, not a live FX rate.
L5. Implementation evidence: Sonnet 5.5 input/output/cache-read/cache5m/cache1h USD per million 2/10/.10/2.5/4. Haiku 5.5 <=100000 total prompt .10/.50/.01/.125/.20; >100000 .50/2.50/.05/.625/1 (whole request; input+read+all writes, 1h subset never double counted). GPT-6.1 Sol <=272000 input including cache read 2/10/.10; above 4/15/.20. GPT-6.1 Terra short 1/5/.05; long 2/7.5/.10. Gemini4 Argon before December1 2026 2/10/.10, thereafter 4/20/.20. Gemini3.8 Flash before January1 2027 .75/3.75/.075, thereafter 1.5/7.5/.15.
L6. Existing GPT6 official standard short/long input/output/cache-read USD: Astra 10/50/1 vs 20/75/2; Sol 2/10/.20 vs 4/15/.40; Luna .10/.50/.01 vs .20/.75/.02. Threshold <=272000 short, >272000 long. Cache writes 30m/12h are distinct from Claude5m/1h and not present in Codex records; do not invent charges or conflate TTLs. Service modes absent from records remain standard-rate estimates, explicitly documented, not billed-cost claims.
L7. Technical constraints: preserve CostFor/CostForCacheTTL signatures and flat legacy behavior. Use explicit event timestamp for scheduled rates. Assumption: date-only cutoffs interpreted UTC; zero timestamp uses base rates deterministically, no wall-clock dependence. Only enrich untouched exact-old-seed entries with new tier/schedule metadata; preserve all customized fields. Validate new config rules before any seed merge writes; invalid rules must error without changing file contents. Ensure manual encodeTable preserves all optional fields. Existing unknown/free model behavior and positive reported-cost precedence must remain. Historical stored costs are unchanged; do not run reingest.
L8. Verification: table-driven RED before implementation, exact threshold/above and date-before/at checks, cache TTL interaction and prompt totals, seed/load/reload round trips, legacy flat/unknown/free cases, customized seed and malformed rules preserve files, source sink integration. Run focused packages then full suite/race/build. Shared pricing helpers imply Risk item3; independent verification and proportional differential public CLI checks against baseline required. No new CLI flags or commands planned.

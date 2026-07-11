#!/usr/bin/env bash
#
# Regenerate internal/pricing/litellm_prices.json from the upstream LiteLLM
# pricing dataset. The snapshot is embedded in the binary as the multi-provider
# pricing fallback beneath the editable ~/.claudeops/pricing.toml table.
#
# Output shape: { "<bare-model-id>": [input, output, cache_read, cache_create] }
# in EUR per 1,000,000 tokens (USD list price x EUR_FACTOR), matching the seed
# convention. Provider-qualified upstream keys ("gemini/...", "azure/...") are
# reduced to their bare last segment; bare keys take precedence on collision.
#
# Usage:
#   ./scripts/update-pricing.sh
#   EUR_FACTOR=1.0 ./scripts/update-pricing.sh      # keep USD numbers
#   LITELLM_URL=<url> ./scripts/update-pricing.sh    # pin a different source
#
set -euo pipefail

EUR_FACTOR="${EUR_FACTOR:-0.92}"
LITELLM_URL="${LITELLM_URL:-https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json}"

for tool in curl jq; do
  command -v "$tool" >/dev/null 2>&1 || { echo "error: '$tool' is required" >&2; exit 1; }
done

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="$repo_root/internal/pricing/litellm_prices.json"

raw="$(mktemp)"
converted="$(mktemp)"
trap 'rm -f "$raw" "$converted"' EXIT

echo "Fetching LiteLLM pricing dataset..."
curl -fsSL "$LITELLM_URL" -o "$raw"

echo "Transforming (EUR = USD x $EUR_FACTOR)..."
jq -c --argjson f "$EUR_FACTOR" '
  def price($v): [
    (($v.input_cost_per_token          // 0) * 1000000 * $f),
    (($v.output_cost_per_token         // 0) * 1000000 * $f),
    (($v.cache_read_input_token_cost   // 0) * 1000000 * $f),
    (($v.cache_creation_input_token_cost // 0) * 1000000 * $f)
  ];
  # Pass 1: bare keys (no "/") with a real input price.
  (reduce (to_entries[]
           | select((.key | test("/")) | not)
           | select(.value.input_cost_per_token != null)) as $e ({};
     .[$e.key] = price($e.value))) as $bare
  # Pass 2: provider-qualified keys -> bare last segment, only if absent.
  | reduce (to_entries[]
            | select(.key | test("/"))
            | select(.value.input_cost_per_token != null)) as $e ($bare;
      ($e.key | split("/") | last) as $k
      | if has($k) then . else .[$k] = price($e.value) end)
' "$raw" > "$converted"

# Validate before overwriting: must be a non-trivial JSON object.
count="$(jq 'keys | length' "$converted")"
if [ "$count" -lt 100 ]; then
  echo "error: refusing to write — only $count models parsed (source format changed?)" >&2
  exit 1
fi

mv "$converted" "$out"
trap 'rm -f "$raw"' EXIT
echo "Wrote $out ($count models, $(wc -c < "$out") bytes)"
echo "Run 'go test ./internal/pricing/' to verify, then commit the snapshot."

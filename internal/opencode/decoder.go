// Package opencode implements source.Ingester for the opencode editor's SQLite DB.
//
// opencode stores assistant conversations in ~/.local/share/opencode/opencode.db.
// Each message row carries a JSON blob in its `data` column. This package reads
// those rows in read-only mode (mode=ro, WAL-safe), decodes the blobs, normalizes
// provider/model IDs to canonical pricing keys, and emits source.Record values via
// the Sink abstraction.
//
// COST RULE (ADR-006): data.cost is ALWAYS ignored. Cost is recomputed by the
// StoreSink via pricing.CostFor. Unknown models ingest with nil cost but tokens
// are still recorded.
//
// PROVIDER/MODEL NORMALIZATION (ADR-005):
//   - Anthropic models: strip dots from version numbers (claude-opus-4.6 → claude-opus-4-6)
//     to match pricing.seed.toml keys; otherwise pass through as-is.
//   - All other providers: produce "providerID/modelID" qualified key (e.g. "openai/gpt-4o").
//   - Empty providerID: return modelID unchanged.
package opencode

import (
	"encoding/json"
	"strings"
)

// tokenCacheData holds cache token sub-fields.
type tokenCacheData struct {
	Read  int64 `json:"read"`
	Write int64 `json:"write"`
}

// tokenData holds token usage fields from the data blob.
type tokenData struct {
	Total     int64          `json:"total"`
	Input     int64          `json:"input"`
	Output    int64          `json:"output"`
	Reasoning int64          `json:"reasoning"`
	Cache     tokenCacheData `json:"cache"`
}

// TokenRecord is a flat token representation used by the Ingester
// to build a source.Record. Reasoning is folded into Out.
type TokenRecord struct {
	In          int64
	Out         int64 // output + reasoning
	CacheRead   int64
	CacheCreate int64
}

// MessageData is the decoded data JSON blob from an opencode message row.
// Fields not present in every row are zero-valued.
type MessageData struct {
	Role       string    `json:"role"`
	ModelID    string    `json:"modelID"`
	ProviderID string    `json:"providerID"`
	Tokens     tokenData `json:"tokens"`
	// Cost field present in the blob but deliberately ignored — see package doc.
	// We capture it only so the JSON decoder does not error on unknown fields.
	RawCost float64 `json:"cost"`
}

// ToTokenRecord converts the token fields into the flat form needed by
// source.Record. Reasoning tokens are folded into Out (design §5.2, mirrors
// the Codex adapter's treatment of reasoning_output_tokens).
func (d *MessageData) ToTokenRecord() TokenRecord {
	return TokenRecord{
		In:          d.Tokens.Input,
		Out:         d.Tokens.Output + d.Tokens.Reasoning,
		CacheRead:   d.Tokens.Cache.Read,
		CacheCreate: d.Tokens.Cache.Write,
	}
}

// DecodeMessageData decodes the raw `data` TEXT column from a message row.
// It is permissive: extra JSON keys are ignored, missing keys are zero-valued.
// Returns an error only for invalid JSON.
func DecodeMessageData(raw []byte) (*MessageData, error) {
	var d MessageData
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// NormalizeModel maps (providerID, modelID) to a canonical pricing key.
//
// Rules (ADR-005):
//  1. Empty providerID → return modelID unchanged (no qualification possible).
//  2. Anthropic provider → normalize dots in version numbers to hyphens so the
//     key matches pricing.seed.toml (e.g. "claude-opus-4.6" → "claude-opus-4-6").
//     Return the normalized model ID bare (no "anthropic/" prefix) to preserve
//     compatibility with existing pricing keys and DB rows.
//  3. All other providers → return "providerID/modelID" as the qualified key.
//     Unknown combinations yield a pass-through that pricing treats as nil cost
//     (tokens still recorded — existing behavior).
func NormalizeModel(providerID, modelID string) string {
	if providerID == "" {
		return modelID
	}
	if providerID == "anthropic" {
		// Normalize dots in version numbers to hyphens.
		// e.g. "claude-opus-4.6" → "claude-opus-4-6"
		// e.g. "claude-sonnet-4-5" → unchanged (no dots)
		return normalizeDots(modelID)
	}
	// All other providers: qualify with "providerID/modelID".
	return providerID + "/" + modelID
}

// normalizeDots replaces dots that appear between digit sequences in model
// version strings with hyphens. This converts "claude-opus-4.6" → "claude-opus-4-6"
// while leaving names like "gemini-2.0-flash" unchanged when qualified (e.g.
// for non-Anthropic models where dots are part of the pass-through key).
//
// For Anthropic models specifically, the pricing seed uses hyphens throughout.
// We apply a simple strategy: replace all dots with hyphens in the modelID when
// the provider is anthropic, since Anthropic model version strings never use
// dots for semantic purposes — versions are separated by hyphens in the seed.
func normalizeDots(modelID string) string {
	return strings.ReplaceAll(modelID, ".", "-")
}

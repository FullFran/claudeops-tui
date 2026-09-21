package agy

import (
	"strings"
	"time"
)

// Field numbers verified against a real agy 1.2.7 install (see
// odd/tasks/agy-support.md's "Verified data format" section). There is no
// public .proto for agy, so these are pinned here as the single source of
// truth rather than scattered as magic numbers through decoder.go.
const (
	// gen_metadata.data top level.
	fieldGenMessage = 1 // the generation message (embedded)

	// generation message.
	fieldGenModelID = 19 // raw model id, e.g. "gemini-3.8-flash-exp-a"
	fieldGenUsage   = 4  // usage submessage (embedded)
	fieldGenKV      = 20 // repeated {1: key, 2: value} pairs

	// usage submessage (also reachable from a step via fieldStepUsage).
	fieldUsageInput       = 2  // uncached input tokens
	fieldUsageOutputTotal = 3  // output tokens total (== field 9 + field 10); authoritative, do not re-derive
	fieldUsageCacheWrite  = 4  // cache-write tokens (inferred, rarely present)
	fieldUsageCacheRead   = 5  // cache-read tokens
	fieldUsageMessageID   = 7  // "bot-<uuid>" — the join key to steps.metadata
	fieldUsageResponseID  = 11 // response id (unused)

	// kv entry (repeated field 20 on the generation message).
	fieldKVKey   = 1
	fieldKVValue = 2

	// steps.metadata top level.
	fieldStepCreated = 1 // Timestamp submessage
	fieldStepUsage   = 9 // the same usage submessage shape as fieldGenUsage

	// Timestamp submessage.
	fieldTimestampSeconds = 1
	fieldTimestampNanos   = 2
)

// kvLastStepIndex is the key name agy uses, inside a generation message's
// repeated field 20, for the index of the step that produced this call —
// the fallback join key when a step's usage message id cannot be matched.
const kvLastStepIndex = "last_step_index"

// Call is one decoded gen_metadata row: a single model call's token usage
// plus the identifiers needed to resolve its timestamp and join it to a
// source.Record.
type Call struct {
	Model         string
	MessageID     string // "" when the generation carries no usage message id
	In            int64  // uncached input tokens
	Out           int64  // output tokens total (thinking + response, already summed by agy)
	CacheRead     int64
	CacheCreate   int64
	LastStepIndex string // decimal string kv, "" when absent
}

// DecodeGenMetadata decodes one gen_metadata.data blob.
//
// ok=false (with a nil error) means the row carries no usage message — a
// generation still in flight, or a non-model-call row — and the caller should
// skip it silently, the same way opencode skips non-assistant rows. A non-nil
// error means the bytes are not valid protobuf at all, which the caller
// treats as a malformed row: skip it, warn, and keep reading the rest of the
// file (see internal/agy/ingester.go).
func DecodeGenMetadata(data []byte) (Call, bool, error) {
	top, err := parseFields(data)
	if err != nil {
		return Call{}, false, err
	}
	genRaw, ok := firstBytes(top, fieldGenMessage)
	if !ok {
		return Call{}, false, nil
	}
	gen, err := parseFields(genRaw)
	if err != nil {
		return Call{}, false, err
	}
	usageRaw, ok := firstBytes(gen, fieldGenUsage)
	if !ok {
		return Call{}, false, nil
	}
	usage, err := parseFields(usageRaw)
	if err != nil {
		return Call{}, false, err
	}

	var c Call
	if v, ok := firstBytes(gen, fieldGenModelID); ok {
		c.Model = string(v)
	}
	if v, ok := firstVarint(usage, fieldUsageInput); ok {
		c.In = int64(v)
	}
	if v, ok := firstVarint(usage, fieldUsageOutputTotal); ok {
		c.Out = int64(v)
	}
	if v, ok := firstVarint(usage, fieldUsageCacheRead); ok {
		c.CacheRead = int64(v)
	}
	if v, ok := firstVarint(usage, fieldUsageCacheWrite); ok {
		c.CacheCreate = int64(v)
	}
	if v, ok := firstBytes(usage, fieldUsageMessageID); ok {
		c.MessageID = string(v)
	}

	for _, kvRaw := range allBytes(gen, fieldGenKV) {
		kv, err := parseFields(kvRaw)
		if err != nil {
			continue // one malformed kv entry does not invalidate the whole row
		}
		key, kok := firstBytes(kv, fieldKVKey)
		val, vok := firstBytes(kv, fieldKVValue)
		if !kok || !vok {
			continue
		}
		if string(key) == kvLastStepIndex {
			c.LastStepIndex = string(val)
		}
	}

	return c, true, nil
}

// DecodeStepMetadata decodes a steps.metadata blob.
//
// created is the step's Timestamp (field 1), zero-valued when absent. msgID
// is the message id inside the step's usage submessage (field 9.7), empty for
// a step that carries no usage message — most steps, per the verified schema.
// ok is false only on a genuine parse failure; an absent timestamp or message
// id is not an error, just a step with nothing to join on for this call.
func DecodeStepMetadata(b []byte) (msgID string, created time.Time, ok bool) {
	top, err := parseFields(b)
	if err != nil {
		return "", time.Time{}, false
	}
	if tsRaw, present := firstBytes(top, fieldStepCreated); present {
		if ts, terr := parseFields(tsRaw); terr == nil {
			sec, _ := firstVarint(ts, fieldTimestampSeconds)
			nanos, _ := firstVarint(ts, fieldTimestampNanos)
			created = time.Unix(int64(sec), int64(nanos)).UTC()
		}
	}
	if usageRaw, present := firstBytes(top, fieldStepUsage); present {
		if usage, uerr := parseFields(usageRaw); uerr == nil {
			if v, mok := firstBytes(usage, fieldUsageMessageID); mok {
				msgID = string(v)
			}
		}
	}
	return msgID, created, true
}

// reasoningEffortSuffixes are trailing markers agy appends for a model
// invoked at a particular reasoning effort. They bill as the base model.
var reasoningEffortSuffixes = []string{"-high", "-medium", "-low", "-minimal"}

// NormalizeModel maps a raw agy model id to a canonical pricing key.
//
// It is scoped to this package — internal/pricing/normalize.go already
// strips "-thinking" and other cross-vendor decorations, and touching it
// would affect every other source. Two agy-specific decorations remain after
// that: an experimental-variant tag ("-exp" or "-exp-<alnum>", e.g.
// "gemini-3.8-flash-exp-a") and a reasoning-effort suffix ("-high", "-medium",
// "-low", "-minimal"). Both are stripped repeatedly to a fixed point, so
// "foo-exp-high" (effort suffix nested inside an exp tag) also resolves.
//
// Pricing an "-exp" variant, or a specific reasoning-effort invocation, at its
// base model's rate is an equivalent-value inference: agy does not publish a
// separate price for either, and the base rate is the closest available
// estimate of what that call was worth. Everything else — including
// "-thinking", which pricing already handles — passes through unchanged.
func NormalizeModel(raw string) string {
	cur := raw
	for {
		if next, changed := stripExpTag(cur); changed {
			cur = next
			continue
		}
		if next, changed := stripReasoningEffort(cur); changed {
			cur = next
			continue
		}
		return cur
	}
}

// stripExpTag removes a trailing "-exp" or "-exp-<alnum>" tag.
func stripExpTag(s string) (string, bool) {
	if base, ok := strings.CutSuffix(s, "-exp"); ok && base != "" {
		return base, true
	}
	if i := strings.LastIndex(s, "-exp-"); i > 0 {
		suffix := s[i+len("-exp-"):]
		if suffix != "" && isAlnum(suffix) {
			return s[:i], true
		}
	}
	return s, false
}

// stripReasoningEffort removes one trailing reasoning-effort suffix.
func stripReasoningEffort(s string) (string, bool) {
	for _, suf := range reasoningEffortSuffixes {
		if base, ok := strings.CutSuffix(s, suf); ok && base != "" {
			return base, true
		}
	}
	return s, false
}

func isAlnum(s string) bool {
	for _, r := range s {
		alpha := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		digit := r >= '0' && r <= '9'
		if !alpha && !digit {
			return false
		}
	}
	return true
}

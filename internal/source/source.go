// Package source defines the port types for multi-source ingestion.
// Adapters for Claude (JSONL tail), Codex (JSONL tail), and opencode (DB poll)
// all implement these interfaces; the store-backed Sink handles pricing and
// persistence behind the Emit abstraction.
package source

import (
	"context"
	"time"
)

// Name is the canonical, lowercase source key stamped on every event.
type Name string

const (
	Claude   Name = "claude"
	Codex    Name = "codex"
	Opencode Name = "opencode"
)

// String returns the string form of the source name.
func (n Name) String() string { return string(n) }

// Record is the source-agnostic ingestion unit handed to the Sink.
// It carries everything store.Insert needs, plus the Source tag and raw
// token counts so pricing can be computed in one place (the Sink).
type Record struct {
	Source      Name
	UUID        string // deterministic, source-prefixed for non-Claude sources
	SessionID   string // source-prefixed for non-Claude sources
	CWD         string // never empty (fallback derivation must happen before Emit)
	Type        string // "assistant" | "user"
	Model       string // canonical pricing key; empty for user events
	TS          time.Time
	In          int64 // non-cached input tokens
	Out         int64 // output tokens (includes reasoning for OpenAI)
	CacheRead   int64
	CacheCreate int64
}

// Sink decouples source adapters from the concrete store. Implemented by
// StoreSink (internal/source/sink.go) which applies pricing + calls store.Insert.
type Sink interface {
	Emit(ctx context.Context, r Record) error
}

// Ingester is the DRIVING port every source adapter implements.
// Mirrors collector.IngestExisting/Watch so the Claude adapter is a
// pure refactor with zero behavior change.
type Ingester interface {
	Name() Name
	IngestExisting(ctx context.Context) error // warm start, honors watermark
	Watch(ctx context.Context) error          // live; returns on ctx.Done
}

// LineParser is the narrow port for append-only JSONL adapters only.
// The Claude ClaudeLineParser and Codex parser both implement this interface.
// Returns (nil, nil) for lines that produce nothing (unknown types, no-usage
// lines) — never errors on version/schema drift (permissive contract).
type LineParser interface {
	ParseLine(line []byte, fileCtx LineContext) ([]Record, error)
}

// LineContext carries per-file state that the parser needs to synthesize IDs
// and derive cwd/session information.
type LineContext struct {
	Path        string // absolute path to the JSONL file
	LineOffset  int64  // byte offset of this line's start (for deterministic UUID synthesis)
	SessionUUID string // parsed from filename for sources without per-event session IDs
	DefaultCWD  string // fallback project key for this file/source
}

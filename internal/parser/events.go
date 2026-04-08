// Package parser decodes a single Claude Code JSONL line into a typed event.
// The parser is permissive: unknown event types are returned as UnknownEvent
// and never cause an error, so the collector can keep ingesting across CLI
// version drift.
package parser

import (
	"encoding/json"
	"errors"
	"time"
)

// Event is the common interface for any decoded line.
type Event interface {
	Kind() string
	Timestamp() time.Time
	SessionID() string
}

// Common holds fields present on most events.
type Common struct {
	Type    string    `json:"type"`
	UUID    string    `json:"uuid"`
	Session string    `json:"sessionId"`
	CWD     string    `json:"cwd"`
	TS      time.Time `json:"timestamp"`
	Version string    `json:"version"`
}

func (c Common) Kind() string         { return c.Type }
func (c Common) Timestamp() time.Time { return c.TS }
func (c Common) SessionID() string    { return c.Session }

// AssistantEvent carries the token usage breakdown — the only event class
// that produces a non-zero cost.
type AssistantEvent struct {
	Common
	Model             string
	InTokens          int64
	OutTokens         int64
	CacheReadTokens   int64
	CacheCreateTokens int64
}

// UserEvent represents a user prompt (no token cost).
type UserEvent struct {
	Common
}

// UnknownEvent is returned for any `type` the parser does not recognize.
// The collector logs once and continues.
type UnknownEvent struct {
	Common
	Raw json.RawMessage
}

// rawAssistant is the JSON shape of an assistant line.
type rawAssistant struct {
	Common
	Message struct {
		Model string `json:"model"`
		Usage struct {
			Input        int64 `json:"input_tokens"`
			Output       int64 `json:"output_tokens"`
			CacheCreate  int64 `json:"cache_creation_input_tokens"`
			CacheRead    int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// ParseLine decodes a single JSONL line.
// Returns (event, nil) on success, including UnknownEvent for unknown types.
// Returns (nil, err) only when the line is not valid JSON or lacks `type`.
func ParseLine(b []byte) (Event, error) {
	if len(b) == 0 {
		return nil, errors.New("empty line")
	}
	var c Common
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	switch c.Type {
	case "assistant":
		var r rawAssistant
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, err
		}
		return AssistantEvent{
			Common:            r.Common,
			Model:             r.Message.Model,
			InTokens:          r.Message.Usage.Input,
			OutTokens:         r.Message.Usage.Output,
			CacheReadTokens:   r.Message.Usage.CacheRead,
			CacheCreateTokens: r.Message.Usage.CacheCreate,
		}, nil
	case "user":
		return UserEvent{Common: c}, nil
	case "":
		return nil, errors.New("missing type field")
	default:
		return UnknownEvent{Common: c, Raw: append([]byte(nil), b...)}, nil
	}
}

// SupportedVersionRange is the inclusive range we know how to parse safely.
// Events outside this range still parse, but the collector surfaces a warning.
var (
	MinSupportedVersion = "2.1.0"
	MaxSupportedVersion = "2.2.0" // exclusive upper bound
)

// VersionInRange reports whether v is within [Min, Max). Empty v → true.
func VersionInRange(v string) bool {
	if v == "" {
		return true
	}
	return semverLE(MinSupportedVersion, v) && semverLT(v, MaxSupportedVersion)
}

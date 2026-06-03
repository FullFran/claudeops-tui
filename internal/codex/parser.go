// Package codex implements source.LineParser for Codex CLI rollout JSONL files.
//
// IMPORTANT: The RolloutLine schema implemented here is spec-derived.
// It MUST be validated against real ~/.codex/sessions/**/rollout-*.jsonl files
// before trusting the parser in production. Codex CLI was NOT installed on the
// development machine; all fixtures are hand-built from the documented schema.
//
// OpenAI model pricing: prices are NOT baked into this package. The parser
// passes model IDs through to pricing.CostFor; unknown models ingest with nil
// cost (tokens still recorded). Add verified OpenAI EUR prices to pricing.seed.toml
// — do NOT add guessed prices here. USD→EUR conversion requires separate
// verification against https://openai.com/pricing.
package codex

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/fullfran/claudeops-tui/internal/source"
)

// rolloutLine is the outer envelope of every line in a Codex rollout file.
type rolloutLine struct {
	Timestamp time.Time       `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

// turnContextPayload is the payload for a "turn_context" line.
// It sets the active model (and optionally cwd) for subsequent token_count lines.
type turnContextPayload struct {
	Model string `json:"model"`
	CWD   string `json:"cwd"`
}

// tokenInfo holds token usage for a single turn or the session cumulative total.
type tokenInfo struct {
	InputTokens           int64 `json:"input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
}

// tokenCountPayload is the payload for a "token_count" line.
// last_token_usage (per-turn) is preferred over total_token_usage (cumulative).
type tokenCountInfo struct {
	LastTokenUsage  *tokenInfo `json:"last_token_usage"`
	TotalTokenUsage *tokenInfo `json:"total_token_usage"`
}

type tokenCountPayload struct {
	Info tokenCountInfo `json:"info"`
}

// sessionState tracks per-session mutable state across lines within one file.
// The Codex format requires this because model identity and cumulative token
// totals are carried in separate lines from the token_count events.
type sessionState struct {
	activeModel string
	activeCWD   string
	prevCumIn   int64
	prevCumOut  int64
}

// Parser implements source.LineParser for Codex CLI rollout JSONL files.
// One Parser instance may be shared across multiple files for the same source;
// state is keyed by sessionUUID so concurrent file processing is safe.
type Parser struct {
	mu       sync.Mutex
	sessions map[string]*sessionState
	warnOnce map[string]bool
}

// NewParser creates a new Codex Parser.
func NewParser() *Parser {
	return &Parser{
		sessions: make(map[string]*sessionState),
		warnOnce: make(map[string]bool),
	}
}

// ParseLine implements source.LineParser.
// Returns (nil, nil) for lines that produce no Records (turn_context, unknown
// types). Returns (nil, err) only on JSON decode failure. Never panics on
// schema drift.
func (p *Parser) ParseLine(line []byte, ctx source.LineContext) ([]source.Record, error) {
	var env rolloutLine
	if err := json.Unmarshal(line, &env); err != nil {
		return nil, fmt.Errorf("codex: parse envelope: %w", err)
	}

	switch env.Type {
	case "turn_context":
		p.handleTurnContext(ctx.SessionUUID, env.Payload, ctx.DefaultCWD)
		return nil, nil

	case "token_count":
		return p.handleTokenCount(env.Timestamp, ctx, env.Payload)

	default:
		// Soft-fail: unknown type — warn once, return nil, nil (permissive contract).
		p.warnOnceFor(env.Type)
		return nil, nil
	}
}

// handleTurnContext decodes the turn_context payload and updates the session's
// active model and cwd. The model set here applies to all subsequent token_count
// lines for this sessionUUID until the next turn_context.
func (p *Parser) handleTurnContext(sessionUUID string, payload json.RawMessage, defaultCWD string) {
	var tc turnContextPayload
	if err := json.Unmarshal(payload, &tc); err != nil {
		// Soft-fail: can't decode turn_context payload — leave state unchanged.
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.getOrCreateSession(sessionUUID)
	if tc.Model != "" {
		s.activeModel = tc.Model
	}
	if tc.CWD != "" {
		s.activeCWD = tc.CWD
	} else if s.activeCWD == "" {
		s.activeCWD = defaultCWD
	}
}

// handleTokenCount decodes a token_count payload and produces a source.Record.
// Token delta logic:
//   - Prefer last_token_usage (per-turn exact delta, no cumulative arithmetic needed).
//   - Fall back to delta-subtract total_token_usage (cumulative) minus previous
//     cumulative for this session, clamping underflows to 0.
func (p *Parser) handleTokenCount(ts time.Time, ctx source.LineContext, payload json.RawMessage) ([]source.Record, error) {
	var tcp tokenCountPayload
	if err := json.Unmarshal(payload, &tcp); err != nil {
		// Soft-fail: malformed token_count payload.
		return nil, nil
	}

	p.mu.Lock()
	s := p.getOrCreateSession(ctx.SessionUUID)

	model := s.activeModel
	if model == "" {
		// No turn_context seen yet — use fallback model name.
		// Cost will be nil (unknown model), but tokens are still recorded.
		model = "unknown"
	}

	cwd := s.activeCWD
	if cwd == "" {
		cwd = ctx.DefaultCWD
	}
	if cwd == "" {
		cwd = "codex:" + ctx.SessionUUID
	}

	var deltaIn, deltaOut, cacheRead int64

	if lu := tcp.Info.LastTokenUsage; lu != nil {
		// Preferred path: per-turn exact delta from last_token_usage.
		// OpenAI token field mapping:
		//   input_tokens includes cached_input_tokens → non-cached = input - cached
		//   reasoning_output_tokens is part of output (fold into Out)
		//   cache_create = 0 (Codex/OpenAI has no cache-write tier)
		deltaIn = clamp(lu.InputTokens - lu.CachedInputTokens)
		cacheRead = clamp(lu.CachedInputTokens)
		deltaOut = clamp(lu.OutputTokens + lu.ReasoningOutputTokens)
	} else if tu := tcp.Info.TotalTokenUsage; tu != nil {
		// Fallback path: delta-subtract cumulative total_token_usage.
		// This prevents the 91x inflation bug (ccusage #950) where summing
		// cumulative totals across turns inflates the session token count.
		newCumIn := tu.InputTokens
		newCumOut := tu.OutputTokens
		deltaIn = clamp(newCumIn - s.prevCumIn)
		deltaOut = clamp(newCumOut - s.prevCumOut)
		s.prevCumIn = newCumIn
		s.prevCumOut = newCumOut
	} else {
		// No usage fields at all — emit nothing.
		p.mu.Unlock()
		return nil, nil
	}

	p.mu.Unlock()

	uuid := SynthesizeUUID(ctx.SessionUUID, ctx.LineOffset)
	sessionID := "codex:" + ctx.SessionUUID

	r := source.Record{
		Source:      source.Codex,
		UUID:        uuid,
		SessionID:   sessionID,
		CWD:         cwd,
		Type:        "assistant",
		Model:       model,
		TS:          ts,
		In:          deltaIn,
		Out:         deltaOut,
		CacheRead:   cacheRead,
		CacheCreate: 0, // Codex/OpenAI has no cache-write tier; always 0
	}
	return []source.Record{r}, nil
}

// getOrCreateSession returns the session state for sessionUUID.
// Caller must hold p.mu.
func (p *Parser) getOrCreateSession(sessionUUID string) *sessionState {
	s, ok := p.sessions[sessionUUID]
	if !ok {
		s = &sessionState{}
		p.sessions[sessionUUID] = s
	}
	return s
}

// warnOnceFor logs an unknown line type once per Parser instance.
func (p *Parser) warnOnceFor(lineType string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.warnOnce[lineType] {
		return
	}
	p.warnOnce[lineType] = true
	// Soft warning: unknown Codex rollout line type. Skipping.
	// This is intentional — the Codex format may add new types in future versions.
	_ = lineType // suppress unused warning; real impl would log to stderr
}

// clamp returns max(0, v) to guard against underflow on malformed lines.
func clamp(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

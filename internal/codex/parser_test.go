// Package codex implements a source.LineParser for Codex CLI rollout JSONL files.
//
// IMPORTANT: The RolloutLine schema and fixture data in these tests are
// spec-derived from the design document. They MUST be validated against a real
// ~/.codex/sessions/**/rollout-*.jsonl before trusting this parser in production.
// Codex CLI is NOT installed on the development machine; fixtures are hand-built.
package codex

import (
	"fmt"
	"testing"

	"github.com/fullfran/claudeops-tui/internal/source"
)

// Compile-time check: Parser satisfies source.LineParser.
var _ source.LineParser = &Parser{}

// --- T2.1: RolloutLine envelope decode + soft-fail unknown types ---

func TestRolloutLineEnvelopeDecode(t *testing.T) {
	p := NewParser()
	ctx := source.LineContext{
		Path:        "/home/user/.codex/sessions/2026/01/15/rollout-20260115T120000Z-abc123.jsonl",
		LineOffset:  0,
		SessionUUID: "abc123",
		DefaultCWD:  "/home/user/myproject",
	}

	tests := []struct {
		name      string
		line      []byte
		wantNil   bool
		wantError bool
	}{
		{
			name: "REQ-2.1.1: known type turn_context parses without error",
			// turn_context is a known type; it sets state and emits no Record.
			line:    []byte(`{"timestamp":"2026-01-15T12:00:00Z","type":"turn_context","payload":{"model":"gpt-4o","cwd":"/home/user/myproject"}}`),
			wantNil: true,
		},
		{
			name:    "REQ-2.1.2: unknown type returns nil, nil — no panic",
			line:    []byte(`{"timestamp":"2026-01-15T12:00:01Z","type":"future_type","payload":{}}`),
			wantNil: true,
		},
		{
			name:      "bad JSON returns error",
			line:      []byte(`{not valid json`),
			wantError: true,
		},
		{
			name:    "unknown extension type with null payload is soft-fail",
			line:    []byte(`{"timestamp":"2026-01-15T12:00:02Z","type":"unknown_future_extension","payload":null}`),
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records, err := p.ParseLine(tt.line, ctx)
			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil && len(records) != 0 {
				t.Errorf("expected 0 records, got %d", len(records))
			}
		})
	}
}

// --- T2.2: Active model state machine ---

func makeCtx(sessionUUID string) source.LineContext {
	return source.LineContext{
		Path:        "/home/user/.codex/sessions/2026/01/15/rollout-20260115T120000Z-" + sessionUUID + ".jsonl",
		LineOffset:  0,
		SessionUUID: sessionUUID,
		DefaultCWD:  "/home/user/myproject",
	}
}

func TestActiveModelStateMachine(t *testing.T) {
	t.Run("REQ-2.2.1: turn_context sets model for subsequent token_count", func(t *testing.T) {
		p := NewParser()
		lctx := makeCtx("sess-2.2.1")

		tcLine := []byte(`{"timestamp":"2026-01-15T12:00:00Z","type":"turn_context","payload":{"model":"gpt-4o","cwd":"/home/user/myproject"}}`)
		lctx.LineOffset = 0
		_, _ = p.ParseLine(tcLine, lctx)

		tokenLine := makeTokenCountLineWithFields(100, 0, 50, 0)
		lctx.LineOffset = int64(len(tcLine) + 1)
		records, err := p.ParseLine(tokenLine, lctx)
		if err != nil {
			t.Fatalf("ParseLine: %v", err)
		}
		if len(records) != 1 {
			t.Fatalf("want 1 record, got %d", len(records))
		}
		if records[0].Model != "gpt-4o" {
			t.Errorf("want Model=gpt-4o, got %q", records[0].Model)
		}
	})

	t.Run("REQ-2.2.2: model carries across multiple token_count lines", func(t *testing.T) {
		p := NewParser()
		lctx := makeCtx("sess-2.2.2")

		tcLine := []byte(`{"timestamp":"2026-01-15T12:00:00Z","type":"turn_context","payload":{"model":"gpt-4o","cwd":"/home/user/myproject"}}`)
		lctx.LineOffset = 0
		_, _ = p.ParseLine(tcLine, lctx)

		tokenLine := makeTokenCountLineWithFields(100, 0, 50, 0)
		for i := 0; i < 3; i++ {
			lctx.LineOffset = int64(len(tcLine)+1) + int64(i)*int64(len(tokenLine)+1)
			records, err := p.ParseLine(tokenLine, lctx)
			if err != nil {
				t.Fatalf("ParseLine i=%d: %v", i, err)
			}
			if len(records) < 1 {
				t.Fatalf("i=%d: want ≥1 record, got %d", i, len(records))
			}
			if records[0].Model != "gpt-4o" {
				t.Errorf("i=%d: want Model=gpt-4o, got %q", i, records[0].Model)
			}
		}
	})

	t.Run("REQ-2.2.3: model updated on second turn_context", func(t *testing.T) {
		p := NewParser()
		lctx := makeCtx("sess-2.2.3")

		tc1 := []byte(`{"timestamp":"2026-01-15T12:00:00Z","type":"turn_context","payload":{"model":"model-A","cwd":"/home/user/myproject"}}`)
		lctx.LineOffset = 0
		_, _ = p.ParseLine(tc1, lctx)

		tc2 := []byte(`{"timestamp":"2026-01-15T12:00:01Z","type":"turn_context","payload":{"model":"model-B","cwd":"/home/user/myproject"}}`)
		lctx.LineOffset = int64(len(tc1) + 1)
		_, _ = p.ParseLine(tc2, lctx)

		tokenLine := makeTokenCountLineWithFields(100, 0, 50, 0)
		lctx.LineOffset = int64(len(tc1)+1) + int64(len(tc2)+1)
		records, err := p.ParseLine(tokenLine, lctx)
		if err != nil {
			t.Fatalf("ParseLine: %v", err)
		}
		if len(records) < 1 {
			t.Fatalf("want ≥1 record, got %d", len(records))
		}
		if records[0].Model != "model-B" {
			t.Errorf("want Model=model-B, got %q", records[0].Model)
		}
	})

	t.Run("REQ-2.2.4: missing model → fallback or skip, no panic", func(t *testing.T) {
		p := NewParser()
		lctx := makeCtx("sess-2.2.4")

		// token_count before any turn_context — must not panic
		tokenLine := makeTokenCountLineWithFields(10, 0, 5, 0)
		lctx.LineOffset = 0
		records, err := p.ParseLine(tokenLine, lctx)
		if err != nil {
			t.Fatalf("should not error: %v", err)
		}
		// If a record was emitted, model must be non-empty (fallback to "unknown")
		for _, r := range records {
			if r.Model == "" {
				t.Error("model must not be empty when no turn_context seen")
			}
		}
	})
}

// --- T2.3: Cumulative→delta deduplication (91x guard) ---

func TestCumulativeDelta(t *testing.T) {
	t.Run("REQ-2.3.1: three cumulative lines — sum == last cumulative (not sum of cumulatives)", func(t *testing.T) {
		// No last_token_usage → force delta-subtract path from total_token_usage.
		// Cumulatives: in=100/250/310, out=40/90/120
		// Sum of deltas must equal last cumulative: in=310, out=120 (NOT 660/250).
		p := NewParser()
		lctx := source.LineContext{
			Path:        "/home/user/.codex/sessions/2026/01/15/rollout-20260115T120000Z-sess91x.jsonl",
			SessionUUID: "sess91x",
			DefaultCWD:  "/home/user/proj",
		}

		cumulatives := [][2]int64{{100, 40}, {250, 90}, {310, 120}}
		offset := int64(0)
		var totalIn, totalOut int64
		for _, c := range cumulatives {
			line := makeTokenCountLineCumulative(c[0], c[1])
			lctx.LineOffset = offset
			records, err := p.ParseLine(line, lctx)
			if err != nil {
				t.Fatalf("ParseLine: %v", err)
			}
			offset += int64(len(line)) + 1
			for _, r := range records {
				totalIn += r.In
				totalOut += r.Out
			}
		}

		if totalIn != 310 {
			t.Errorf("REQ-2.3.1: totalIn want 310, got %d (91x guard failed: wrong=%d)", totalIn, 100+250+310)
		}
		if totalOut != 120 {
			t.Errorf("REQ-2.3.1: totalOut want 120, got %d", totalOut)
		}
	})

	t.Run("REQ-2.3.2: single token_count line produces its own value", func(t *testing.T) {
		p := NewParser()
		lctx := source.LineContext{
			Path:        "/home/user/.codex/sessions/2026/01/15/rollout-20260115T120000Z-sess232.jsonl",
			SessionUUID: "sess232",
			LineOffset:  0,
			DefaultCWD:  "/home/user/proj",
		}

		line := makeTokenCountLineCumulative(150, 60)
		records, err := p.ParseLine(line, lctx)
		if err != nil {
			t.Fatalf("ParseLine: %v", err)
		}
		if len(records) != 1 {
			t.Fatalf("want 1 record, got %d", len(records))
		}
		if records[0].In != 150 {
			t.Errorf("want In=150, got %d", records[0].In)
		}
	})

	t.Run("REQ-2.3.3: first-line delta equals cumulative", func(t *testing.T) {
		p := NewParser()
		lctx := source.LineContext{
			Path:        "/home/user/.codex/sessions/2026/01/15/rollout-20260115T120000Z-sess233.jsonl",
			SessionUUID: "sess233",
			LineOffset:  0,
			DefaultCWD:  "/home/user/proj",
		}

		line := makeTokenCountLineCumulative(80, 30)
		records, err := p.ParseLine(line, lctx)
		if err != nil {
			t.Fatalf("ParseLine: %v", err)
		}
		if len(records) != 1 {
			t.Fatalf("want 1 record, got %d", len(records))
		}
		if records[0].In != 80 {
			t.Errorf("first-line delta: want In=80, got %d", records[0].In)
		}
	})

	t.Run("REQ-2.3.4: re-tail same offset produces same UUID (idempotent dedup key)", func(t *testing.T) {
		p := NewParser()
		lctx := source.LineContext{
			Path:        "/home/user/.codex/sessions/2026/01/15/rollout-20260115T120000Z-sess234.jsonl",
			SessionUUID: "sess234",
			DefaultCWD:  "/home/user/proj",
		}

		tcLine := []byte(`{"timestamp":"2026-01-15T12:00:00Z","type":"turn_context","payload":{"model":"gpt-4o","cwd":"/home/user/myproject"}}`)
		lctx.LineOffset = 0
		_, _ = p.ParseLine(tcLine, lctx)

		tokenLine := makeTokenCountLineWithFields(100, 0, 50, 0)
		lctx.LineOffset = int64(len(tcLine) + 1)

		var uuid1, uuid2 string
		for pass := 0; pass < 2; pass++ {
			records, err := p.ParseLine(tokenLine, lctx)
			if err != nil {
				t.Fatalf("pass %d: %v", pass, err)
			}
			if len(records) > 0 {
				if pass == 0 {
					uuid1 = records[0].UUID
				} else {
					uuid2 = records[0].UUID
				}
			}
		}
		if uuid1 != "" && uuid2 != "" && uuid1 != uuid2 {
			t.Errorf("re-tail must produce same UUID: %q vs %q", uuid1, uuid2)
		}
	})
}

// --- T2.4: OpenAI→4-class token mapping ---

func TestTokenFieldMapping(t *testing.T) {
	p := NewParser()
	tc := []byte(`{"timestamp":"2026-01-15T12:00:00Z","type":"turn_context","payload":{"model":"gpt-4o","cwd":"/home/user/myproject"}}`)
	lctx := makeCtx("sesstm")
	lctx.LineOffset = 0
	_, _ = p.ParseLine(tc, lctx)
	baseOffset := int64(len(tc) + 1)

	t.Run("REQ-2.4.1: non-cached input = input - cached_input", func(t *testing.T) {
		// input=1000, cached=200 → In=800, CacheRead=200
		line := makeTokenCountLineWithFields(1000, 200, 300, 0)
		lctx.LineOffset = baseOffset
		records, err := p.ParseLine(line, lctx)
		if err != nil {
			t.Fatalf("ParseLine: %v", err)
		}
		if len(records) != 1 {
			t.Fatalf("want 1 record, got %d", len(records))
		}
		r := records[0]
		if r.In != 800 {
			t.Errorf("In want 800 (1000-200), got %d", r.In)
		}
		if r.CacheRead != 200 {
			t.Errorf("CacheRead want 200, got %d", r.CacheRead)
		}
	})

	t.Run("REQ-2.4.2: reasoning tokens counted as output", func(t *testing.T) {
		// output=500, reasoning=100 → Out >= 600
		line := makeTokenCountLineWithFields(1000, 0, 500, 100)
		lctx.LineOffset = baseOffset + 1
		records, err := p.ParseLine(line, lctx)
		if err != nil {
			t.Fatalf("ParseLine: %v", err)
		}
		if len(records) != 1 {
			t.Fatalf("want 1 record, got %d", len(records))
		}
		if records[0].Out < 600 {
			t.Errorf("Out want >=600 (output+reasoning), got %d", records[0].Out)
		}
	})

	t.Run("REQ-2.4.3: cache_create always zero", func(t *testing.T) {
		line := makeTokenCountLineWithFields(100, 0, 50, 0)
		lctx.LineOffset = baseOffset + 2
		records, err := p.ParseLine(line, lctx)
		if err != nil {
			t.Fatalf("ParseLine: %v", err)
		}
		if len(records) != 1 {
			t.Fatalf("want 1 record, got %d", len(records))
		}
		if records[0].CacheCreate != 0 {
			t.Errorf("CacheCreate must be 0, got %d", records[0].CacheCreate)
		}
	})
}

// --- fixture helpers ---

// makeTokenCountLineCumulative builds a token_count line with ONLY total_token_usage
// (no last_token_usage), forcing the cumulative delta-subtract path.
func makeTokenCountLineCumulative(cumIn, cumOut int64) []byte {
	return []byte(fmt.Sprintf(
		`{"timestamp":"2026-01-15T12:00:00Z","type":"token_count","payload":{"info":{"total_token_usage":{"input_tokens":%d,"output_tokens":%d}}}}`,
		cumIn, cumOut,
	))
}

// makeTokenCountLineWithFields builds a token_count line WITH last_token_usage
// populated, which is the preferred per-turn delta path.
func makeTokenCountLineWithFields(inputTokens, cachedInput, outputTokens, reasoningTokens int64) []byte {
	return []byte(fmt.Sprintf(
		`{"timestamp":"2026-01-15T12:00:00Z","type":"token_count","payload":{"info":{"last_token_usage":{"input_tokens":%d,"cached_input_tokens":%d,"output_tokens":%d,"reasoning_output_tokens":%d},"total_token_usage":{"input_tokens":%d,"output_tokens":%d}}}}`,
		inputTokens, cachedInput, outputTokens, reasoningTokens,
		inputTokens, outputTokens,
	))
}

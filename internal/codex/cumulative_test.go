package codex

import (
	"fmt"
	"testing"

	"github.com/fullfran/claudeops-tui/internal/source"
)

// makeTokenCountLineTotals builds a token_count line with ONLY total_token_usage,
// including the cached and reasoning fields.
func makeTokenCountLineTotals(cumIn, cumCached, cumOut, cumReasoning int64) []byte {
	return []byte(fmt.Sprintf(
		`{"timestamp":"2026-01-15T12:00:00Z","type":"token_count","payload":{"info":{"total_token_usage":{"input_tokens":%d,"cached_input_tokens":%d,"output_tokens":%d,"reasoning_output_tokens":%d}}}}`,
		cumIn, cumCached, cumOut, cumReasoning,
	))
}

// makeTokenCountLineBoth builds a token_count line carrying both the per-turn
// usage and the running session totals, as real Codex rollouts do.
func makeTokenCountLineBoth(lastIn, lastCached, lastOut, lastReasoning, cumIn, cumCached, cumOut, cumReasoning int64) []byte {
	return []byte(fmt.Sprintf(
		`{"timestamp":"2026-01-15T12:00:00Z","type":"token_count","payload":{"info":{"last_token_usage":{"input_tokens":%d,"cached_input_tokens":%d,"output_tokens":%d,"reasoning_output_tokens":%d},"total_token_usage":{"input_tokens":%d,"cached_input_tokens":%d,"output_tokens":%d,"reasoning_output_tokens":%d}}}}`,
		lastIn, lastCached, lastOut, lastReasoning,
		cumIn, cumCached, cumOut, cumReasoning,
	))
}

// parseAt feeds one line at the given offset and returns the single record it
// produced, or nil when the line emitted nothing.
func parseAt(t *testing.T, p *Parser, sessionUUID string, offset int64, line []byte) *source.Record {
	t.Helper()
	lctx := makeCtx(sessionUUID)
	lctx.LineOffset = offset
	records, err := p.ParseLine(line, lctx)
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if len(records) == 0 {
		return nil
	}
	if len(records) != 1 {
		t.Fatalf("want at most 1 record, got %d", len(records))
	}
	return &records[0]
}

// TestCumulativeBaselineTracksLastTokenUsage covers #39: the baseline must also
// advance on the per-turn path, or a later total-only line re-counts everything.
func TestCumulativeBaselineTracksLastTokenUsage(t *testing.T) {
	p := NewParser()
	const sess = "sess-mixed"

	if r := parseAt(t, p, sess, 0, makeTokenCountLineBoth(100, 0, 50, 0, 100, 0, 50, 0)); r == nil || r.In != 100 {
		t.Fatalf("first per-turn line: want In=100, got %+v", r)
	}
	if r := parseAt(t, p, sess, 200, makeTokenCountLineBoth(120, 0, 60, 0, 220, 0, 110, 0)); r == nil || r.In != 120 {
		t.Fatalf("second per-turn line: want In=120, got %+v", r)
	}

	// Format drift: a line carrying only the running totals.
	r := parseAt(t, p, sess, 400, makeTokenCountLineTotals(300, 0, 150, 0))
	if r == nil {
		t.Fatal("total-only line produced no record")
	}
	if r.In != 80 {
		t.Errorf("In: want 80 (300-220), got %d", r.In)
	}
	if r.Out != 40 {
		t.Errorf("Out: want 40 (150-110), got %d", r.Out)
	}
}

// TestCumulativeFallbackSplitsCachedAndReasoning covers #39: the cumulative path
// must map token fields exactly like the per-turn path.
func TestCumulativeFallbackSplitsCachedAndReasoning(t *testing.T) {
	tests := []struct {
		name                        string
		offset                      int64
		line                        []byte
		wantIn, wantOut, wantCached int64
	}{
		{
			name:   "first cumulative line",
			offset: 0,
			line:   makeTokenCountLineTotals(1000, 200, 300, 100),
			wantIn: 800, wantOut: 400, wantCached: 200,
		},
		{
			name:   "second cumulative line deltas every field",
			offset: 300,
			line:   makeTokenCountLineTotals(1500, 400, 500, 150),
			wantIn: 300, wantOut: 250, wantCached: 200,
		},
	}

	p := NewParser()
	const sess = "sess-fields"
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := parseAt(t, p, sess, tc.offset, tc.line)
			if r == nil {
				t.Fatal("no record produced")
			}
			if r.In != tc.wantIn {
				t.Errorf("In: want %d got %d", tc.wantIn, r.In)
			}
			if r.Out != tc.wantOut {
				t.Errorf("Out: want %d got %d", tc.wantOut, r.Out)
			}
			if r.CacheRead != tc.wantCached {
				t.Errorf("CacheRead: want %d got %d", tc.wantCached, r.CacheRead)
			}
		})
	}
}

// TestCumulativeBaselineOnResume covers #39: parser state is lost on restart
// while file offsets persist, so the first total-only line of a resumed file
// must seed the baseline instead of re-counting the whole session.
func TestCumulativeBaselineOnResume(t *testing.T) {
	t.Run("resumed mid-file", func(t *testing.T) {
		p := NewParser()
		const sess = "sess-resume"

		if r := parseAt(t, p, sess, 4096, makeTokenCountLineTotals(5000, 0, 2000, 0)); r != nil {
			t.Errorf("want no record for the baseline line, got %+v", r)
		}
		r := parseAt(t, p, sess, 4300, makeTokenCountLineTotals(5200, 0, 2100, 0))
		if r == nil {
			t.Fatal("no record produced after the baseline line")
		}
		if r.In != 200 || r.Out != 100 {
			t.Errorf("want In=200 Out=100, got In=%d Out=%d", r.In, r.Out)
		}
	})

	t.Run("read from the start of the file", func(t *testing.T) {
		p := NewParser()
		const sess = "sess-start"

		tcLine := []byte(`{"timestamp":"2026-01-15T12:00:00Z","type":"turn_context","payload":{"model":"gpt-5","cwd":"/home/u/p"}}`)
		if r := parseAt(t, p, sess, 0, tcLine); r != nil {
			t.Fatalf("turn_context should emit nothing, got %+v", r)
		}
		r := parseAt(t, p, sess, 120, makeTokenCountLineTotals(400, 0, 200, 0))
		if r == nil {
			t.Fatal("no record produced")
		}
		if r.In != 400 || r.Out != 200 {
			t.Errorf("want the full cumulative In=400 Out=200, got In=%d Out=%d", r.In, r.Out)
		}
	})
}

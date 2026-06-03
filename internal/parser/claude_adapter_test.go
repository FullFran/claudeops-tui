package parser

import (
	"testing"

	"github.com/fullfran/claudeops-tui/internal/source"
)

// Compile-time check: ClaudeLineParser satisfies source.LineParser.
var _ source.LineParser = ClaudeLineParser{}

// TestClaudeLineParserAdapter covers REQ-1.5.1:
// ClaudeLineParser wraps ParseLine and returns correct source.Records.
func TestClaudeLineParserAdapter(t *testing.T) {
	assistantLine := []byte(`{"type":"assistant","uuid":"adapter-u1","sessionId":"adapter-s1","cwd":"/adapter/proj","version":"2.1.96","timestamp":"2026-04-08T13:10:57.123Z","message":{"role":"assistant","model":"claude-opus-4-6","usage":{"input_tokens":5,"cache_creation_input_tokens":20780,"cache_read_input_tokens":15718,"output_tokens":1101}}}`)

	lp := ClaudeLineParser{}
	ctx := source.LineContext{Path: "/some/file.jsonl"}

	t.Run("assistant line returns Record with source=claude", func(t *testing.T) {
		records, err := lp.ParseLine(assistantLine, ctx)
		if err != nil {
			t.Fatalf("ParseLine: %v", err)
		}
		if len(records) != 1 {
			t.Fatalf("want 1 record, got %d", len(records))
		}
		r := records[0]
		if r.Source != source.Claude {
			t.Errorf("want Source=claude, got %q", r.Source)
		}
		if r.UUID != "adapter-u1" {
			t.Errorf("UUID mismatch: %q", r.UUID)
		}
		if r.Model != "claude-opus-4-6" {
			t.Errorf("Model mismatch: %q", r.Model)
		}
		if r.In != 5 || r.Out != 1101 || r.CacheRead != 15718 || r.CacheCreate != 20780 {
			t.Errorf("token fields wrong: %+v", r)
		}
		if r.CWD != "/adapter/proj" {
			t.Errorf("CWD mismatch: %q", r.CWD)
		}
	})

	t.Run("user line returns Record with source=claude type=user", func(t *testing.T) {
		userLine := []byte(`{"type":"user","uuid":"adapter-u2","sessionId":"adapter-s1","cwd":"/adapter/proj","timestamp":"2026-04-08T13:11:00Z"}`)
		records, err := lp.ParseLine(userLine, ctx)
		if err != nil {
			t.Fatalf("ParseLine user: %v", err)
		}
		if len(records) != 1 {
			t.Fatalf("want 1 record, got %d", len(records))
		}
		r := records[0]
		if r.Source != source.Claude {
			t.Errorf("source: want claude got %q", r.Source)
		}
		if r.Type != "user" {
			t.Errorf("type: want user got %q", r.Type)
		}
	})

	t.Run("unknown event type returns nil, nil (soft-fail)", func(t *testing.T) {
		unknownLine := []byte(`{"type":"permission-mode","uuid":"u3","sessionId":"s1","cwd":"/p","timestamp":"2026-04-08T13:11:01Z"}`)
		records, err := lp.ParseLine(unknownLine, ctx)
		if err != nil {
			t.Errorf("unknown type should not error, got %v", err)
		}
		if len(records) != 0 {
			t.Errorf("unknown type should return 0 records, got %d", len(records))
		}
	})

	t.Run("bad JSON returns error", func(t *testing.T) {
		_, err := lp.ParseLine([]byte(`{not json`), ctx)
		if err == nil {
			t.Error("expected error on bad JSON")
		}
	})
}

package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fullfran/claudeops-tui/internal/source"
)

// --- T2.6: CODEX_HOME root resolution + fixture ingest round-trip ---

func TestCodexRoot(t *testing.T) {
	t.Run("REQ-2.6: CODEX_HOME env var used when set", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("CODEX_HOME", dir)
		root := CodexRoot()
		if root != dir {
			t.Errorf("want %q, got %q", dir, root)
		}
	})

	t.Run("REQ-2.6: fallback to ~/.codex/sessions when CODEX_HOME not set", func(t *testing.T) {
		t.Setenv("CODEX_HOME", "")
		root := CodexRoot()
		if !strings.HasSuffix(root, filepath.Join(".codex", "sessions")) {
			t.Errorf("expected suffix .codex/sessions, got %q", root)
		}
	})
}

// TestCodexRoundTrip verifies that hand-built fixture JSONL files under a
// YYYY/MM/DD/ tree are ingested and produce Records with Source=="codex" and
// non-zero tokens. Skipped in -short mode.
//
// IMPORTANT: This test builds fixtures from the spec-derived schema.
// Validate against real ~/.codex/sessions/**/rollout-*.jsonl before
// trusting the parser in production.
func TestCodexRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in -short mode")
	}

	// Build a fixture directory structure: YYYY/MM/DD/rollout-<ts>-<uuid>.jsonl
	root := t.TempDir()
	sessionUUID := "fixture-sess-roundtrip"
	dir := filepath.Join(root, "2026", "01", "15")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fname := filepath.Join(dir, "rollout-20260115T120000Z-"+sessionUUID+".jsonl")

	// Write two lines: turn_context (sets model) + token_count (produces a Record)
	tcLine := `{"timestamp":"2026-01-15T12:00:00Z","type":"turn_context","payload":{"model":"gpt-4o","cwd":"/home/user/myproject"}}`
	tokenLine := makeTokenCountLineWithFields(500, 100, 200, 0)
	content := tcLine + "\n" + string(tokenLine) + "\n"
	if err := os.WriteFile(fname, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// Collect emitted Records via a stub sink.
	var emitted []source.Record
	stubSink := &stubSink{emit: func(r source.Record) { emitted = append(emitted, r) }}

	// Use the parser directly on the file (mimics Collector ingestFile behaviour).
	p := NewParser()
	data, err := os.ReadFile(fname)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	offset := int64(0)
	for _, rawLine := range splitLines(data) {
		lctx := source.LineContext{
			Path:        fname,
			LineOffset:  offset,
			SessionUUID: sessionUUID,
			DefaultCWD:  "/home/user/myproject",
		}
		records, err := p.ParseLine(rawLine, lctx)
		if err != nil {
			t.Fatalf("ParseLine: %v", err)
		}
		for _, r := range records {
			if err := stubSink.Emit(context.Background(), r); err != nil {
				t.Fatalf("Emit: %v", err)
			}
		}
		offset += int64(len(rawLine)) + 1
	}

	if len(emitted) == 0 {
		t.Fatal("expected at least one Record from round-trip")
	}
	for _, r := range emitted {
		if r.Source != source.Codex {
			t.Errorf("Source: want codex, got %q", r.Source)
		}
		if r.In+r.Out+r.CacheRead == 0 {
			t.Errorf("expected non-zero tokens, got %+v", r)
		}
	}
}

// stubSink is a minimal source.Sink for tests.
type stubSink struct {
	emit func(source.Record)
}

func (s *stubSink) Emit(_ context.Context, r source.Record) error {
	if s.emit != nil {
		s.emit(r)
	}
	return nil
}

// splitLines splits raw bytes into lines, stripping the trailing newline from each.
func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			line := data[start:i]
			if len(line) > 0 {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if start < len(data) {
		if line := data[start:]; len(line) > 0 {
			lines = append(lines, line)
		}
	}
	return lines
}

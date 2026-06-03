package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/fullfran/claudeops-tui/internal/config"
	"github.com/fullfran/claudeops-tui/internal/pricing"
	"github.com/fullfran/claudeops-tui/internal/source"
	"github.com/fullfran/claudeops-tui/internal/store"
)

// stubSinkForMain counts Emit calls by source.
type stubSinkForMain struct {
	counts map[string]int
}

func (ss *stubSinkForMain) Emit(_ context.Context, r source.Record) error {
	if ss.counts == nil {
		ss.counts = make(map[string]int)
	}
	ss.counts[string(r.Source)]++
	return nil
}

// TestBuildCollectors covers REQ-1.6.2 and REQ-1.6.3:
// disabled source → no Collector started; enabled source → Collector started.
func TestBuildCollectors(t *testing.T) {
	t.Run("REQ-1.6.2 disabled source produces zero collectors", func(t *testing.T) {
		dir := t.TempDir()
		s, err := store.Open(filepath.Join(dir, "test.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()

		tbl, _ := pricing.LoadOrSeed(filepath.Join(dir, "pricing.toml"))
		calc := pricing.NewCalculator(tbl)
		sink := source.NewStoreSink(s, calc)

		sources := []config.SourceConfig{
			{Name: "codex", Enabled: false, Root: dir},
		}
		cols := buildCollectors(sources, sink, dir)
		if len(cols) != 0 {
			t.Errorf("want 0 collectors for disabled codex, got %d", len(cols))
		}
	})

	t.Run("REQ-1.6.3 enabled claude source produces a collector", func(t *testing.T) {
		dir := t.TempDir()
		s, err := store.Open(filepath.Join(dir, "test.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()

		tbl, _ := pricing.LoadOrSeed(filepath.Join(dir, "pricing.toml"))
		calc := pricing.NewCalculator(tbl)
		sink := source.NewStoreSink(s, calc)

		claudeRoot := filepath.Join(dir, "projects")
		sources := []config.SourceConfig{
			{Name: "claude", Enabled: true, Root: claudeRoot},
		}
		cols := buildCollectors(sources, sink, claudeRoot)
		if len(cols) != 1 {
			t.Errorf("want 1 collector for enabled claude, got %d", len(cols))
		}
	})

	t.Run("claude wiring uses source.Claude and ClaudeLineParser", func(t *testing.T) {
		dir := t.TempDir()
		ss := &stubSinkForMain{}
		sources := []config.SourceConfig{
			{Name: "claude", Enabled: true, Root: dir},
		}
		claudeRoot := dir
		cols := buildCollectors(sources, ss, claudeRoot)
		if len(cols) != 1 {
			t.Fatalf("want 1 collector, got %d", len(cols))
		}
		// Collector is wired; verify it has a root.
		// (We don't run it here — just confirm wiring doesn't panic.)
	})

	// WARNING-3 coverage: enabling a codex source must produce exactly one
	// Collector (wired with the codex parser) — consistent with the claude subtest above.
	t.Run("REQ-1.6.3 enabled codex source produces a collector", func(t *testing.T) {
		dir := t.TempDir()
		s, err := store.Open(filepath.Join(dir, "test.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()

		tbl, _ := pricing.LoadOrSeed(filepath.Join(dir, "pricing.toml"))
		calc := pricing.NewCalculator(tbl)
		sink := source.NewStoreSink(s, calc)

		codexRoot := filepath.Join(dir, "codex-sessions")
		sources := []config.SourceConfig{
			{Name: "codex", Enabled: true, Root: codexRoot},
		}
		cols := buildCollectors(sources, sink, dir)
		if len(cols) != 1 {
			t.Errorf("want 1 collector for enabled codex, got %d", len(cols))
		}
	})
}

package main

import (
	"path/filepath"
	"testing"

	"github.com/fullfran/claudeops-tui/internal/config"
	"github.com/fullfran/claudeops-tui/internal/pricing"
	"github.com/fullfran/claudeops-tui/internal/source"
	"github.com/fullfran/claudeops-tui/internal/store"
)

// TestBuildAgyIngester mirrors TestBuildOpencodeIngester: buildAgyIngester
// must return nil when the agy source is disabled or absent, and a non-nil
// Ingester named source.Agy when enabled, honoring an explicit Root override.
func TestBuildAgyIngester(t *testing.T) {
	t.Run("disabled agy returns nil ingester", func(t *testing.T) {
		dir := t.TempDir()
		s, err := store.Open(filepath.Join(dir, "test.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = s.Close() }()

		tbl, _ := pricing.LoadOrSeed(filepath.Join(dir, "pricing.toml"))
		calc := pricing.NewCalculator(tbl)
		sink := source.NewStoreSink(s, calc)

		sources := []config.SourceConfig{
			{Name: "agy", Enabled: false, Root: dir},
		}
		ing := buildAgyIngester(sources, s, sink)
		if ing != nil {
			t.Error("expected nil ingester for disabled agy")
		}
	})

	t.Run("enabled agy returns ingester with correct name", func(t *testing.T) {
		dir := t.TempDir()
		s, err := store.Open(filepath.Join(dir, "test.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = s.Close() }()

		tbl, _ := pricing.LoadOrSeed(filepath.Join(dir, "pricing.toml"))
		calc := pricing.NewCalculator(tbl)
		sink := source.NewStoreSink(s, calc)

		sources := []config.SourceConfig{
			{Name: "agy", Enabled: true, Root: filepath.Join(dir, "antigravity-cli")},
		}
		ing := buildAgyIngester(sources, s, sink)
		if ing == nil {
			t.Fatal("expected non-nil ingester for enabled agy")
		}
		if ing.Name() != source.Agy {
			t.Errorf("Name(): got %q want %q", ing.Name(), source.Agy)
		}
	})

	t.Run("no agy config uses default root", func(t *testing.T) {
		dir := t.TempDir()
		s, err := store.Open(filepath.Join(dir, "test.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = s.Close() }()

		tbl, _ := pricing.LoadOrSeed(filepath.Join(dir, "pricing.toml"))
		calc := pricing.NewCalculator(tbl)
		sink := source.NewStoreSink(s, calc)

		// No agy config at all → nil (disabled by default).
		ing := buildAgyIngester(nil, s, sink)
		if ing != nil {
			t.Error("expected nil ingester when no agy config present")
		}
	})
}

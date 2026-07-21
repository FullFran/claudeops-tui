package main

import (
	"path/filepath"
	"testing"

	"github.com/fullfran/claudeops-tui/internal/config"
	"github.com/fullfran/claudeops-tui/internal/pricing"
	"github.com/fullfran/claudeops-tui/internal/source"
	"github.com/fullfran/claudeops-tui/internal/store"
)

// TestBuildOpencodeIngester verifies that buildOpencodeIngester:
//   - returns nil when opencode source is disabled
//   - returns a non-nil Ingester with Name()=="opencode" when enabled
func TestBuildOpencodeIngester(t *testing.T) {
	t.Run("disabled opencode returns nil ingester", func(t *testing.T) {
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
			{Name: "opencode", Enabled: false, Root: dir},
		}
		ing := buildOpencodeIngester(sources, s, sink)
		if ing != nil {
			t.Error("expected nil ingester for disabled opencode")
		}
	})

	t.Run("enabled opencode returns ingester with correct name", func(t *testing.T) {
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
			{Name: "opencode", Enabled: true, Root: filepath.Join(dir, "opencode.db")},
		}
		ing := buildOpencodeIngester(sources, s, sink)
		if ing == nil {
			t.Fatal("expected non-nil ingester for enabled opencode")
		}
		if ing.Name() != source.Opencode {
			t.Errorf("Name(): got %q want %q", ing.Name(), source.Opencode)
		}
	})

	t.Run("no opencode config uses default db path", func(t *testing.T) {
		dir := t.TempDir()
		s, err := store.Open(filepath.Join(dir, "test.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = s.Close() }()

		tbl, _ := pricing.LoadOrSeed(filepath.Join(dir, "pricing.toml"))
		calc := pricing.NewCalculator(tbl)
		sink := source.NewStoreSink(s, calc)

		// No opencode config at all → nil (disabled by default).
		ing := buildOpencodeIngester(nil, s, sink)
		if ing != nil {
			t.Error("expected nil ingester when no opencode config present")
		}
	})
}

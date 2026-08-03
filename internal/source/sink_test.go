package source_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/fullfran/claudeops-tui/internal/pricing"
	"github.com/fullfran/claudeops-tui/internal/source"
	"github.com/fullfran/claudeops-tui/internal/store"
)

func newTestSink(t *testing.T) (*source.StoreSink, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	tbl, err := pricing.LoadOrSeed(filepath.Join(dir, "pricing.toml"))
	if err != nil {
		t.Fatalf("pricing.LoadOrSeed: %v", err)
	}
	calc := pricing.NewCalculator(tbl)
	sink := source.NewStoreSink(s, calc)
	return sink, s
}

// TestStoreSink covers REQ-1.2.1, REQ-1.4.1, REQ-1.4.2, REQ-1.4.3.
func TestStoreSink(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	t.Run("REQ-1.2.1 claude record source stamped correctly", func(t *testing.T) {
		sink, s := newTestSink(t)
		r := source.Record{
			Source:    source.Claude,
			UUID:      "sink-claude-1",
			SessionID: "sess-c1",
			CWD:       "/p/myproject",
			Type:      "assistant",
			Model:     "claude-opus-4-6",
			TS:        now,
			In:        5,
			Out:       100,
		}
		if err := sink.Emit(ctx, r); err != nil {
			t.Fatalf("Emit: %v", err)
		}
		var src string
		if err := s.DB().QueryRow(`SELECT source FROM events WHERE uuid='sink-claude-1'`).Scan(&src); err != nil {
			t.Fatalf("SELECT source: %v", err)
		}
		if src != "claude" {
			t.Errorf("want source='claude', got %q", src)
		}
	})

	t.Run("REQ-1.4.1 empty CWD + non-claude source + fallback succeeds", func(t *testing.T) {
		sink, s := newTestSink(t)
		r := source.Record{
			Source:    source.Codex,
			UUID:      "sink-codex-1",
			SessionID: "sess-codex1",
			CWD:       "", // empty — fallback should be applied by caller
			Type:      "assistant",
			Model:     "gpt-4o",
			TS:        now,
		}
		// The Sink must apply fallback CWD for non-claude sources when CWD is empty.
		if err := sink.Emit(ctx, r); err != nil {
			t.Fatalf("Emit with empty CWD codex: %v", err)
		}
		var project string
		if err := s.DB().QueryRow(`SELECT p.name FROM events e JOIN sessions sess ON sess.id=e.session_id JOIN projects p ON p.id=sess.project_id WHERE e.uuid='sink-codex-1'`).Scan(&project); err != nil {
			t.Fatalf("SELECT project: %v", err)
		}
		if project == "" {
			t.Error("project should not be empty after CWD fallback")
		}
	})

	// WARNING-2 coverage: unknown (unpriced) model — row must be inserted with
	// tokens recorded and cost_eur NULL (no pricing entry exists for a made-up
	// model id that is deliberately absent from the seed table).
	t.Run("REQ-1.4.1b unknown model emits row with NULL cost", func(t *testing.T) {
		sink, s := newTestSink(t)
		r := source.Record{
			Source:    source.Codex,
			UUID:      "sink-codex-unknown-model",
			SessionID: "sess-codex-unk",
			CWD:       "", // triggers fallback path
			Type:      "assistant",
			Model:     "gpt-nonexistent-v99", // not present in the seed pricing table
			TS:        now,
			In:        100,
			Out:       50,
		}
		if err := sink.Emit(ctx, r); err != nil {
			t.Fatalf("Emit with unknown model: %v", err)
		}
		// Row must exist — tokens persisted.
		var inTok, outTok int64
		var costEUR *float64
		row := s.DB().QueryRow(
			`SELECT in_tokens, out_tokens, cost_eur FROM events WHERE uuid='sink-codex-unknown-model'`,
		)
		if err := row.Scan(&inTok, &outTok, &costEUR); err != nil {
			t.Fatalf("SELECT row for unknown-model event: %v", err)
		}
		if inTok != 100 {
			t.Errorf("want in_tokens=100, got %d", inTok)
		}
		if outTok != 50 {
			t.Errorf("want out_tokens=50, got %d", outTok)
		}
		if costEUR != nil {
			t.Errorf("want cost_eur=NULL for unknown model, got %v", *costEUR)
		}
	})

	t.Run("REQ-1.4.2 empty CWD + claude source returns error", func(t *testing.T) {
		sink, _ := newTestSink(t)
		r := source.Record{
			Source:    source.Claude,
			UUID:      "sink-claude-nocwd",
			SessionID: "sess-nc",
			CWD:       "", // empty CWD + claude → error
			Type:      "assistant",
			TS:        now,
		}
		err := sink.Emit(ctx, r)
		if err == nil {
			t.Fatal("want error for claude + empty CWD, got nil")
		}
	})

	t.Run("REQ-1.4.3 normal claude record project derived from cwd", func(t *testing.T) {
		sink, s := newTestSink(t)
		r := source.Record{
			Source:    source.Claude,
			UUID:      "sink-claude-cwd",
			SessionID: "sess-cwd",
			CWD:       "/home/user/myrepo",
			Type:      "assistant",
			Model:     "claude-opus-4-6",
			TS:        now,
		}
		if err := sink.Emit(ctx, r); err != nil {
			t.Fatalf("Emit: %v", err)
		}
		var project, src string
		row := s.DB().QueryRow(`SELECT p.name, e.source FROM events e JOIN sessions sess ON sess.id=e.session_id JOIN projects p ON p.id=sess.project_id WHERE e.uuid='sink-claude-cwd'`)
		if err := row.Scan(&project, &src); err != nil {
			t.Fatalf("SELECT: %v", err)
		}
		if project != "myrepo" {
			t.Errorf("want project='myrepo', got %q", project)
		}
		if src != "claude" {
			t.Errorf("want source='claude', got %q", src)
		}
	})
}

// TestStoreSinkReportedCost covers the rule that a source-reported billing
// figure replaces the table-derived estimate, but only when it is positive.
func TestStoreSinkReportedCost(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	// usd builds a *float64 for the ReportedCostUSD field.
	usd := func(v float64) *float64 { return &v }

	// costOf reads back the stored cost for one event.
	costOf := func(t *testing.T, s *store.Store, uuid string) *float64 {
		t.Helper()
		var c *float64
		if err := s.DB().QueryRow(`SELECT cost_eur FROM events WHERE uuid=?`, uuid).Scan(&c); err != nil {
			t.Fatalf("SELECT cost_eur for %s: %v", uuid, err)
		}
		return c
	}

	// estimate is what the price table alone produces for the token counts the
	// cases below share, so each case can assert against it rather than against
	// a hard-coded number that a price refresh would invalidate.
	const (
		inTok  = 1_000_000
		outTok = 500_000
		model  = "claude-opus-4-6"
	)
	baseline := func(t *testing.T) float64 {
		t.Helper()
		sink, s := newTestSink(t)
		r := source.Record{
			Source: source.Opencode, UUID: "baseline", SessionID: "sess-b",
			CWD: "/p/b", Type: "assistant", Model: model, TS: now, In: inTok, Out: outTok,
		}
		if err := sink.Emit(ctx, r); err != nil {
			t.Fatalf("Emit: %v", err)
		}
		got := costOf(t, s, "baseline")
		if got == nil {
			t.Fatal("baseline estimate is NULL; the test model lost its price entry")
		}
		return *got
	}

	t.Run("positive report replaces the estimate", func(t *testing.T) {
		est := baseline(t)
		sink, s := newTestSink(t)
		r := source.Record{
			Source: source.Opencode, UUID: "billed", SessionID: "sess-1",
			CWD: "/p/x", Type: "assistant", Model: model, TS: now, In: inTok, Out: outTok,
			ReportedCostUSD: usd(6.5877),
		}
		if err := sink.Emit(ctx, r); err != nil {
			t.Fatalf("Emit: %v", err)
		}
		got := costOf(t, s, "billed")
		if got == nil {
			t.Fatal("want the reported cost, got NULL")
		}
		want := pricing.EURFromUSD(6.5877)
		if diff := *got - want; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("want reported cost %v EUR, got %v", want, *got)
		}
		if *got == est {
			t.Error("reported cost was not applied: it equals the table estimate")
		}
	})

	t.Run("zero report keeps the estimate", func(t *testing.T) {
		est := baseline(t)
		sink, s := newTestSink(t)
		r := source.Record{
			Source: source.Opencode, UUID: "subscription", SessionID: "sess-2",
			CWD: "/p/x", Type: "assistant", Model: model, TS: now, In: inTok, Out: outTok,
			ReportedCostUSD: usd(0),
		}
		if err := sink.Emit(ctx, r); err != nil {
			t.Fatalf("Emit: %v", err)
		}
		got := costOf(t, s, "subscription")
		if got == nil || *got != est {
			t.Errorf("want the table estimate %v kept, got %v", est, got)
		}
	})

	t.Run("negative report keeps the estimate", func(t *testing.T) {
		est := baseline(t)
		sink, s := newTestSink(t)
		r := source.Record{
			Source: source.Opencode, UUID: "negative", SessionID: "sess-3",
			CWD: "/p/x", Type: "assistant", Model: model, TS: now, In: inTok, Out: outTok,
			ReportedCostUSD: usd(-1),
		}
		if err := sink.Emit(ctx, r); err != nil {
			t.Fatalf("Emit: %v", err)
		}
		got := costOf(t, s, "negative")
		if got == nil || *got != est {
			t.Errorf("want the table estimate %v kept, got %v", est, got)
		}
	})

	t.Run("report rescues a model the table cannot price", func(t *testing.T) {
		sink, s := newTestSink(t)
		r := source.Record{
			Source: source.Opencode, UUID: "unpriced", SessionID: "sess-4",
			CWD: "/p/x", Type: "assistant", Model: "opencode-go/kimi-k3-nonexistent",
			TS: now, In: inTok, Out: outTok,
			ReportedCostUSD: usd(2.4177),
		}
		if err := sink.Emit(ctx, r); err != nil {
			t.Fatalf("Emit: %v", err)
		}
		got := costOf(t, s, "unpriced")
		if got == nil {
			t.Fatal("want the reported cost for an unpriced model, got NULL")
		}
		want := pricing.EURFromUSD(2.4177)
		if diff := *got - want; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("want %v EUR, got %v", want, *got)
		}
	})
}

package source

import (
	"context"
	"fmt"
	"time"

	"github.com/fullfran/claudeops-tui/internal/pricing"
	"github.com/fullfran/claudeops-tui/internal/store"
)

// TaskResolver resolves which task (if any) an event belongs to.
// Implemented by internal/tasks.Tracker; accepted as an interface to avoid
// import cycles and to keep the source package dependency-light.
type TaskResolver interface {
	Resolve(sessionID string, ts time.Time) *string
}

// nopTaskResolver is the default when no task tracker is wired up.
type nopTaskResolver struct{}

func (nopTaskResolver) Resolve(string, time.Time) *string { return nil }

// StoreSink implements Sink by applying pricing and calling store.Insert.
// It owns the cwd-fallback logic for non-Claude sources so that store.Insert's
// non-empty-cwd contract is never violated.
type StoreSink struct {
	store *store.Store
	calc  *pricing.Calculator
	tasks TaskResolver
}

// NewStoreSink creates a StoreSink without a task resolver.
func NewStoreSink(s *store.Store, calc *pricing.Calculator) *StoreSink {
	return &StoreSink{
		store: s,
		calc:  calc,
		tasks: nopTaskResolver{},
	}
}

// NewStoreSinkWithTasks creates a StoreSink with a task resolver.
func NewStoreSinkWithTasks(s *store.Store, calc *pricing.Calculator, tr TaskResolver) *StoreSink {
	if tr == nil {
		tr = nopTaskResolver{}
	}
	return &StoreSink{store: s, calc: calc, tasks: tr}
}

// Store returns the underlying *store.Store for offset persistence by the Collector.
func (ss *StoreSink) Store() *store.Store { return ss.store }

// Emit applies cwd fallback for non-Claude sources, computes cost, and inserts.
// For Claude events, empty CWD is a hard error (preserves existing validation).
// For other sources, empty CWD is replaced with a synthetic fallback so that
// store.Insert's non-empty-cwd invariant is always satisfied.
func (ss *StoreSink) Emit(ctx context.Context, r Record) error {
	cwd := r.CWD
	if cwd == "" {
		if r.Source == Claude {
			// Claude must always provide CWD — preserve existing error contract.
			return fmt.Errorf("source.StoreSink: claude event missing CWD (uuid=%s)", r.UUID)
		}
		// Non-Claude sources get a synthetic CWD so Insert accepts the row.
		// filepath.Base of this synthetic value yields a source-tagged project name.
		cwd = string(r.Source) + ":" + r.SessionID
	}

	ev := store.Event{
		UUID:                r.UUID,
		SessionID:           r.SessionID,
		CWD:                 cwd,
		Type:                r.Type,
		Model:               r.Model,
		TS:                  r.TS,
		InTokens:            r.In,
		OutTokens:           r.Out,
		CacheReadTokens:     r.CacheRead,
		CacheCreateTokens:   r.CacheCreate,
		CacheCreate1hTokens: r.CacheCreate1h,
		Source:              string(r.Source),
	}

	// CostForCacheTTL, not CostFor: a 1h cache write bills at roughly 1.6x the
	// 5m rate, so flattening the split undercosts every long-TTL write.
	var cost *float64
	if ss.calc != nil && r.Model != "" {
		cost = ss.calc.CostForCacheTTL(r.Model, r.In, r.Out, r.CacheRead, r.CacheCreate, r.CacheCreate1h)
	}
	if billed := billedCostEUR(r); billed != nil {
		cost = billed
	}

	taskID := ss.tasks.Resolve(r.SessionID, r.TS)
	return ss.store.Insert(ctx, ev, cost, taskID)
}

// billedCostEUR converts a source-reported cost into EUR, or returns nil when
// the estimate should be kept instead.
//
// Only a positive report wins. A zero is not the same claim as a positive one:
// a tool that routes through a subscription the user already paid for records
// 0 for that call, because no incremental money changed hands. Taking it
// literally would erase the equivalent-API-value figure this dashboard exists
// to show, and would do it precisely for the traffic that dominates it. So a
// reported 0 falls back to the price table, and only an amount the source says
// it actually charged replaces the estimate.
func billedCostEUR(r Record) *float64 {
	if r.ReportedCostUSD == nil || *r.ReportedCostUSD <= 0 {
		return nil
	}
	eur := pricing.EURFromUSD(*r.ReportedCostUSD)
	return &eur
}

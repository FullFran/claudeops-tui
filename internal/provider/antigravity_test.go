package provider

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/fullfran/claudeops-tui/internal/agy"
)

func TestAntigravityAvailable(t *testing.T) {
	t.Run("no snapshot file", func(t *testing.T) {
		a := NewAntigravity(filepath.Join(t.TempDir(), "antigravity-quota.json"))
		if a.Available() {
			t.Error("Available() = true, want false with no snapshot on disk")
		}
	})

	t.Run("snapshot present", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "antigravity-quota.json")
		if err := agy.WriteQuotaSnapshot(path, agy.QuotaSnapshot{ObservedAt: fixedNow()}); err != nil {
			t.Fatalf("WriteQuotaSnapshot: %v", err)
		}
		a := NewAntigravity(path)
		if !a.Available() {
			t.Error("Available() = false, want true with a snapshot on disk")
		}
	})
}

func TestAntigravityFetch(t *testing.T) {
	// The documented fixture bucket: remaining_fraction 0.9378 -> 6.22% used.
	path := filepath.Join(t.TempDir(), "antigravity-quota.json")
	observedAt := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	resetAt := time.Date(2026, 7, 6, 7, 50, 32, 0, time.UTC) // after observedAt: not expired
	snap := agy.QuotaSnapshot{
		ObservedAt: observedAt,
		PlanTier:   "Pro",
		Buckets: map[string]agy.QuotaBucket{
			"gemini-weekly": {RemainingFraction: 0.9378, ResetTime: resetAt},
		},
	}
	if err := agy.WriteQuotaSnapshot(path, snap); err != nil {
		t.Fatalf("WriteQuotaSnapshot: %v", err)
	}

	a := &Antigravity{SnapshotPath: path, Now: func() time.Time { return observedAt }}
	u, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if u.Provider != "Antigravity" {
		t.Errorf("Provider = %q, want Antigravity", u.Provider)
	}
	if len(u.Windows) != 1 {
		t.Fatalf("got %d windows, want 1: %+v", len(u.Windows), u.Windows)
	}
	w := u.Windows[0]
	if w.Label != "gemini-weekly" {
		t.Errorf("Label = %q, want gemini-weekly", w.Label)
	}
	if w.Utilization < 6.21 || w.Utilization > 6.23 {
		t.Errorf("Utilization = %v, want ~6.22", w.Utilization)
	}
	if !w.ResetsAt.Equal(resetAt) {
		t.Errorf("ResetsAt = %v, want %v", w.ResetsAt, resetAt)
	}
	if u.Note != "plan: Pro" {
		t.Errorf("Note = %q, want %q", u.Note, "plan: Pro")
	}
	if !u.FetchedAt.Equal(snap.ObservedAt) {
		t.Errorf("FetchedAt = %v, want the snapshot's ObservedAt %v", u.FetchedAt, snap.ObservedAt)
	}
}

func TestAntigravityFetchDropsExpiredBucket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "antigravity-quota.json")
	now := fixedNow()
	snap := agy.QuotaSnapshot{
		ObservedAt: now,
		Buckets: map[string]agy.QuotaBucket{
			// Reset time already passed: the quota reset since agy last
			// reported it, so this reading is no longer trustworthy.
			"gemini-weekly": {RemainingFraction: 0.9378, ResetTime: now.Add(-time.Hour)},
			// Still valid.
			"gemini-daily": {RemainingFraction: 0.5, ResetTime: now.Add(time.Hour)},
		},
	}
	if err := agy.WriteQuotaSnapshot(path, snap); err != nil {
		t.Fatalf("WriteQuotaSnapshot: %v", err)
	}

	a := &Antigravity{SnapshotPath: path, Now: func() time.Time { return now }}
	u, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(u.Windows) != 1 {
		t.Fatalf("got %d windows, want 1 (expired bucket dropped): %+v", len(u.Windows), u.Windows)
	}
	if u.Windows[0].Label != "gemini-daily" {
		t.Errorf("Label = %q, want gemini-daily", u.Windows[0].Label)
	}
}

func TestAntigravityFetchAllExpiredYieldsNoWindows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "antigravity-quota.json")
	now := fixedNow()
	snap := agy.QuotaSnapshot{
		ObservedAt: now,
		Buckets: map[string]agy.QuotaBucket{
			"gemini-weekly": {RemainingFraction: 0.9378, ResetTime: now.Add(-time.Hour)},
		},
	}
	if err := agy.WriteQuotaSnapshot(path, snap); err != nil {
		t.Fatalf("WriteQuotaSnapshot: %v", err)
	}

	a := &Antigravity{SnapshotPath: path, Now: func() time.Time { return now }}
	u, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(u.Windows) != 0 {
		t.Errorf("got %d windows, want 0 when every bucket has expired", len(u.Windows))
	}
}

func TestAntigravityFetchMissingSnapshot(t *testing.T) {
	a := NewAntigravity(filepath.Join(t.TempDir(), "antigravity-quota.json"))
	if _, err := a.Fetch(context.Background()); err == nil {
		t.Error("expected an error fetching with no snapshot on disk")
	}
}

func TestAntigravityFetchDeterministicOrder(t *testing.T) {
	// Map iteration is randomised; the rendered order must not flicker
	// between runs the way statusline.DetectAgent's doc comment warns about.
	path := filepath.Join(t.TempDir(), "antigravity-quota.json")
	now := fixedNow()
	snap := agy.QuotaSnapshot{
		ObservedAt: now,
		Buckets: map[string]agy.QuotaBucket{
			"gemini-weekly": {RemainingFraction: 0.9, ResetTime: now.Add(time.Hour)},
			"gemini-daily":  {RemainingFraction: 0.5, ResetTime: now.Add(time.Hour)},
			"gemini-hourly": {RemainingFraction: 0.1, ResetTime: now.Add(time.Hour)},
		},
	}
	if err := agy.WriteQuotaSnapshot(path, snap); err != nil {
		t.Fatalf("WriteQuotaSnapshot: %v", err)
	}
	a := &Antigravity{SnapshotPath: path, Now: func() time.Time { return now }}
	u, err := a.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	want := []string{"gemini-daily", "gemini-hourly", "gemini-weekly"} // sorted
	if len(u.Windows) != len(want) {
		t.Fatalf("got %d windows, want %d", len(u.Windows), len(want))
	}
	for i, label := range want {
		if u.Windows[i].Label != label {
			t.Errorf("Windows[%d].Label = %q, want %q", i, u.Windows[i].Label, label)
		}
	}
}

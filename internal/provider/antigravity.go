package provider

import (
	"context"
	"os"
	"sort"
	"time"

	"github.com/fullfran/claudeops-tui/internal/agy"
)

// Antigravity serves Google Antigravity CLI (agy) quota from the on-disk
// snapshot `claudeops agy statusline` persists. Unlike the other providers
// there is no live endpoint to poll: agy exposes quota only through its own
// status-line payload, pushed to that command whenever its agent state
// changes, so the last snapshot on disk is the freshest answer this process
// can give.
type Antigravity struct {
	// SnapshotPath is where the quota snapshot lives (conventionally
	// config.Paths.AntigravityQuotaPath).
	SnapshotPath string
	Now          func() time.Time
}

// NewAntigravity builds an Antigravity provider reading from snapshotPath.
func NewAntigravity(snapshotPath string) *Antigravity {
	return &Antigravity{SnapshotPath: snapshotPath, Now: time.Now}
}

// Name implements Provider.
func (a *Antigravity) Name() string { return "Antigravity" }

// Local implements the provider.Local marker interface: the quota snapshot on
// disk is rewritten by `claudeops agy statusline` whenever agy's own state
// changes, so caching a fetch here like a network provider would show a
// reading stale by as much as the registry's TTL even though a fresher one
// might already be sitting on disk.
func (a *Antigravity) Local() bool { return true }

// Available reports whether a quota snapshot has ever been written. There is
// no credential to check — agy's own status-line invocation is the only
// signal this provider has that agy is even in use.
func (a *Antigravity) Available() bool {
	_, err := os.Stat(a.SnapshotPath)
	return err == nil
}

// Fetch reads and normalizes the persisted quota snapshot.
func (a *Antigravity) Fetch(ctx context.Context) (Usage, error) {
	snap, err := agy.ReadQuotaSnapshot(a.SnapshotPath)
	if err != nil {
		return Usage{}, err
	}
	return AntigravityUsage(snap, a.now()), nil
}

func (a *Antigravity) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// AntigravityUsage normalizes a persisted quota snapshot into a Usage value.
// Exported so `claudeops agy statusline` can render the exact shape it just
// wrote without a round trip through disk.
//
// A bucket whose reset time has already passed is dropped: the quota reset
// since agy last reported it, so the remaining_fraction on file is no longer
// a truthful answer and showing it would be worse than showing nothing.
func AntigravityUsage(snap agy.QuotaSnapshot, now time.Time) Usage {
	// Buckets is a map; iterate in a fixed order so the rendered line does not
	// flicker between runs the way statusline.DetectAgent's doc comment warns
	// map iteration otherwise would.
	labels := make([]string, 0, len(snap.Buckets))
	for label := range snap.Buckets {
		labels = append(labels, label)
	}
	sort.Strings(labels)

	windows := make([]Window, 0, len(labels))
	for _, label := range labels {
		b := snap.Buckets[label]
		if !b.ResetTime.IsZero() && b.ResetTime.Before(now) {
			continue
		}
		util := (1 - b.RemainingFraction) * 100
		switch {
		case util < 0:
			util = 0
		case util > 100:
			util = 100
		}
		windows = append(windows, Window{Label: label, Utilization: util, ResetsAt: b.ResetTime})
	}

	u := Usage{Provider: "Antigravity", Windows: windows, FetchedAt: snap.ObservedAt}
	if snap.PlanTier != "" {
		u.Note = "plan: " + snap.PlanTier
	}
	return u
}

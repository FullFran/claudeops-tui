package provider

import (
	"context"
	"os"
	"sort"
	"strings"
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
	//
	// Primary windows (5h, 7d) are placed first to match the status-line layout
	// of Claude and Codex, and so that `statusline --reset` inspects the primary
	// 5h window rather than a secondary lane.
	rawLabels := make([]string, 0, len(snap.Buckets))
	for label := range snap.Buckets {
		rawLabels = append(rawLabels, label)
	}
	sort.Slice(rawLabels, func(i, j int) bool {
		pi := agyLabelPriority(NormalizeAntigravityBucketLabel(rawLabels[i]))
		pj := agyLabelPriority(NormalizeAntigravityBucketLabel(rawLabels[j]))
		if pi != pj {
			return pi < pj
		}
		return rawLabels[i] < rawLabels[j]
	})

	windows := make([]Window, 0, len(rawLabels))
	for _, raw := range rawLabels {
		b := snap.Buckets[raw]
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
		windows = append(windows, Window{
			Label:       NormalizeAntigravityBucketLabel(raw),
			Utilization: util,
			ResetsAt:    b.ResetTime,
		})
	}

	u := Usage{Provider: "Antigravity", Windows: windows, FetchedAt: snap.ObservedAt}
	if snap.PlanTier != "" {
		u.Note = "plan: " + snap.PlanTier
	}
	return u
}

// NormalizeAntigravityBucketLabel shortens Antigravity bucket labels to match
// the compact conventions of Claude and Codex:
//   - "gemini-5h"     -> "5h"
//   - "gemini-weekly" -> "7d"
//   - "3p-weekly"     -> "3p-7d"
//   - "3p-5h"         -> "3p-5h"
func NormalizeAntigravityBucketLabel(raw string) string {
	label := strings.TrimPrefix(raw, "gemini-")
	switch {
	case label == "weekly":
		return "7d"
	case strings.HasSuffix(label, "-weekly"):
		return strings.TrimSuffix(label, "-weekly") + "-7d"
	case label == "daily":
		return "1d"
	case strings.HasSuffix(label, "-daily"):
		return strings.TrimSuffix(label, "-daily") + "-1d"
	case label == "hourly":
		return "1h"
	case strings.HasSuffix(label, "-hourly"):
		return strings.TrimSuffix(label, "-hourly") + "-1h"
	default:
		return label
	}
}

// agyLabelPriority orders primary windows (5h, 7d) first to match Claude and
// Codex, followed by any other primary windows, then 3p windows, then extras.
func agyLabelPriority(label string) int {
	switch label {
	case "5h":
		return 1
	case "7d":
		return 2
	case "1d":
		return 3
	case "1h":
		return 4
	case "3p-5h":
		return 10
	case "3p-7d":
		return 11
	case "3p-1d":
		return 12
	case "3p-1h":
		return 13
	default:
		return 100
	}
}

package provider

import (
	"testing"
	"time"

	"github.com/fullfran/claudeops-tui/internal/usage"
)

func TestSnapshotToUsage(t *testing.T) {
	reset := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	util := 42.0

	tests := []struct {
		name       string
		snap       usage.Snapshot
		wantLabels []string
		wantNote   string
	}{
		{
			name: "only five-hour bucket",
			snap: usage.Snapshot{
				FiveHour: &usage.Bucket{Utilization: 12.5, ResetsAt: reset},
			},
			wantLabels: []string{"5h"},
		},
		{
			name: "five-hour, seven-day and per-model buckets in order",
			snap: usage.Snapshot{
				FiveHour:       &usage.Bucket{Utilization: 10, ResetsAt: reset},
				SevenDay:       &usage.Bucket{Utilization: 20, ResetsAt: reset},
				SevenDayOpus:   &usage.Bucket{Utilization: 30, ResetsAt: reset},
				SevenDaySonnet: &usage.Bucket{Utilization: 40, ResetsAt: reset},
			},
			wantLabels: []string{"5h", "7d", "7d (opus)", "7d (sonnet)"},
		},
		{
			name: "extra credits note when enabled",
			snap: usage.Snapshot{
				FiveHour:   &usage.Bucket{Utilization: 5, ResetsAt: reset},
				ExtraUsage: &usage.ExtraUsage{IsEnabled: true, Utilization: &util},
			},
			wantLabels: []string{"5h"},
			wantNote:   "extra credits in use",
		},
		{
			name:       "empty snapshot yields no windows",
			snap:       usage.Snapshot{},
			wantLabels: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := snapshotToUsage(tt.snap)

			if got.Provider != "Claude" {
				t.Errorf("Provider = %q, want Claude", got.Provider)
			}
			if len(got.Windows) != len(tt.wantLabels) {
				t.Fatalf("got %d windows, want %d", len(got.Windows), len(tt.wantLabels))
			}
			for i, want := range tt.wantLabels {
				if got.Windows[i].Label != want {
					t.Errorf("window[%d].Label = %q, want %q", i, got.Windows[i].Label, want)
				}
			}
			if got.Note != tt.wantNote {
				t.Errorf("Note = %q, want %q", got.Note, tt.wantNote)
			}
		})
	}
}

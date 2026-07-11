package provider

import (
	"context"
	"os"

	"github.com/fullfran/claudeops-tui/internal/usage"
)

// Anthropic adapts the existing usage.Client to the Provider interface.
//
// It reuses the OAuth usage endpoint, caching, and token refresh already
// implemented in the usage package, and maps the Anthropic-specific bucket
// snapshot onto the normalized Usage shape.
type Anthropic struct {
	client    *usage.Client
	credsPath string
}

// NewAnthropic builds an Anthropic provider from an existing usage.Client and
// the path to ~/.claude/.credentials.json (used for availability detection).
func NewAnthropic(client *usage.Client, credsPath string) *Anthropic {
	return &Anthropic{client: client, credsPath: credsPath}
}

// Name implements Provider.
func (a *Anthropic) Name() string { return "Claude" }

// Available reports whether the credentials file carries an OAuth block.
// Users on ANTHROPIC_API_KEY (no OAuth) have no usage endpoint, so the
// provider is skipped rather than surfaced as a permanent error.
func (a *Anthropic) Available() bool {
	if a.client == nil {
		return false
	}
	if _, err := os.Stat(a.credsPath); err != nil {
		return false
	}
	_, err := usage.LoadCredentials(a.credsPath)
	return err == nil
}

// Fetch retrieves the Anthropic usage snapshot and maps it to Usage.
func (a *Anthropic) Fetch(ctx context.Context) (Usage, error) {
	snap, err := a.client.Get(ctx)
	if err != nil {
		return Usage{}, err
	}
	return snapshotToUsage(snap), nil
}

// snapshotToUsage flattens the Anthropic named buckets into ordered windows.
func snapshotToUsage(snap usage.Snapshot) Usage {
	windows := make([]Window, 0, 4)
	if snap.FiveHour != nil {
		windows = append(windows, Window{Label: "5h", Utilization: snap.FiveHour.Utilization, ResetsAt: snap.FiveHour.ResetsAt})
	}
	if snap.SevenDay != nil {
		windows = append(windows, Window{Label: "7d", Utilization: snap.SevenDay.Utilization, ResetsAt: snap.SevenDay.ResetsAt})
	}
	for _, nb := range snap.PerModelBuckets() {
		windows = append(windows, Window{Label: nb.Label, Utilization: nb.Bucket.Utilization, ResetsAt: nb.Bucket.ResetsAt})
	}
	u := Usage{Provider: "Claude", Windows: windows, FetchedAt: snap.FetchedAt}
	if snap.ExtraUsage != nil && snap.ExtraUsage.IsEnabled && snap.ExtraUsage.Utilization != nil {
		u.Note = "extra credits in use"
	}
	return u
}

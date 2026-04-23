package export

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/fullfran/claudeops-tui/internal/config"
	"github.com/fullfran/claudeops-tui/internal/store"
)

// Store is the subset of *store.Store that Pusher requires.
type Store interface {
	ConfigGet(ctx context.Context, key string) (string, bool, error)
	ConfigSet(ctx context.Context, key, value string) error
	AggregatesByProjectBetween(ctx context.Context, from, to time.Time) ([]store.ProjectPeriodAgg, error)
}

// Pusher orchestrates delta-window OTLP metric pushes.
type Pusher struct {
	store  Store
	cfg    config.ExportSettings
	creds  CredReader
	client *http.Client
	out    io.Writer
}

// PushOptions controls a single Push invocation.
type PushOptions struct {
	DryRun bool
	Since  *time.Time
}

// PushResult describes the outcome of a successful push.
type PushResult struct {
	PeriodFrom time.Time
	PeriodTo   time.Time
	DataPoints int
	DryRun     bool
}

// New creates a Pusher with injected dependencies.
func New(s Store, cfg config.ExportSettings, creds CredReader, client *http.Client, out io.Writer) *Pusher {
	return &Pusher{store: s, cfg: cfg, creds: creds, client: client, out: out}
}

// Push collects metrics for the delta window and POSTs them to the OTLP endpoint.
// When opts.DryRun is true the payload is written to p.out instead of sent.
func (p *Pusher) Push(ctx context.Context, opts PushOptions) (PushResult, error) {
	if !p.cfg.Enabled {
		return PushResult{}, fmt.Errorf("export is disabled")
	}
	if p.cfg.Endpoint == "" {
		return PushResult{}, fmt.Errorf("export.endpoint is not configured")
	}

	// Read email (non-fatal).
	email := ""
	if e, err := p.creds.Email(); err != nil {
		log.Printf("export: reading credentials: %v", err)
	} else {
		email = e
	}

	// Determine time window.
	to := time.Now().Truncate(time.Second)
	var from time.Time
	switch {
	case opts.Since != nil:
		from = *opts.Since
	default:
		if v, ok, err := p.store.ConfigGet(ctx, "export.last_pushed_at"); err == nil && ok {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				from = t
			}
		}
		if from.IsZero() {
			from = to.Add(-30 * 24 * time.Hour)
		}
	}

	// Query per-project aggregates.
	rows, err := p.store.AggregatesByProjectBetween(ctx, from, to)
	if err != nil {
		return PushResult{}, fmt.Errorf("export: query aggregates: %w", err)
	}

	// Build resource attributes.
	var resourceAttrs []KeyValue
	resourceAttrs = append(resourceAttrs, strAttr("service.name", "claudeops"))
	if email != "" {
		resourceAttrs = append(resourceAttrs, strAttr("user.email", email))
	}
	if p.cfg.UserName != "" {
		resourceAttrs = append(resourceAttrs, strAttr("claudeops.user_name", p.cfg.UserName))
	}
	if p.cfg.TeamName != "" {
		resourceAttrs = append(resourceAttrs, strAttr("claudeops.team_name", p.cfg.TeamName))
	}
	resource := Resource{Attributes: resourceAttrs}

	scope := InstrumentationScope{Name: "claudeops", Version: "1"}
	payload := buildPayload(resource, PeriodData{From: from, To: to, ByProject: rows}, scope)

	// Count data points.
	dataPoints := 0
	for _, rm := range payload.ResourceMetrics {
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				if m.Sum != nil {
					dataPoints += len(m.Sum.DataPoints)
				}
			}
		}
	}

	if opts.DryRun {
		b, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return PushResult{}, fmt.Errorf("export: marshal dry-run: %w", err)
		}
		if _, err := p.out.Write(b); err != nil {
			return PushResult{}, fmt.Errorf("export: write dry-run: %w", err)
		}
		return PushResult{PeriodFrom: from, PeriodTo: to, DataPoints: dataPoints, DryRun: true}, nil
	}

	// Marshal and POST.
	body, err := json.Marshal(payload)
	if err != nil {
		return PushResult{}, fmt.Errorf("export: marshal: %w", err)
	}
	endpoint := p.cfg.Endpoint + "/v1/metrics"
	if err := post(ctx, p.client, endpoint, p.cfg.Headers, body); err != nil {
		return PushResult{}, fmt.Errorf("export: post: %w", err)
	}

	// Persist last_pushed_at (non-fatal).
	if err := p.store.ConfigSet(ctx, "export.last_pushed_at", to.UTC().Format(time.RFC3339)); err != nil {
		log.Printf("export: save last_pushed_at: %v", err)
	}

	return PushResult{PeriodFrom: from, PeriodTo: to, DataPoints: dataPoints}, nil
}

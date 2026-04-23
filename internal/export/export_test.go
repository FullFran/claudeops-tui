package export

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fullfran/claudeops-tui/internal/config"
	"github.com/fullfran/claudeops-tui/internal/store"
)

// fakeStore implements the Store interface for testing.
type fakeStore struct {
	configData       map[string]string
	projectAggs      []store.ProjectPeriodAgg
	configGetErr     error
	configSetErr     error
	projectAggErr    error
	lastFrom, lastTo time.Time
}

func (f *fakeStore) ConfigGet(_ context.Context, key string) (string, bool, error) {
	if f.configGetErr != nil {
		return "", false, f.configGetErr
	}
	v, ok := f.configData[key]
	return v, ok, nil
}

func (f *fakeStore) ConfigSet(_ context.Context, key, value string) error {
	if f.configSetErr != nil {
		return f.configSetErr
	}
	if f.configData == nil {
		f.configData = map[string]string{}
	}
	f.configData[key] = value
	return nil
}

func (f *fakeStore) AggregatesByProjectBetween(_ context.Context, from, to time.Time) ([]store.ProjectPeriodAgg, error) {
	f.lastFrom = from
	f.lastTo = to
	if f.projectAggErr != nil {
		return nil, f.projectAggErr
	}
	return f.projectAggs, nil
}

// fakeCreds implements CredReader.
type fakeCreds struct {
	email string
	err   error
}

func (f *fakeCreds) Email() (string, error) {
	return f.email, f.err
}

// enabledCfg returns a minimal valid ExportSettings for testing.
func enabledCfg(endpoint string) config.ExportSettings {
	return config.ExportSettings{
		Enabled:  true,
		Endpoint: endpoint,
		Headers:  map[string]string{},
	}
}

func TestPusherDisabled(t *testing.T) {
	fs := &fakeStore{}
	cfg := config.ExportSettings{Enabled: false}
	p := New(fs, cfg, &fakeCreds{}, &http.Client{}, io.Discard)
	_, err := p.Push(context.Background(), PushOptions{})
	if err == nil {
		t.Fatal("expected error when export is disabled")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Errorf("expected 'disabled' in error, got %q", err.Error())
	}
}

func TestPusherNoEndpoint(t *testing.T) {
	fs := &fakeStore{}
	cfg := config.ExportSettings{Enabled: true, Endpoint: ""}
	p := New(fs, cfg, &fakeCreds{}, &http.Client{}, io.Discard)
	_, err := p.Push(context.Background(), PushOptions{})
	if err == nil {
		t.Fatal("expected error when endpoint is empty")
	}
	if !strings.Contains(err.Error(), "endpoint") {
		t.Errorf("expected 'endpoint' in error, got %q", err.Error())
	}
}

func TestPusherFirstPush_NoLastPushedAt(t *testing.T) {
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	fs := &fakeStore{
		configData:  map[string]string{},
		projectAggs: []store.ProjectPeriodAgg{{ProjectName: "myproj", CostEUR: 1.5, Sessions: 2}},
	}
	before := time.Now().Add(-31 * 24 * time.Hour)
	p := New(fs, enabledCfg(srv.URL), &fakeCreds{email: "user@example.com"}, &http.Client{}, io.Discard)

	result, err := p.Push(context.Background(), PushOptions{})
	if err != nil {
		t.Fatalf("Push() error: %v", err)
	}
	if result.DryRun {
		t.Error("expected DryRun=false")
	}
	// From should be approx 30 days ago
	if fs.lastFrom.Before(before) {
		t.Errorf("from time %v should be after %v (30d ago)", fs.lastFrom, before)
	}
	// last_pushed_at should be saved
	if _, ok := fs.configData["export.last_pushed_at"]; !ok {
		t.Error("expected export.last_pushed_at to be saved after successful push")
	}
	// Verify payload was sent
	if len(received) == 0 {
		t.Error("expected HTTP body to be sent")
	}
	var req exportRequest
	if err := json.Unmarshal(received, &req); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(req.ResourceMetrics) == 0 {
		t.Fatal("expected resource metrics in payload")
	}
	// Verify email in resource attributes
	found := false
	for _, attr := range req.ResourceMetrics[0].Resource.Attributes {
		if attr.Key == "user.email" && attr.Value.StringValue != nil && *attr.Value.StringValue == "user@example.com" {
			found = true
		}
	}
	if !found {
		t.Error("expected user.email=user@example.com in resource attributes")
	}
}

func TestPusherSecondPush_UsesStoredLastPushedAt(t *testing.T) {
	storedAt := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	fs := &fakeStore{
		configData: map[string]string{
			"export.last_pushed_at": storedAt.Format(time.RFC3339),
		},
	}
	p := New(fs, enabledCfg(srv.URL), &fakeCreds{}, &http.Client{}, io.Discard)
	_, err := p.Push(context.Background(), PushOptions{})
	if err != nil {
		t.Fatalf("Push() error: %v", err)
	}
	// from should match stored timestamp
	if !fs.lastFrom.Equal(storedAt) {
		t.Errorf("from = %v, want %v", fs.lastFrom, storedAt)
	}
}

func TestPusherDryRun(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	fs := &fakeStore{
		configData:  map[string]string{},
		projectAggs: []store.ProjectPeriodAgg{{ProjectName: "proj", CostEUR: 0.5}},
	}
	var buf bytes.Buffer
	p := New(fs, enabledCfg(srv.URL), &fakeCreds{}, &http.Client{}, &buf)
	result, err := p.Push(context.Background(), PushOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Push() dry-run error: %v", err)
	}
	if !result.DryRun {
		t.Error("expected DryRun=true in result")
	}
	if called {
		t.Error("expected NO HTTP call in dry-run mode")
	}
	if _, ok := fs.configData["export.last_pushed_at"]; ok {
		t.Error("expected last_pushed_at NOT saved in dry-run mode")
	}
	// Should have written JSON to out
	if buf.Len() == 0 {
		t.Error("expected JSON output in dry-run mode")
	}
	// Verify it is valid JSON
	var req exportRequest
	if err := json.Unmarshal(buf.Bytes(), &req); err != nil {
		t.Fatalf("dry-run output is not valid JSON: %v", err)
	}
}

func TestPusherOptsSince_OverridesStoredLastPushedAt(t *testing.T) {
	storedAt := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	since := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	fs := &fakeStore{
		configData: map[string]string{
			"export.last_pushed_at": storedAt.Format(time.RFC3339),
		},
	}
	p := New(fs, enabledCfg(srv.URL), &fakeCreds{}, &http.Client{}, io.Discard)
	_, err := p.Push(context.Background(), PushOptions{Since: &since})
	if err != nil {
		t.Fatalf("Push() error: %v", err)
	}
	if !fs.lastFrom.Equal(since) {
		t.Errorf("from = %v, want %v (opts.Since should override stored)", fs.lastFrom, since)
	}
}

func TestPusherHTTPError_LastPushedAtNotUpdated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest) // non-retryable error
	}))
	defer srv.Close()

	fs := &fakeStore{configData: map[string]string{}}
	p := New(fs, enabledCfg(srv.URL), &fakeCreds{}, &http.Client{}, io.Discard)
	_, err := p.Push(context.Background(), PushOptions{})
	if err == nil {
		t.Fatal("expected error on HTTP failure")
	}
	if _, ok := fs.configData["export.last_pushed_at"]; ok {
		t.Error("expected last_pushed_at NOT saved when HTTP fails")
	}
}

func TestPusherEmptyProjectList_Succeeds(t *testing.T) {
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	fs := &fakeStore{
		configData:  map[string]string{},
		projectAggs: []store.ProjectPeriodAgg{}, // empty
	}
	p := New(fs, enabledCfg(srv.URL), &fakeCreds{}, &http.Client{}, io.Discard)
	result, err := p.Push(context.Background(), PushOptions{})
	if err != nil {
		t.Fatalf("Push() empty list error: %v", err)
	}
	if result.DataPoints != 0 {
		t.Errorf("expected 0 data points, got %d", result.DataPoints)
	}
	// Payload should still have been sent
	if len(received) == 0 {
		t.Error("expected HTTP body sent even for empty project list")
	}
}

func TestPusherEmailFromCreds(t *testing.T) {
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	fs := &fakeStore{configData: map[string]string{}}
	p := New(fs, enabledCfg(srv.URL), &fakeCreds{email: "test@team.io"}, &http.Client{}, io.Discard)
	_, err := p.Push(context.Background(), PushOptions{})
	if err != nil {
		t.Fatalf("Push() error: %v", err)
	}

	var req exportRequest
	if err := json.Unmarshal(received, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	found := false
	for _, attr := range req.ResourceMetrics[0].Resource.Attributes {
		if attr.Key == "user.email" && attr.Value.StringValue != nil && *attr.Value.StringValue == "test@team.io" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected user.email=test@team.io in resource attributes, got %+v",
			req.ResourceMetrics[0].Resource.Attributes)
	}
}

func TestPusherUserAndTeamName(t *testing.T) {
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	fs := &fakeStore{configData: map[string]string{}}
	cfg := enabledCfg(srv.URL)
	cfg.UserName = "alice"
	cfg.TeamName = "backend-team"
	p := New(fs, cfg, &fakeCreds{}, &http.Client{}, io.Discard)
	_, err := p.Push(context.Background(), PushOptions{})
	if err != nil {
		t.Fatalf("Push() error: %v", err)
	}

	var req exportRequest
	if err := json.Unmarshal(received, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	attrs := req.ResourceMetrics[0].Resource.Attributes
	find := func(key string) string {
		for _, a := range attrs {
			if a.Key == key && a.Value.StringValue != nil {
				return *a.Value.StringValue
			}
		}
		return ""
	}
	if got := find("claudeops.user_name"); got != "alice" {
		t.Errorf("claudeops.user_name = %q, want %q", got, "alice")
	}
	if got := find("claudeops.team_name"); got != "backend-team" {
		t.Errorf("claudeops.team_name = %q, want %q", got, "backend-team")
	}
}

func TestPusherDataPointCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	fs := &fakeStore{
		configData: map[string]string{},
		projectAggs: []store.ProjectPeriodAgg{
			{ProjectName: "proj1", CostEUR: 1.0, Sessions: 1},
			{ProjectName: "proj2", CostEUR: 2.0, Sessions: 2},
		},
	}
	p := New(fs, enabledCfg(srv.URL), &fakeCreds{}, &http.Client{}, io.Discard)
	result, err := p.Push(context.Background(), PushOptions{})
	if err != nil {
		t.Fatalf("Push() error: %v", err)
	}
	// 2 projects × (1 cost + 1 sessions + 4 token types) = 12 data points
	if result.DataPoints != 12 {
		t.Errorf("DataPoints = %d, want 12", result.DataPoints)
	}
	if result.PeriodFrom.IsZero() {
		t.Error("expected PeriodFrom to be set")
	}
	if result.PeriodTo.IsZero() {
		t.Error("expected PeriodTo to be set")
	}
}

func TestPusherCredReadError_NonFatal(t *testing.T) {
	// Cred read errors should be non-fatal — push should still succeed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	fs := &fakeStore{configData: map[string]string{}}
	p := New(fs, enabledCfg(srv.URL), &fakeCreds{err: fmt.Errorf("no creds")}, &http.Client{}, io.Discard)
	_, err := p.Push(context.Background(), PushOptions{})
	if err != nil {
		t.Fatalf("Push() should succeed even when creds fail, got: %v", err)
	}
}

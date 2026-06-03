package opencode

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/fullfran/claudeops-tui/internal/source"
)

// makeFixtureDB creates a minimal opencode-schema fixture DB in dir.
// It creates message and session tables that match the real opencode.db schema.
func makeFixtureDB(t *testing.T, dir string) *sql.DB {
	t.Helper()
	path := filepath.Join(dir, "opencode.db")
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("makeFixtureDB Open: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS session (
			id        TEXT PRIMARY KEY,
			directory TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE IF NOT EXISTS message (
			id           TEXT PRIMARY KEY,
			session_id   TEXT NOT NULL,
			time_created INTEGER NOT NULL,
			time_updated INTEGER NOT NULL DEFAULT 0,
			data         TEXT NOT NULL
		);
	`)
	if err != nil {
		t.Fatalf("makeFixtureDB schema: %v", err)
	}
	return db
}

// insertSession inserts a session row and returns the session ID.
func insertSession(t *testing.T, db *sql.DB, id, directory string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO session (id, directory) VALUES (?, ?)`, id, directory)
	if err != nil {
		t.Fatalf("insertSession: %v", err)
	}
}

// insertMessage inserts a message row with the given data JSON.
func insertMessage(t *testing.T, db *sql.DB, id, sessionID string, timeCreated int64, dataJSON string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO message (id, session_id, time_created, data) VALUES (?, ?, ?, ?)`,
		id, sessionID, timeCreated, dataJSON,
	)
	if err != nil {
		t.Fatalf("insertMessage %s: %v", id, err)
	}
}

// fakeStore implements the watermark interface needed by the Ingester.
type fakeStore struct {
	watermarks map[string]string
}

func newFakeStore() *fakeStore {
	return &fakeStore{watermarks: make(map[string]string)}
}

func (f *fakeStore) LoadSourceWatermark(src string) (string, error) {
	return f.watermarks[src], nil
}

func (f *fakeStore) SaveSourceWatermark(src, pos string) error {
	f.watermarks[src] = pos
	return nil
}

// fakeSink collects emitted Records.
type fakeSink struct {
	records []source.Record
}

func (f *fakeSink) Emit(_ context.Context, r source.Record) error {
	f.records = append(f.records, r)
	return nil
}

// --- Tests ---

// TestIngesterFilterAssistantOnly verifies that only role=="assistant" rows are emitted.
// REQ-3.2
func TestIngesterFilterAssistantOnly(t *testing.T) {
	dir := t.TempDir()
	db := makeFixtureDB(t, dir)
	defer db.Close()

	insertSession(t, db, "ses1", "/home/user/myproject")
	insertMessage(t, db, "msg1", "ses1", 1000,
		`{"role":"assistant","modelID":"claude-opus-4-8","providerID":"anthropic","cost":0,
		  "tokens":{"total":500,"input":100,"output":50,"reasoning":0,"cache":{"write":0,"read":350}}}`)
	insertMessage(t, db, "msg2", "ses1", 1001,
		`{"role":"user","modelID":"","providerID":"","cost":0,
		  "tokens":{"total":0,"input":0,"output":0,"reasoning":0,"cache":{"write":0,"read":0}}}`)
	insertMessage(t, db, "msg3", "ses1", 1002,
		`{"role":"tool","modelID":"","providerID":"","cost":0,
		  "tokens":{"total":0,"input":0,"output":0,"reasoning":0,"cache":{"write":0,"read":0}}}`)

	sink := &fakeSink{}
	wm := newFakeStore()
	ing := NewIngester(filepath.Join(dir, "opencode.db"), wm, sink)

	if err := ing.IngestExisting(context.Background()); err != nil {
		t.Fatalf("IngestExisting: %v", err)
	}
	if len(sink.records) != 1 {
		t.Fatalf("expected 1 record (assistant only), got %d", len(sink.records))
	}
	if sink.records[0].UUID != "opencode:msg1" {
		t.Errorf("UUID: got %q want %q", sink.records[0].UUID, "opencode:msg1")
	}
}

// TestIngesterUUIDPrefix verifies REQ-3.7: uuid = "opencode:" + message.id.
func TestIngesterUUIDPrefix(t *testing.T) {
	dir := t.TempDir()
	db := makeFixtureDB(t, dir)
	defer db.Close()

	insertSession(t, db, "ses1", "/home/user/project")
	insertMessage(t, db, "abc123", "ses1", 1000,
		`{"role":"assistant","modelID":"claude-sonnet-4-5","providerID":"anthropic","cost":0,
		  "tokens":{"total":200,"input":50,"output":20,"reasoning":0,"cache":{"write":0,"read":130}}}`)

	sink := &fakeSink{}
	wm := newFakeStore()
	ing := NewIngester(filepath.Join(dir, "opencode.db"), wm, sink)

	if err := ing.IngestExisting(context.Background()); err != nil {
		t.Fatalf("IngestExisting: %v", err)
	}
	if len(sink.records) != 1 {
		t.Fatalf("want 1 record, got %d", len(sink.records))
	}
	want := "opencode:abc123"
	if sink.records[0].UUID != want {
		t.Errorf("UUID: got %q want %q", sink.records[0].UUID, want)
	}
	if sink.records[0].Source != source.Opencode {
		t.Errorf("Source: got %q want %q", sink.records[0].Source, source.Opencode)
	}
}

// TestIngesterProjectAttribution verifies REQ-3.5: project = filepath.Base(session.directory).
func TestIngesterProjectAttribution(t *testing.T) {
	dir := t.TempDir()
	db := makeFixtureDB(t, dir)
	defer db.Close()

	insertSession(t, db, "ses1", "/home/user/myproject")
	insertMessage(t, db, "msg1", "ses1", 1000,
		`{"role":"assistant","modelID":"claude-opus-4-8","providerID":"anthropic","cost":0,
		  "tokens":{"total":100,"input":10,"output":10,"reasoning":0,"cache":{"write":0,"read":80}}}`)

	sink := &fakeSink{}
	wm := newFakeStore()
	ing := NewIngester(filepath.Join(dir, "opencode.db"), wm, sink)

	if err := ing.IngestExisting(context.Background()); err != nil {
		t.Fatalf("IngestExisting: %v", err)
	}
	if len(sink.records) != 1 {
		t.Fatalf("want 1 record, got %d", len(sink.records))
	}
	// CWD should be session.directory; filepath.Base gives project name.
	if sink.records[0].CWD != "/home/user/myproject" {
		t.Errorf("CWD: got %q want %q", sink.records[0].CWD, "/home/user/myproject")
	}
}

// TestIngesterFallbackCWD verifies REQ-3.5: missing directory → fallback non-empty CWD.
func TestIngesterFallbackCWD(t *testing.T) {
	dir := t.TempDir()
	db := makeFixtureDB(t, dir)
	defer db.Close()

	// Insert message with no matching session
	insertMessage(t, db, "orphan1", "nonexistent-session", 1000,
		`{"role":"assistant","modelID":"claude-opus-4-8","providerID":"anthropic","cost":0,
		  "tokens":{"total":100,"input":10,"output":10,"reasoning":0,"cache":{"write":0,"read":80}}}`)

	sink := &fakeSink{}
	wm := newFakeStore()
	ing := NewIngester(filepath.Join(dir, "opencode.db"), wm, sink)

	if err := ing.IngestExisting(context.Background()); err != nil {
		t.Fatalf("IngestExisting: %v", err)
	}
	if len(sink.records) != 1 {
		t.Fatalf("want 1 record, got %d", len(sink.records))
	}
	if sink.records[0].CWD == "" {
		t.Error("CWD must never be empty (fallback must apply)")
	}
}

// TestIngesterWatermark verifies REQ-3.4: incremental polling only returns new rows.
func TestIngesterWatermark(t *testing.T) {
	dir := t.TempDir()
	db := makeFixtureDB(t, dir)
	defer db.Close()

	insertSession(t, db, "ses1", "/home/user/project")
	insertMessage(t, db, "msg1", "ses1", 1000,
		`{"role":"assistant","modelID":"claude-opus-4-8","providerID":"anthropic","cost":0,
		  "tokens":{"total":100,"input":10,"output":10,"reasoning":0,"cache":{"write":0,"read":80}}}`)
	insertMessage(t, db, "msg2", "ses1", 2000,
		`{"role":"assistant","modelID":"claude-opus-4-8","providerID":"anthropic","cost":0,
		  "tokens":{"total":100,"input":10,"output":10,"reasoning":0,"cache":{"write":0,"read":80}}}`)

	sink := &fakeSink{}
	wm := newFakeStore()
	ing := NewIngester(filepath.Join(dir, "opencode.db"), wm, sink)

	// First poll: should get both rows.
	if err := ing.IngestExisting(context.Background()); err != nil {
		t.Fatalf("first IngestExisting: %v", err)
	}
	if len(sink.records) != 2 {
		t.Fatalf("first poll: want 2 records, got %d", len(sink.records))
	}

	// Check watermark was persisted.
	pos, _ := wm.LoadSourceWatermark("opencode")
	if pos == "" {
		t.Fatal("watermark not saved after first poll")
	}

	// Second poll with same DB state: should get 0 new rows.
	sink2 := &fakeSink{}
	ing2 := NewIngester(filepath.Join(dir, "opencode.db"), wm, sink2)
	if err := ing2.IngestExisting(context.Background()); err != nil {
		t.Fatalf("second IngestExisting: %v", err)
	}
	if len(sink2.records) != 0 {
		t.Errorf("second poll: want 0 records (already seen), got %d", len(sink2.records))
	}

	// Insert a new message with a higher time_created.
	insertMessage(t, db, "msg3", "ses1", 3000,
		`{"role":"assistant","modelID":"claude-opus-4-8","providerID":"anthropic","cost":0,
		  "tokens":{"total":50,"input":5,"output":5,"reasoning":0,"cache":{"write":0,"read":40}}}`)

	// Third poll: should get only msg3.
	sink3 := &fakeSink{}
	ing3 := NewIngester(filepath.Join(dir, "opencode.db"), wm, sink3)
	if err := ing3.IngestExisting(context.Background()); err != nil {
		t.Fatalf("third IngestExisting: %v", err)
	}
	if len(sink3.records) != 1 {
		t.Fatalf("third poll: want 1 record (only msg3), got %d", len(sink3.records))
	}
	if sink3.records[0].UUID != "opencode:msg3" {
		t.Errorf("third poll UUID: got %q want %q", sink3.records[0].UUID, "opencode:msg3")
	}
}

// TestIngesterModelNormalization verifies REQ-3.6: provider/model normalization
// is applied and the canonical key is stored in Record.Model.
func TestIngesterModelNormalization(t *testing.T) {
	dir := t.TempDir()
	db := makeFixtureDB(t, dir)
	defer db.Close()

	insertSession(t, db, "ses1", "/home/user/project")
	// Anthropic model with dots in version (should normalize).
	insertMessage(t, db, "msg1", "ses1", 1000,
		`{"role":"assistant","modelID":"claude-opus-4.6","providerID":"anthropic","cost":0,
		  "tokens":{"total":100,"input":10,"output":10,"reasoning":0,"cache":{"write":0,"read":80}}}`)
	// Non-Anthropic model (should be qualified).
	insertMessage(t, db, "msg2", "ses1", 2000,
		`{"role":"assistant","modelID":"gpt-4o","providerID":"openai","cost":0.01,
		  "tokens":{"total":100,"input":10,"output":10,"reasoning":0,"cache":{"write":0,"read":0}}}`)

	sink := &fakeSink{}
	wm := newFakeStore()
	ing := NewIngester(filepath.Join(dir, "opencode.db"), wm, sink)

	if err := ing.IngestExisting(context.Background()); err != nil {
		t.Fatalf("IngestExisting: %v", err)
	}
	if len(sink.records) != 2 {
		t.Fatalf("want 2 records, got %d", len(sink.records))
	}
	// Find by UUID.
	recs := make(map[string]source.Record)
	for _, r := range sink.records {
		recs[r.UUID] = r
	}

	if r, ok := recs["opencode:msg1"]; ok {
		if r.Model != "claude-opus-4-6" {
			t.Errorf("anthropic model normalization: got %q want %q", r.Model, "claude-opus-4-6")
		}
	} else {
		t.Error("msg1 not found")
	}
	if r, ok := recs["opencode:msg2"]; ok {
		if r.Model != "openai/gpt-4o" {
			t.Errorf("openai model qualification: got %q want %q", r.Model, "openai/gpt-4o")
		}
	} else {
		t.Error("msg2 not found")
	}
}

// TestIngesterCostIgnored verifies REQ-3.3: data.cost is ignored even when non-zero.
// The Ingester must emit the Record with the model key (so the Sink can price it);
// it must NOT copy data.cost into the record.
func TestIngesterCostIgnored(t *testing.T) {
	dir := t.TempDir()
	db := makeFixtureDB(t, dir)
	defer db.Close()

	insertSession(t, db, "ses1", "/home/user/project")
	insertMessage(t, db, "msg1", "ses1", 1000,
		`{"role":"assistant","modelID":"claude-opus-4-8","providerID":"anthropic","cost":9999.99,
		  "tokens":{"total":100,"input":10,"output":10,"reasoning":0,"cache":{"write":0,"read":80}}}`)

	sink := &fakeSink{}
	wm := newFakeStore()
	ing := NewIngester(filepath.Join(dir, "opencode.db"), wm, sink)

	if err := ing.IngestExisting(context.Background()); err != nil {
		t.Fatalf("IngestExisting: %v", err)
	}
	if len(sink.records) != 1 {
		t.Fatalf("want 1 record, got %d", len(sink.records))
	}
	// The Record carries no cost field — cost is computed by the Sink.
	// We verify that the Ingester did not add any cost-related field to Record.
	// source.Record has no Cost field by design — nothing to assert here except
	// that the emit happened (tokens were recorded) and model is correctly set.
	r := sink.records[0]
	if r.Model != "claude-opus-4-8" {
		t.Errorf("Model: got %q want %q", r.Model, "claude-opus-4-8")
	}
	if r.In != 10 {
		t.Errorf("In: got %d want 10", r.In)
	}
}

// TestIngesterSessionIDPrefix verifies that SessionID is prefixed with "opencode:".
func TestIngesterSessionIDPrefix(t *testing.T) {
	dir := t.TempDir()
	db := makeFixtureDB(t, dir)
	defer db.Close()

	insertSession(t, db, "ses-abc", "/home/user/project")
	insertMessage(t, db, "msg1", "ses-abc", 1000,
		`{"role":"assistant","modelID":"claude-opus-4-8","providerID":"anthropic","cost":0,
		  "tokens":{"total":100,"input":10,"output":10,"reasoning":0,"cache":{"write":0,"read":80}}}`)

	sink := &fakeSink{}
	wm := newFakeStore()
	ing := NewIngester(filepath.Join(dir, "opencode.db"), wm, sink)

	if err := ing.IngestExisting(context.Background()); err != nil {
		t.Fatalf("IngestExisting: %v", err)
	}
	if len(sink.records) != 1 {
		t.Fatalf("want 1 record, got %d", len(sink.records))
	}
	want := "opencode:ses-abc"
	if sink.records[0].SessionID != want {
		t.Errorf("SessionID: got %q want %q", sink.records[0].SessionID, want)
	}
}

// TestIngesterTimestamp verifies that the TS field is set from time_created (epoch-ms).
func TestIngesterTimestamp(t *testing.T) {
	dir := t.TempDir()
	db := makeFixtureDB(t, dir)
	defer db.Close()

	// 1779661848629 ms = 2026-04-24T... — just verify non-zero and correct scale.
	insertSession(t, db, "ses1", "/home/user/project")
	insertMessage(t, db, "msg1", "ses1", 1779661848629,
		`{"role":"assistant","modelID":"claude-opus-4-8","providerID":"anthropic","cost":0,
		  "tokens":{"total":100,"input":10,"output":10,"reasoning":0,"cache":{"write":0,"read":80}}}`)

	sink := &fakeSink{}
	wm := newFakeStore()
	ing := NewIngester(filepath.Join(dir, "opencode.db"), wm, sink)

	if err := ing.IngestExisting(context.Background()); err != nil {
		t.Fatalf("IngestExisting: %v", err)
	}
	if len(sink.records) != 1 {
		t.Fatalf("want 1 record, got %d", len(sink.records))
	}
	ts := sink.records[0].TS
	if ts.IsZero() {
		t.Fatal("TS must not be zero")
	}
	// Epoch ms 1779661848629 → year ~2026.
	if ts.Year() < 2020 || ts.Year() > 2100 {
		t.Errorf("TS year looks wrong: %d (ts=%v)", ts.Year(), ts)
	}
}

// TestIngesterName verifies the Ingester.Name() method.
func TestIngesterName(t *testing.T) {
	wm := newFakeStore()
	sink := &fakeSink{}
	ing := NewIngester("/some/path", wm, sink)
	if ing.Name() != source.Opencode {
		t.Errorf("Name(): got %q want %q", ing.Name(), source.Opencode)
	}
}

// TestIngesterWatchCancels verifies that Watch returns on ctx.Done.
func TestIngesterWatchCancels(t *testing.T) {
	dir := t.TempDir()
	db := makeFixtureDB(t, dir)
	db.Close()

	wm := newFakeStore()
	sink := &fakeSink{}
	ing := NewIngester(filepath.Join(dir, "opencode.db"), wm, sink)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- ing.Watch(ctx) }()
	select {
	case err := <-done:
		if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
			t.Errorf("Watch returned unexpected error: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Watch did not return after ctx.Done")
	}
}

// TestIngesterMissingDB verifies graceful handling of a missing opencode.db
// (opencode not installed or DB path wrong). IngestExisting must not panic
// and should return an error or silently succeed (skip).
func TestIngesterMissingDB(t *testing.T) {
	wm := newFakeStore()
	sink := &fakeSink{}
	ing := NewIngester("/nonexistent/path/opencode.db", wm, sink)

	// We don't mandate the exact behavior — but it must not panic.
	_ = ing.IngestExisting(context.Background())
	// No records should have been emitted.
	if len(sink.records) != 0 {
		t.Errorf("expected 0 records from missing DB, got %d", len(sink.records))
	}
}

// TestIngesterRealDB is an integration test that reads from the real
// ~/.local/share/opencode/opencode.db. It is skipped under -short.
func TestIngesterRealDB(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real DB integration test in -short mode")
	}
	home, err := homeDir()
	if err != nil {
		t.Skip("cannot resolve home dir:", err)
	}
	dbPath := filepath.Join(home, ".local", "share", "opencode", "opencode.db")
	if !fileExists(dbPath) {
		t.Skip("opencode.db not found at", dbPath)
	}

	wm := newFakeStore()
	sink := &fakeSink{}
	ing := NewIngester(dbPath, wm, sink)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := ing.IngestExisting(ctx); err != nil {
		t.Fatalf("IngestExisting on real DB: %v", err)
	}
	t.Logf("real DB: ingested %d records", len(sink.records))
	// Spot-check: all records must have non-empty UUID, CWD, source.
	for i, r := range sink.records {
		if r.UUID == "" {
			t.Errorf("record[%d]: empty UUID", i)
		}
		if r.CWD == "" {
			t.Errorf("record[%d]: empty CWD", i)
		}
		if r.Source != source.Opencode {
			t.Errorf("record[%d]: Source=%q want %q", i, r.Source, source.Opencode)
		}
	}
}

package opencode

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/fullfran/claudeops-tui/internal/pricing"
	"github.com/fullfran/claudeops-tui/internal/source"
	"github.com/fullfran/claudeops-tui/internal/store"
)

// makeFixtureDB creates a minimal opencode-schema fixture DB in dir.
// It creates message and session tables that match the real opencode.db schema.
func makeFixtureDB(t *testing.T, dir string) *sql.DB {
	t.Helper()
	path := filepath.Join(dir, "opencode.db")
	// synchronous(off) keeps the fixture writes off the fsync path; the DB is
	// discarded with the temp dir, so durability is irrelevant here.
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(off)"
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
	defer func() { _ = db.Close() }()

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
	defer func() { _ = db.Close() }()

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
	defer func() { _ = db.Close() }()

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
	defer func() { _ = db.Close() }()

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
	defer func() { _ = db.Close() }()

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

	// Second poll with same DB state: only the boundary row is re-read, never a
	// row below the watermark.
	sink2 := &fakeSink{}
	ing2 := NewIngester(filepath.Join(dir, "opencode.db"), wm, sink2)
	if err := ing2.IngestExisting(context.Background()); err != nil {
		t.Fatalf("second IngestExisting: %v", err)
	}
	if got := uuids(sink2.records); len(got) != 1 || got[0] != "opencode:msg2" {
		t.Errorf("second poll: want only the boundary row, got %v", got)
	}

	// Insert a new message with a higher time_created.
	insertMessage(t, db, "msg3", "ses1", 3000,
		`{"role":"assistant","modelID":"claude-opus-4-8","providerID":"anthropic","cost":0,
		  "tokens":{"total":50,"input":5,"output":5,"reasoning":0,"cache":{"write":0,"read":40}}}`)

	// Third poll: the new row plus the boundary row, never msg1.
	sink3 := &fakeSink{}
	ing3 := NewIngester(filepath.Join(dir, "opencode.db"), wm, sink3)
	if err := ing3.IngestExisting(context.Background()); err != nil {
		t.Fatalf("third IngestExisting: %v", err)
	}
	got := uuids(sink3.records)
	if !contains(got, "opencode:msg3") {
		t.Errorf("third poll: msg3 missing, got %v", got)
	}
	if contains(got, "opencode:msg1") {
		t.Errorf("third poll: msg1 re-read below the watermark, got %v", got)
	}
}

// TestIngesterModelNormalization verifies REQ-3.6: provider/model normalization
// is applied and the canonical key is stored in Record.Model.
func TestIngesterModelNormalization(t *testing.T) {
	dir := t.TempDir()
	db := makeFixtureDB(t, dir)
	defer func() { _ = db.Close() }()

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
	defer func() { _ = db.Close() }()

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
	defer func() { _ = db.Close() }()

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
	defer func() { _ = db.Close() }()

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
	// Closed deliberately so the ingester reopens the file by path; a failed
	// close would leave the fixture locked and the reopen would not be tested.
	if err := db.Close(); err != nil {
		t.Fatalf("close fixture db: %v", err)
	}

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
// ~/.local/share/opencode/opencode.db. It is opt-in via CLAUDEOPS_INTEGRATION=1
// so `go test ./...` — the command CI runs — stays hermetic.
func TestIngesterRealDB(t *testing.T) {
	if os.Getenv("CLAUDEOPS_INTEGRATION") != "1" {
		t.Skip("skipping real DB integration test; set CLAUDEOPS_INTEGRATION=1 to run")
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

	// Generous deadline: a multi-GB DB takes a while, and a truncated scan now
	// fails loudly instead of passing with zero records.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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

// assistantData builds a minimal assistant data blob with the given output tokens.
func assistantData(out int64) string {
	return fmt.Sprintf(`{"role":"assistant","modelID":"claude-opus-4-8","providerID":"anthropic","cost":0,
	  "tokens":{"total":%d,"input":10,"output":%d,"reasoning":0,"cache":{"write":0,"read":0}}}`, out+10, out)
}

// failingSink fails Emit for the UUIDs listed in fail, recording every other Record.
type failingSink struct {
	records []source.Record
	fail    map[string]bool
}

func (f *failingSink) Emit(_ context.Context, r source.Record) error {
	if f.fail[r.UUID] {
		return errors.New("emit failed")
	}
	f.records = append(f.records, r)
	return nil
}

func uuids(recs []source.Record) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.UUID)
	}
	return out
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// TestIngesterEmitFailureDoesNotAdvanceWatermark verifies that a row whose Emit
// fails is retried on the next poll instead of being permanently skipped (#34).
func TestIngesterEmitFailureDoesNotAdvanceWatermark(t *testing.T) {
	dir := t.TempDir()
	db := makeFixtureDB(t, dir)
	defer func() { _ = db.Close() }()

	insertSession(t, db, "ses1", "/home/user/project")
	insertMessage(t, db, "msg1", "ses1", 1000, assistantData(10))
	insertMessage(t, db, "msg2", "ses1", 2000, assistantData(20))
	insertMessage(t, db, "msg3", "ses1", 3000, assistantData(30))

	wm := newFakeStore()
	bad := &failingSink{fail: map[string]bool{"opencode:msg2": true}}
	ing := NewIngester(filepath.Join(dir, "opencode.db"), wm, bad)

	if err := ing.IngestExisting(context.Background()); err == nil {
		t.Fatal("IngestExisting: want error when Emit fails, got nil")
	}

	good := &fakeSink{}
	ing2 := NewIngester(filepath.Join(dir, "opencode.db"), wm, good)
	if err := ing2.IngestExisting(context.Background()); err != nil {
		t.Fatalf("second IngestExisting: %v", err)
	}
	got := uuids(good.records)
	for _, want := range []string{"opencode:msg2", "opencode:msg3"} {
		if !contains(got, want) {
			t.Errorf("retry poll: %q not re-read, got %v", want, got)
		}
	}
}

// TestIngesterBoundaryTimestamps verifies that rows committed late with a
// time_created equal to the watermark are still picked up (#34).
func TestIngesterBoundaryTimestamps(t *testing.T) {
	dir := t.TempDir()
	db := makeFixtureDB(t, dir)
	defer func() { _ = db.Close() }()

	insertSession(t, db, "ses1", "/home/user/project")
	for _, id := range []string{"msg1", "msg2", "msg3"} {
		insertMessage(t, db, id, "ses1", 1000, assistantData(10))
	}

	wm := newFakeStore()
	sink := &fakeSink{}
	ing := NewIngester(filepath.Join(dir, "opencode.db"), wm, sink)
	if err := ing.IngestExisting(context.Background()); err != nil {
		t.Fatalf("first IngestExisting: %v", err)
	}
	if len(sink.records) != 3 {
		t.Fatalf("first poll: want 3 records, got %d", len(sink.records))
	}

	// A fourth row commits after the first poll's snapshot with the same
	// time_created as the watermark.
	insertMessage(t, db, "msg4", "ses1", 1000, assistantData(10))

	sink2 := &fakeSink{}
	ing2 := NewIngester(filepath.Join(dir, "opencode.db"), wm, sink2)
	if err := ing2.IngestExisting(context.Background()); err != nil {
		t.Fatalf("second IngestExisting: %v", err)
	}
	if !contains(uuids(sink2.records), "opencode:msg4") {
		t.Errorf("boundary row never re-read, got %v", uuids(sink2.records))
	}
}

// TestIngesterPartialRowCorrected verifies that a row read mid-write is re-read
// on the next poll so the store-side corrective upsert can repair it (#34).
func TestIngesterPartialRowCorrected(t *testing.T) {
	dir := t.TempDir()
	db := makeFixtureDB(t, dir)
	defer func() { _ = db.Close() }()

	insertSession(t, db, "ses1", "/home/user/project")
	insertMessage(t, db, "msg1", "ses1", 1000, assistantData(5))

	s, err := store.Open(filepath.Join(dir, "claudeops.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	tbl, err := pricing.LoadOrSeed(filepath.Join(dir, "pricing.toml"))
	if err != nil {
		t.Fatalf("pricing.LoadOrSeed: %v", err)
	}
	sink := source.NewStoreSink(s, pricing.NewCalculator(tbl))

	ing := NewIngester(filepath.Join(dir, "opencode.db"), newFakeStore(), sink)
	if err := ing.IngestExisting(context.Background()); err != nil {
		t.Fatalf("first IngestExisting: %v", err)
	}
	if got := outTokens(t, s, "opencode:msg1"); got != 5 {
		t.Fatalf("after partial poll: out_tokens=%d want 5", got)
	}

	// The row finishes streaming: same time_created, final token counts.
	if _, err := db.Exec(`UPDATE message SET data = ? WHERE id = 'msg1'`, assistantData(50)); err != nil {
		t.Fatalf("update message: %v", err)
	}

	if err := ing.IngestExisting(context.Background()); err != nil {
		t.Fatalf("second IngestExisting: %v", err)
	}
	if got := outTokens(t, s, "opencode:msg1"); got != 50 {
		t.Errorf("after corrective poll: out_tokens=%d want 50", got)
	}
}

// outTokens reads the stored out_tokens for a single event uuid.
func outTokens(t *testing.T, s *store.Store, uuid string) int64 {
	t.Helper()
	var out int64
	if err := s.DB().QueryRow(`SELECT out_tokens FROM events WHERE uuid = ?`, uuid).Scan(&out); err != nil {
		t.Fatalf("query out_tokens for %s: %v", uuid, err)
	}
	return out
}

// TestIngesterSchemaMismatch verifies that a DB whose schema does not match the
// expected opencode layout surfaces an error instead of silently ingesting
// nothing forever (#53).
func TestIngesterSchemaMismatch(t *testing.T) {
	tests := []struct {
		name   string
		schema string
	}{
		{
			name:   "no message table",
			schema: `CREATE TABLE session (id TEXT PRIMARY KEY, directory TEXT NOT NULL DEFAULT '')`,
		},
		{
			name: "renamed column",
			schema: `CREATE TABLE session (id TEXT PRIMARY KEY, directory TEXT NOT NULL DEFAULT '');
				CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, created_at INTEGER NOT NULL, data TEXT NOT NULL)`,
		},
		{
			name:   "no session table",
			schema: `CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, time_created INTEGER NOT NULL, data TEXT NOT NULL)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "opencode.db")
			db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
			if err != nil {
				t.Fatalf("open fixture: %v", err)
			}
			defer func() { _ = db.Close() }()
			if _, err := db.Exec(tt.schema); err != nil {
				t.Fatalf("fixture schema: %v", err)
			}

			sink := &fakeSink{}
			ing := NewIngester(path, newFakeStore(), sink)
			if err := ing.IngestExisting(context.Background()); err == nil {
				t.Fatal("IngestExisting: want error on schema mismatch, got nil")
			}
			if ing.LastErr() == nil {
				t.Error("LastErr: want non-nil after schema mismatch")
			}
			if len(sink.records) != 0 {
				t.Errorf("want 0 records, got %d", len(sink.records))
			}
		})
	}
}

// TestIngesterMissingDBIsSilent verifies that an absent DB (opencode not
// installed) stays a silent no-op rather than a reported failure.
func TestIngesterMissingDBIsSilent(t *testing.T) {
	ing := NewIngester(filepath.Join(t.TempDir(), "opencode.db"), newFakeStore(), &fakeSink{})
	if err := ing.IngestExisting(context.Background()); err != nil {
		t.Errorf("IngestExisting on missing DB: %v", err)
	}
	if ing.LastErr() != nil {
		t.Errorf("LastErr on missing DB: %v", ing.LastErr())
	}
}

package agy

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fullfran/claudeops-tui/internal/source"
)

// --- fixtures ---------------------------------------------------------
//
// Every fixture here is built from the verified agy 1.2.7 schema in
// odd/tasks/agy-support.md, never from real user data — see wire_test.go's
// note on the privacy constraint.

// makeConversationDB creates a "<id>.db" fixture in dir matching agy's
// verified gen_metadata/steps schema.
func makeConversationDB(t *testing.T, dir, id string) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(dir, id+".db")
	// synchronous(off) keeps fixture writes off the fsync path — the DB is
	// discarded with the temp dir, so durability is irrelevant here, mirroring
	// internal/opencode's makeFixtureDB.
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(off)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open conversation db: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE gen_metadata (idx integer, data blob, size integer NOT NULL DEFAULT 0, PRIMARY KEY (idx));
		CREATE TABLE steps (idx integer, step_type integer NOT NULL DEFAULT 0, status integer NOT NULL DEFAULT 0,
			has_subtrajectory numeric NOT NULL DEFAULT false, metadata blob, error_details blob, permissions blob,
			task_details blob, render_info blob, step_payload blob, step_format integer NOT NULL DEFAULT 0,
			PRIMARY KEY (idx));
	`)
	if err != nil {
		t.Fatalf("conversation schema: %v", err)
	}
	return db, path
}

func insertGenMetadata(t *testing.T, db *sql.DB, idx int64, data []byte) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO gen_metadata (idx, data, size) VALUES (?, ?, ?)`, idx, data, len(data)); err != nil {
		t.Fatalf("insertGenMetadata idx=%d: %v", idx, err)
	}
}

func insertStep(t *testing.T, db *sql.DB, idx int64, metadata []byte) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO steps (idx, metadata) VALUES (?, ?)`, idx, metadata); err != nil {
		t.Fatalf("insertStep idx=%d: %v", idx, err)
	}
}

// makeSummariesDB creates conversation_summaries.db in dir (agy's data root,
// not the conversations subdirectory) with one row per entry in workspaces.
func makeSummariesDB(t *testing.T, dir string, workspaces map[string]string) {
	t.Helper()
	path := filepath.Join(dir, summariesDBFileName)
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open summaries db: %v", err)
	}
	defer func() { _ = db.Close() }()
	_, err = db.Exec(`CREATE TABLE conversation_summaries (
		conversation_id text PRIMARY KEY,
		workspace_uris text NOT NULL,
		parent_conversation_id text
	)`)
	if err != nil {
		t.Fatalf("summaries schema: %v", err)
	}
	for id, uris := range workspaces {
		if _, err := db.Exec(`INSERT INTO conversation_summaries (conversation_id, workspace_uris) VALUES (?, ?)`, id, uris); err != nil {
			t.Fatalf("insert summary %s: %v", id, err)
		}
	}
}

// fakeStore implements WatermarkStore for tests.
type fakeStore struct {
	mu         sync.Mutex
	watermarks map[string]string
}

func newFakeStore() *fakeStore {
	return &fakeStore{watermarks: map[string]string{}}
}

func (f *fakeStore) LoadSourceWatermark(src string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.watermarks[src], nil
}

func (f *fakeStore) SaveSourceWatermark(src, pos string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.watermarks[src] = pos
	return nil
}

// fakeSink collects emitted records.
type fakeSink struct {
	mu      sync.Mutex
	records []source.Record
}

func (f *fakeSink) Emit(_ context.Context, r source.Record) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, r)
	return nil
}

func (f *fakeSink) Records() []source.Record {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]source.Record(nil), f.records...)
}

// --- tests --------------------------------------------------------------

func TestIngesterEmitsRecordsFromTwoRowFixture(t *testing.T) {
	dir := t.TempDir()
	convDir := filepath.Join(dir, conversationsDirName)
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}

	const convID = "11111111-1111-1111-1111-111111111111"
	db, _ := makeConversationDB(t, convDir, convID)
	defer func() { _ = db.Close() }()

	usage1 := buildUsage(100, 20, 0, 5, "bot-msg-1")
	insertGenMetadata(t, db, 1, buildGenMetadata(buildGenMessage("gemini-3.8-flash-exp-a", usage1)))
	insertStep(t, db, 1, buildStepMetadata(buildTimestamp(1790000000, 0), usage1))

	usage2 := buildUsage(50, 10, 0, 0, "bot-msg-2")
	insertGenMetadata(t, db, 2, buildGenMetadata(buildGenMessage("claude-sonnet-4-6", usage2)))
	insertStep(t, db, 2, buildStepMetadata(buildTimestamp(1790000060, 0), usage2))

	makeSummariesDB(t, dir, map[string]string{convID: "file:///home/user/myproject"})

	sink := &fakeSink{}
	ing := NewIngester(dir, newFakeStore(), sink)

	if err := ing.IngestExisting(context.Background()); err != nil {
		t.Fatalf("IngestExisting: %v", err)
	}
	recs := sink.Records()
	if len(recs) != 2 {
		t.Fatalf("want 2 records, got %d", len(recs))
	}

	r1 := recs[0]
	if r1.UUID != "agy:"+convID+":bot-msg-1" {
		t.Errorf("UUID: got %q", r1.UUID)
	}
	if r1.SessionID != "agy:"+convID {
		t.Errorf("SessionID: got %q", r1.SessionID)
	}
	if r1.CWD != "/home/user/myproject" {
		t.Errorf("CWD: got %q, want %q", r1.CWD, "/home/user/myproject")
	}
	if r1.Model != "gemini-3.8-flash" {
		t.Errorf("Model: got %q, want %q", r1.Model, "gemini-3.8-flash")
	}
	if r1.In != 100 || r1.Out != 20 || r1.CacheRead != 5 {
		t.Errorf("tokens: got In=%d Out=%d CacheRead=%d, want 100/20/5", r1.In, r1.Out, r1.CacheRead)
	}
	wantTS := time.Unix(1790000000, 0).UTC()
	if !r1.TS.Equal(wantTS) {
		t.Errorf("TS: got %v, want %v", r1.TS, wantTS)
	}
	if r1.Source != source.Agy {
		t.Errorf("Source: got %q, want %q", r1.Source, source.Agy)
	}
	if r1.Type != "assistant" {
		t.Errorf("Type: got %q, want %q", r1.Type, "assistant")
	}
	if r1.ReportedCostUSD != nil {
		t.Errorf("ReportedCostUSD: got %v, want nil", *r1.ReportedCostUSD)
	}

	if recs[1].Model != "claude-sonnet-4-6" {
		t.Errorf("row 2 Model: got %q, want %q", recs[1].Model, "claude-sonnet-4-6")
	}
}

func TestIngesterSecondPollEmitsNothing(t *testing.T) {
	dir := t.TempDir()
	convDir := filepath.Join(dir, conversationsDirName)
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const convID = "22222222-2222-2222-2222-222222222222"
	db, _ := makeConversationDB(t, convDir, convID)
	defer func() { _ = db.Close() }()

	usage := buildUsage(10, 5, 0, 0, "bot-1")
	insertGenMetadata(t, db, 1, buildGenMetadata(buildGenMessage("claude-sonnet-4-6", usage)))
	insertStep(t, db, 1, buildStepMetadata(buildTimestamp(1790000000, 0), usage))

	wm := newFakeStore()
	sink1 := &fakeSink{}
	if err := NewIngester(dir, wm, sink1).IngestExisting(context.Background()); err != nil {
		t.Fatalf("first poll: %v", err)
	}
	if len(sink1.Records()) != 1 {
		t.Fatalf("first poll: want 1 record, got %d", len(sink1.Records()))
	}

	sink2 := &fakeSink{}
	if err := NewIngester(dir, wm, sink2).IngestExisting(context.Background()); err != nil {
		t.Fatalf("second poll: %v", err)
	}
	if got := len(sink2.Records()); got != 0 {
		t.Fatalf("second poll: want 0 records, got %d", got)
	}
}

// agy numbers gen_metadata rows from 0, so the first model call of every
// conversation sits at idx 0. A missing watermark must not be read as "0 already
// emitted", or that call is dropped from every conversation.
func TestIngesterEmitsRowAtIdxZero(t *testing.T) {
	dir := t.TempDir()
	convDir := filepath.Join(dir, conversationsDirName)
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const convID = "00000000-0000-0000-0000-000000000000"
	db, _ := makeConversationDB(t, convDir, convID)
	defer func() { _ = db.Close() }()

	usage0 := buildUsage(15056, 111, 0, 0, "bot-0")
	insertGenMetadata(t, db, 0, buildGenMetadata(buildGenMessage("gemini-3.8-flash-exp-a", usage0)))
	insertStep(t, db, 1, buildStepMetadata(buildTimestamp(1790000000, 0), usage0))
	usage1 := buildUsage(5600, 1204, 0, 12203, "bot-1")
	insertGenMetadata(t, db, 1, buildGenMetadata(buildGenMessage("gemini-3.8-flash-exp-a", usage1)))
	insertStep(t, db, 3, buildStepMetadata(buildTimestamp(1790000060, 0), usage1))

	wm := newFakeStore()
	sink := &fakeSink{}
	if err := NewIngester(dir, wm, sink).IngestExisting(context.Background()); err != nil {
		t.Fatalf("poll: %v", err)
	}
	recs := sink.Records()
	if len(recs) != 2 {
		t.Fatalf("want 2 records (idx 0 and 1), got %d", len(recs))
	}
	if recs[0].In != 15056 {
		t.Errorf("first record: want the idx 0 call (In=15056), got In=%d", recs[0].In)
	}

	again := &fakeSink{}
	if err := NewIngester(dir, wm, again).IngestExisting(context.Background()); err != nil {
		t.Fatalf("second poll: %v", err)
	}
	if got := len(again.Records()); got != 0 {
		t.Fatalf("second poll: want 0 records, got %d", got)
	}
}

func TestIngesterAppendedRowEmittedOnNextPoll(t *testing.T) {
	dir := t.TempDir()
	convDir := filepath.Join(dir, conversationsDirName)
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const convID = "33333333-3333-3333-3333-333333333333"
	db, _ := makeConversationDB(t, convDir, convID)
	defer func() { _ = db.Close() }()

	usage1 := buildUsage(10, 5, 0, 0, "bot-1")
	insertGenMetadata(t, db, 1, buildGenMetadata(buildGenMessage("claude-sonnet-4-6", usage1)))
	insertStep(t, db, 1, buildStepMetadata(buildTimestamp(1790000000, 0), usage1))

	wm := newFakeStore()
	sink1 := &fakeSink{}
	if err := NewIngester(dir, wm, sink1).IngestExisting(context.Background()); err != nil {
		t.Fatalf("first poll: %v", err)
	}
	if len(sink1.Records()) != 1 {
		t.Fatalf("first poll: want 1 record, got %d", len(sink1.Records()))
	}

	// A row appended after the first poll, resolvable via its own step (so
	// the 2-minute deferral rule never enters into this test).
	usage2 := buildUsage(20, 8, 0, 0, "bot-2")
	insertGenMetadata(t, db, 2, buildGenMetadata(buildGenMessage("claude-sonnet-4-6", usage2)))
	insertStep(t, db, 2, buildStepMetadata(buildTimestamp(1790000100, 0), usage2))

	sink2 := &fakeSink{}
	if err := NewIngester(dir, wm, sink2).IngestExisting(context.Background()); err != nil {
		t.Fatalf("second poll: %v", err)
	}
	recs := sink2.Records()
	if len(recs) != 1 {
		t.Fatalf("second poll: want 1 new record, got %d", len(recs))
	}
	if recs[0].UUID != "agy:"+convID+":bot-2" {
		t.Errorf("UUID: got %q", recs[0].UUID)
	}
}

func TestIngesterMalformedBlobSkippedRestEmit(t *testing.T) {
	dir := t.TempDir()
	convDir := filepath.Join(dir, conversationsDirName)
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const convID = "44444444-4444-4444-4444-444444444444"
	db, _ := makeConversationDB(t, convDir, convID)
	defer func() { _ = db.Close() }()

	usage1 := buildUsage(10, 5, 0, 0, "bot-good-1")
	insertGenMetadata(t, db, 1, buildGenMetadata(buildGenMessage("claude-sonnet-4-6", usage1)))
	insertStep(t, db, 1, buildStepMetadata(buildTimestamp(1790000000, 0), usage1))

	insertGenMetadata(t, db, 2, []byte{0xFF, 0xFF, 0xFF}) // malformed protobuf

	usage3 := buildUsage(30, 15, 0, 0, "bot-good-2")
	insertGenMetadata(t, db, 3, buildGenMetadata(buildGenMessage("claude-sonnet-4-6", usage3)))
	insertStep(t, db, 3, buildStepMetadata(buildTimestamp(1790000200, 0), usage3))

	var warnings []string
	sink := &fakeSink{}
	wm := newFakeStore()
	ing := NewIngester(dir, wm, sink)
	ing.OnWarn = func(msg string) { warnings = append(warnings, msg) }

	if err := ing.IngestExisting(context.Background()); err != nil {
		t.Fatalf("IngestExisting: %v", err)
	}
	recs := sink.Records()
	if len(recs) != 2 {
		t.Fatalf("want 2 records (malformed row skipped), got %d", len(recs))
	}
	if len(warnings) == 0 {
		t.Error("want a warning for the malformed row, got none")
	}
	for _, r := range recs {
		if r.UUID == "agy:"+convID+":gen:2" {
			t.Error("the malformed row was emitted")
		}
	}
	// The watermark advances past the malformed row too — it was read and
	// deliberately skipped, unlike a deferred row.
	pos, _ := wm.LoadSourceWatermark("agy:" + convID)
	if pos != "3" {
		t.Errorf("watermark: got %q, want %q", pos, "3")
	}
	// `claudeops ingest` prints these, so they must match what was emitted
	// and skipped rather than read 0 for a DB poller.
	if got := ing.IngestedCount(); got != 2 {
		t.Errorf("IngestedCount: got %d, want 2", got)
	}
	if got := ing.ParseErrorCount(); got != 1 {
		t.Errorf("ParseErrorCount: got %d, want 1", got)
	}
}

func TestIngesterMissingTableBlocksOnlyThatFile(t *testing.T) {
	dir := t.TempDir()
	convDir := filepath.Join(dir, conversationsDirName)
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Bad conversation: has a steps table but no gen_metadata table at all.
	badPath := filepath.Join(convDir, "bad-conversation.db")
	badDB, err := sql.Open("sqlite", "file:"+badPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := badDB.Exec(`CREATE TABLE steps (idx integer, metadata blob, PRIMARY KEY(idx))`); err != nil {
		t.Fatal(err)
	}
	if err := badDB.Close(); err != nil {
		t.Fatal(err)
	}

	const goodID = "55555555-5555-5555-5555-555555555555"
	goodDB, _ := makeConversationDB(t, convDir, goodID)
	defer func() { _ = goodDB.Close() }()
	usage := buildUsage(10, 5, 0, 0, "bot-good")
	insertGenMetadata(t, goodDB, 1, buildGenMetadata(buildGenMessage("claude-sonnet-4-6", usage)))
	insertStep(t, goodDB, 1, buildStepMetadata(buildTimestamp(1790000000, 0), usage))

	sink := &fakeSink{}
	ing := NewIngester(dir, newFakeStore(), sink)

	if err := ing.IngestExisting(context.Background()); err == nil {
		t.Fatal("IngestExisting: want an error because one file failed, got nil")
	}
	if ing.LastErr() == nil {
		t.Error("LastErr: want non-nil after a file failure")
	}
	recs := sink.Records()
	if len(recs) != 1 {
		t.Fatalf("good file: want 1 record despite the other file's failure, got %d", len(recs))
	}
	if recs[0].UUID != "agy:"+goodID+":bot-good" {
		t.Errorf("UUID: got %q", recs[0].UUID)
	}
}

func TestIngesterTimestampFallbackViaLastStepIndex(t *testing.T) {
	dir := t.TempDir()
	convDir := filepath.Join(dir, conversationsDirName)
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const convID = "66666666-6666-6666-6666-666666666666"
	db, _ := makeConversationDB(t, convDir, convID)
	defer func() { _ = db.Close() }()

	// The usage message id has no matching step (no 9.7 join target), but
	// last_step_index names a step that does carry a timestamp.
	usage := buildUsage(10, 5, 0, 0, "bot-unmatched")
	gen := buildGenMessage("claude-sonnet-4-6", usage, buildKV(kvLastStepIndex, "9"))
	insertGenMetadata(t, db, 1, buildGenMetadata(gen))

	// Step 9 carries only a timestamp, no usage submessage — most steps, per
	// the verified schema.
	insertStep(t, db, 9, buildStepMetadata(buildTimestamp(1790000555, 0), nil))

	sink := &fakeSink{}
	if err := NewIngester(dir, newFakeStore(), sink).IngestExisting(context.Background()); err != nil {
		t.Fatalf("IngestExisting: %v", err)
	}
	recs := sink.Records()
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	want := time.Unix(1790000555, 0).UTC()
	if !recs[0].TS.Equal(want) {
		t.Errorf("TS: got %v, want %v (resolved via last_step_index)", recs[0].TS, want)
	}
}

// TestIngesterDefersUnresolvedRowInRecentlyModifiedFile covers the third
// timestamp-resolution step: a row whose message id and last_step_index both
// fail to resolve, in a file modified within the last 2 minutes (the fixture
// was just written, so this holds for free), must be deferred — not dated
// from the file's mtime — and must not advance the watermark past it. Once
// the missing step later appears, a subsequent poll picks the row up.
func TestIngesterDefersUnresolvedRowInRecentlyModifiedFile(t *testing.T) {
	dir := t.TempDir()
	convDir := filepath.Join(dir, conversationsDirName)
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const convID = "77777777-7777-7777-7777-777777777777"
	db, _ := makeConversationDB(t, convDir, convID)
	defer func() { _ = db.Close() }()

	usage1 := buildUsage(10, 5, 0, 0, "bot-1")
	insertGenMetadata(t, db, 1, buildGenMetadata(buildGenMessage("claude-sonnet-4-6", usage1)))
	insertStep(t, db, 1, buildStepMetadata(buildTimestamp(1790000000, 0), usage1))

	// The newest row has no matching step yet.
	usage2 := buildUsage(20, 8, 0, 0, "bot-unresolved")
	insertGenMetadata(t, db, 2, buildGenMetadata(buildGenMessage("claude-sonnet-4-6", usage2)))

	wm := newFakeStore()
	sink1 := &fakeSink{}
	if err := NewIngester(dir, wm, sink1).IngestExisting(context.Background()); err != nil {
		t.Fatalf("first poll: %v", err)
	}
	recs1 := sink1.Records()
	if len(recs1) != 1 {
		t.Fatalf("first poll: want 1 record (row 2 deferred), got %d", len(recs1))
	}
	if recs1[0].UUID != "agy:"+convID+":bot-1" {
		t.Errorf("UUID: got %q", recs1[0].UUID)
	}
	pos, _ := wm.LoadSourceWatermark("agy:" + convID)
	if pos != "1" {
		t.Errorf("watermark: got %q, want %q (must not pass the deferred row)", pos, "1")
	}

	// The step arrives.
	insertStep(t, db, 2, buildStepMetadata(buildTimestamp(1790000900, 0), usage2))

	sink2 := &fakeSink{}
	if err := NewIngester(dir, wm, sink2).IngestExisting(context.Background()); err != nil {
		t.Fatalf("second poll: %v", err)
	}
	recs2 := sink2.Records()
	if len(recs2) != 1 {
		t.Fatalf("second poll: want 1 record (row 2 now resolved), got %d", len(recs2))
	}
	if recs2[0].UUID != "agy:"+convID+":bot-unresolved" {
		t.Errorf("UUID: got %q", recs2[0].UUID)
	}
}

// TestIngesterUUIDFallbackWhenMessageIDEmpty covers the "agy:<id>:gen:<idx>"
// UUID shape used when a row carries no usage message id at all.
func TestIngesterUUIDFallbackWhenMessageIDEmpty(t *testing.T) {
	dir := t.TempDir()
	convDir := filepath.Join(dir, conversationsDirName)
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const convID = "99999999-9999-9999-9999-999999999999"
	db, _ := makeConversationDB(t, convDir, convID)
	defer func() { _ = db.Close() }()

	// No message id in the usage submessage; last_step_index resolves the
	// timestamp instead.
	usage := buildUsage(10, 5, 0, 0, "")
	gen := buildGenMessage("claude-sonnet-4-6", usage, buildKV(kvLastStepIndex, "3"))
	insertGenMetadata(t, db, 7, buildGenMetadata(gen))
	insertStep(t, db, 3, buildStepMetadata(buildTimestamp(1790000300, 0), nil))

	sink := &fakeSink{}
	if err := NewIngester(dir, newFakeStore(), sink).IngestExisting(context.Background()); err != nil {
		t.Fatalf("IngestExisting: %v", err)
	}
	recs := sink.Records()
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	want := "agy:" + convID + ":gen:7"
	if recs[0].UUID != want {
		t.Errorf("UUID: got %q, want %q", recs[0].UUID, want)
	}
}

func TestIngesterWorkspaceLookupFailureDoesNotBlockIngestion(t *testing.T) {
	dir := t.TempDir()
	convDir := filepath.Join(dir, conversationsDirName)
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const convID = "88888888-8888-8888-8888-888888888888"
	db, _ := makeConversationDB(t, convDir, convID)
	defer func() { _ = db.Close() }()
	usage := buildUsage(10, 5, 0, 0, "bot-1")
	insertGenMetadata(t, db, 1, buildGenMetadata(buildGenMessage("claude-sonnet-4-6", usage)))
	insertStep(t, db, 1, buildStepMetadata(buildTimestamp(1790000000, 0), usage))
	// Deliberately no conversation_summaries.db at all.

	sink := &fakeSink{}
	if err := NewIngester(dir, newFakeStore(), sink).IngestExisting(context.Background()); err != nil {
		t.Fatalf("IngestExisting: %v", err)
	}
	recs := sink.Records()
	if len(recs) != 1 {
		t.Fatalf("want 1 record despite the missing summaries db, got %d", len(recs))
	}
	if recs[0].CWD == "" {
		t.Error("CWD must never be empty (fallback must apply)")
	}
	if recs[0].CWD != "agy:"+convID {
		t.Errorf("CWD: got %q, want fallback %q", recs[0].CWD, "agy:"+convID)
	}
}

func TestIngesterMissingRootIsSilent(t *testing.T) {
	dir := t.TempDir()
	ing := NewIngester(filepath.Join(dir, "does-not-exist"), newFakeStore(), &fakeSink{})
	if err := ing.IngestExisting(context.Background()); err != nil {
		t.Errorf("IngestExisting on a missing root: %v", err)
	}
	if ing.LastErr() != nil {
		t.Errorf("LastErr on a missing root: %v", ing.LastErr())
	}
}

func TestIngesterName(t *testing.T) {
	ing := NewIngester("/some/path", newFakeStore(), &fakeSink{})
	if ing.Name() != source.Agy {
		t.Errorf("Name(): got %q, want %q", ing.Name(), source.Agy)
	}
}

func TestIngesterWatchCancels(t *testing.T) {
	ing := NewIngester(filepath.Join(t.TempDir(), "absent"), newFakeStore(), &fakeSink{})
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

func TestDefaultRoot(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	want := filepath.Join(home, ".gemini", "antigravity-cli")
	if got := DefaultRoot(); got != want {
		t.Errorf("DefaultRoot() = %q, want %q", got, want)
	}
}

func TestFirstFileURIPathFormats(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"single uri", "file:///home/user/proj", "/home/user/proj"},
		{"json array", `["file:///home/user/proj", "file:///home/user/other"]`, "/home/user/proj"},
		{"comma separated", "file:///home/user/proj, file:///home/user/other", "/home/user/proj"},
		{"newline separated", "file:///home/user/proj\nfile:///home/user/other", "/home/user/proj"},
		{"not a file uri", "not-a-uri", ""},
		{"empty", "", ""},
		{"url-encoded space", "file:///home/user/my%20project", "/home/user/my project"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstFileURIPath(tt.raw); got != tt.want {
				t.Errorf("firstFileURIPath(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

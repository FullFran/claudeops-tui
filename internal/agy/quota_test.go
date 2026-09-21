package agy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixturePayload is the documented example payload from
// https://antigravity.google/docs/cli/statusline, verbatim. Any field not
// modeled by StatuslinePayload (email, cwd, transcript_path, model,
// context_window, ...) must never reach the persisted snapshot.
const fixturePayload = `{"cwd":"/home/user/my-project","session_id":"12345678-abcd-ef01-2345-6789abcdef01","conversation_id":"12345678-abcd-ef01-2345-6789abcdef01","transcript_path":"/home/user/.gemini/antigravity/brain/12345678-abcd-ef01-2345-6789abcdef01/.system_generated/logs/transcript.jsonl","model":{"id":"Gemini 3.5 Flash (High)","display_name":"Gemini 3.5 Flash (High)"},"workspace":{"current_dir":"/home/user/my-project","project_dir":"/home/user/my-project"},"version":"1.0.13","context_window":{"total_input_tokens":88244,"total_output_tokens":61074,"context_window_size":1048576,"used_percentage":14.24,"remaining_percentage":85.76,"current_usage":{"input_tokens":63382,"output_tokens":346,"cache_creation_input_tokens":0,"cache_read_input_tokens":20857}},"exceeds_200k_tokens":false,"product":"antigravity","quota":{"gemini-weekly":{"remaining_fraction":0.9378,"reset_time":"2026-07-06T07:50:32Z","reset_in_seconds":560580}},"agent_state":"idle","vcs":{"type":"git","branch":"main","dirty":false},"sandbox":{"enabled":false},"artifact_count":2,"plan_tier":"Pro","email":"developer@email.com","task_count":1,"terminal_width":111,"execution_mode":"planning"}`

func TestParseStatuslinePayload(t *testing.T) {
	t.Run("decodes the documented fixture", func(t *testing.T) {
		p, err := ParseStatuslinePayload(strings.NewReader(fixturePayload))
		if err != nil {
			t.Fatalf("ParseStatuslinePayload: %v", err)
		}
		if p.PlanTier != "Pro" {
			t.Errorf("PlanTier = %q, want Pro", p.PlanTier)
		}
		if len(p.Quota) != 1 {
			t.Fatalf("Quota: got %d buckets, want 1", len(p.Quota))
		}
		b, ok := p.Quota["gemini-weekly"]
		if !ok {
			t.Fatal(`Quota["gemini-weekly"] missing`)
		}
		if b.RemainingFraction != 0.9378 {
			t.Errorf("RemainingFraction = %v, want 0.9378", b.RemainingFraction)
		}
		if b.ResetTime != "2026-07-06T07:50:32Z" {
			t.Errorf("ResetTime = %q, want 2026-07-06T07:50:32Z", b.ResetTime)
		}
	})

	t.Run("empty stdin errors", func(t *testing.T) {
		if _, err := ParseStatuslinePayload(strings.NewReader("")); err == nil {
			t.Error("expected an error for empty stdin")
		}
	})

	t.Run("malformed JSON errors", func(t *testing.T) {
		if _, err := ParseStatuslinePayload(strings.NewReader("{not json")); err == nil {
			t.Error("expected an error for malformed JSON")
		}
	})

	t.Run("oversized payload errors", func(t *testing.T) {
		huge := strings.NewReader(`{"plan_tier":"` + strings.Repeat("x", maxStatuslinePayloadSize+1) + `"}`)
		if _, err := ParseStatuslinePayload(huge); err == nil {
			t.Error("expected an error for a payload over the size cap")
		}
	})
}

func TestSnapshotFromPayloadDropsEverythingButAllowedFields(t *testing.T) {
	p, err := ParseStatuslinePayload(strings.NewReader(fixturePayload))
	if err != nil {
		t.Fatalf("ParseStatuslinePayload: %v", err)
	}
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	snap := SnapshotFromPayload(p, now)

	if !snap.ObservedAt.Equal(now) {
		t.Errorf("ObservedAt = %v, want %v", snap.ObservedAt, now)
	}
	if snap.PlanTier != "Pro" {
		t.Errorf("PlanTier = %q, want Pro", snap.PlanTier)
	}
	b, ok := snap.Buckets["gemini-weekly"]
	if !ok {
		t.Fatal(`Buckets["gemini-weekly"] missing`)
	}
	if b.RemainingFraction != 0.9378 {
		t.Errorf("RemainingFraction = %v, want 0.9378", b.RemainingFraction)
	}
	wantReset := time.Date(2026, 7, 6, 7, 50, 32, 0, time.UTC)
	if !b.ResetTime.Equal(wantReset) {
		t.Errorf("ResetTime = %v, want %v", b.ResetTime, wantReset)
	}

	// The full round trip through disk must never carry the fields the
	// payload has but the snapshot must not: email, cwd, transcript_path.
	out := t.TempDir()
	path := filepath.Join(out, "antigravity-quota.json")
	if err := WriteQuotaSnapshot(path, snap); err != nil {
		t.Fatalf("WriteQuotaSnapshot: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	for _, forbidden := range []string{"developer@email.com", "email", "cwd", "transcript_path", "session_id", "my-project"} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("persisted snapshot must not contain %q:\n%s", forbidden, raw)
		}
	}
}

func TestSnapshotFromPayloadNoQuota(t *testing.T) {
	p, err := ParseStatuslinePayload(strings.NewReader(`{"plan_tier":"Pro","agent_state":"idle"}`))
	if err != nil {
		t.Fatalf("ParseStatuslinePayload: %v", err)
	}
	if len(p.Quota) != 0 {
		t.Fatalf("Quota: got %d buckets, want 0", len(p.Quota))
	}
}

func TestWriteQuotaSnapshotThenReadQuotaSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "antigravity-quota.json")

	snap := QuotaSnapshot{
		ObservedAt: time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC),
		PlanTier:   "Pro",
		Buckets: map[string]QuotaBucket{
			"gemini-weekly": {
				RemainingFraction: 0.9378,
				ResetTime:         time.Date(2026, 7, 6, 7, 50, 32, 0, time.UTC),
			},
		},
	}
	if err := WriteQuotaSnapshot(path, snap); err != nil {
		t.Fatalf("WriteQuotaSnapshot: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}

	got, err := ReadQuotaSnapshot(path)
	if err != nil {
		t.Fatalf("ReadQuotaSnapshot: %v", err)
	}
	if !got.ObservedAt.Equal(snap.ObservedAt) || got.PlanTier != snap.PlanTier {
		t.Errorf("got %+v, want %+v", got, snap)
	}
	if b := got.Buckets["gemini-weekly"]; b.RemainingFraction != 0.9378 || !b.ResetTime.Equal(snap.Buckets["gemini-weekly"].ResetTime) {
		t.Errorf("bucket = %+v", b)
	}
}

func TestReadQuotaSnapshotMissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadQuotaSnapshot(filepath.Join(dir, "does-not-exist.json"))
	if err == nil {
		t.Fatal("expected an error for a missing snapshot")
	}
}

func TestReadQuotaSnapshotCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "antigravity-quota.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadQuotaSnapshot(path); err == nil {
		t.Fatal("expected an error for a corrupt snapshot file")
	}
}

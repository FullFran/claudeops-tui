package agy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// maxStatuslinePayloadSize bounds how much of agy's status-line stdin
// `claudeops agy statusline` will read. The documented payload is a few
// hundred bytes; 1 MiB is generous headroom against a malformed or hostile
// script without letting a runaway payload exhaust memory.
const maxStatuslinePayloadSize = 1 << 20 // 1 MiB

// ErrEmptyPayload and ErrPayloadTooLarge are the stdin-shape failures
// `claudeops agy statusline` treats as quiet no-ops: print nothing, change
// nothing, exit zero. agy's prompt is not a place to surface a parse error.
var (
	ErrEmptyPayload    = errors.New("agy: empty statusline payload")
	ErrPayloadTooLarge = fmt.Errorf("agy: statusline payload exceeds %d bytes", maxStatuslinePayloadSize)
)

// StatuslinePayload is the subset of agy's status-line stdin JSON this
// package understands. See
// https://antigravity.google/docs/cli/statusline for the full documented
// shape; every other field (cwd, email, session_id, transcript_path, model,
// context_window, ...) is deliberately left undecoded, so it can never reach
// the persisted snapshot even by accident — see SnapshotFromPayload.
type StatuslinePayload struct {
	PlanTier string                      `json:"plan_tier"`
	Quota    map[string]StatuslineBucket `json:"quota"`
}

// StatuslineBucket is one entry of the payload's `quota` object.
type StatuslineBucket struct {
	RemainingFraction float64 `json:"remaining_fraction"`
	ResetTime         string  `json:"reset_time"`
}

// ParseStatuslinePayload decodes agy's stdin payload, capped at
// maxStatuslinePayloadSize. Empty stdin and anything over the cap are
// reported as errors rather than decoded, same as malformed JSON.
func ParseStatuslinePayload(r io.Reader) (StatuslinePayload, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxStatuslinePayloadSize+1))
	if err != nil {
		return StatuslinePayload{}, err
	}
	if len(b) == 0 {
		return StatuslinePayload{}, ErrEmptyPayload
	}
	if len(b) > maxStatuslinePayloadSize {
		return StatuslinePayload{}, ErrPayloadTooLarge
	}
	var p StatuslinePayload
	if err := json.Unmarshal(b, &p); err != nil {
		return StatuslinePayload{}, err
	}
	return p, nil
}

// QuotaSnapshot is the on-disk representation `claudeops agy statusline`
// persists and provider.Antigravity reads back. Deliberately narrow: agy's
// stdin payload carries the user's email, cwd and transcript path among
// other things, none of which belong in a quota reading kept on disk.
type QuotaSnapshot struct {
	ObservedAt time.Time              `json:"observed_at"`
	PlanTier   string                 `json:"plan_tier,omitempty"`
	Buckets    map[string]QuotaBucket `json:"buckets,omitempty"`
}

// QuotaBucket is one bucket inside a QuotaSnapshot.
type QuotaBucket struct {
	RemainingFraction float64   `json:"remaining_fraction"`
	ResetTime         time.Time `json:"reset_time,omitzero"`
}

// SnapshotFromPayload narrows a decoded stdin payload down to the fields
// worth keeping on disk. now stamps ObservedAt — agy's payload carries no
// timestamp of its own.
func SnapshotFromPayload(p StatuslinePayload, now time.Time) QuotaSnapshot {
	snap := QuotaSnapshot{ObservedAt: now, PlanTier: p.PlanTier}
	if len(p.Quota) == 0 {
		return snap
	}
	snap.Buckets = make(map[string]QuotaBucket, len(p.Quota))
	for name, b := range p.Quota {
		qb := QuotaBucket{RemainingFraction: b.RemainingFraction}
		if b.ResetTime != "" {
			if t, err := time.Parse(time.RFC3339, b.ResetTime); err == nil {
				qb.ResetTime = t
			}
		}
		snap.Buckets[name] = qb
	}
	return snap
}

// ErrNoQuotaSnapshot reports that agy has never reported a quota reading (no
// snapshot file yet), or that the file on disk could not be read back as one.
// Either way the caller's remedy is the same: there is nothing to show.
var ErrNoQuotaSnapshot = errors.New("agy: no quota snapshot yet")

// ReadQuotaSnapshot loads the persisted snapshot. A missing file and a
// corrupt one are both reported as ErrNoQuotaSnapshot — mirroring
// statusline.ReadCache, a status line has no use for the distinction and must
// never surface a parse error where a quota belongs.
func ReadQuotaSnapshot(path string) (QuotaSnapshot, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return QuotaSnapshot{}, ErrNoQuotaSnapshot
		}
		return QuotaSnapshot{}, err
	}
	var s QuotaSnapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return QuotaSnapshot{}, ErrNoQuotaSnapshot
	}
	return s, nil
}

// WriteQuotaSnapshot atomically persists snap (temp file + rename, mode
// 0600), mirroring statusline.WriteCache: several agy invocations can race,
// and a half-written file must never be observed by a concurrent reader.
func WriteQuotaSnapshot(path string, snap QuotaSnapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".antigravity-quota-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename succeeds

	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

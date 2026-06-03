package codex

import (
	"crypto/sha1"
	"fmt"
	"strconv"
)

// SynthesizeUUID returns a deterministic, source-prefixed UUID for a Codex
// rollout line identified by (sessionUUID, lineByteOffset).
//
// Format: "codex:" + hex(sha1(sessionUUID + "@" + strconv(lineOffset))[:20])
//
// Properties:
//   - Same inputs always produce the same UUID (idempotent re-tail).
//   - Different offsets within the same session produce different UUIDs.
//   - Different sessions at the same offset produce different UUIDs.
//   - The "codex:" prefix prevents PK collisions with claude/opencode UUIDs.
func SynthesizeUUID(sessionUUID string, lineOffset int64) string {
	h := sha1.New()
	_, _ = fmt.Fprintf(h, "%s@%s", sessionUUID, strconv.FormatInt(lineOffset, 10))
	return "codex:" + fmt.Sprintf("%x", h.Sum(nil))
}

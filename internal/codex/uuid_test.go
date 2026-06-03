package codex

import (
	"strings"
	"testing"
)

// --- T2.5: Deterministic synthesized uuid ---

func TestSynthesizeUUID(t *testing.T) {
	t.Run("REQ-2.5.1: same inputs → same uuid", func(t *testing.T) {
		u1 := SynthesizeUUID("sess-abc", 42)
		u2 := SynthesizeUUID("sess-abc", 42)
		if u1 != u2 {
			t.Errorf("expected identical UUIDs, got %q and %q", u1, u2)
		}
	})

	t.Run("REQ-2.5.2: different offset → different uuid", func(t *testing.T) {
		u1 := SynthesizeUUID("sess-abc", 42)
		u2 := SynthesizeUUID("sess-abc", 99)
		if u1 == u2 {
			t.Errorf("expected different UUIDs for different offsets, got %q", u1)
		}
	})

	t.Run("REQ-2.5.3: source prefix 'codex:' present", func(t *testing.T) {
		u := SynthesizeUUID("sess-abc", 0)
		if !strings.HasPrefix(u, "codex:") {
			t.Errorf("expected 'codex:' prefix, got %q", u)
		}
	})

	t.Run("REQ-2.5.4: different session → different uuid at same offset", func(t *testing.T) {
		u1 := SynthesizeUUID("sess-aaa", 42)
		u2 := SynthesizeUUID("sess-bbb", 42)
		if u1 == u2 {
			t.Errorf("different sessions must produce different UUIDs, got %q", u1)
		}
	})
}

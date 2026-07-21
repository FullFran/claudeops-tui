package codex

import (
	"strings"
	"testing"
)

// TestWarnOnUnknownLineType checks that schema drift is reported once per type
// instead of being swallowed.
func TestWarnOnUnknownLineType(t *testing.T) {
	p := NewParser()
	var warnings []string
	p.OnWarn = func(msg string) { warnings = append(warnings, msg) }

	lines := [][]byte{
		[]byte(`{"timestamp":"2026-01-15T12:00:00Z","type":"compacted","payload":{}}`),
		[]byte(`{"timestamp":"2026-01-15T12:00:01Z","type":"compacted","payload":{}}`),
		[]byte(`{"timestamp":"2026-01-15T12:00:02Z","type":"event_msg","payload":{}}`),
	}
	for _, line := range lines {
		if _, err := p.ParseLine(line, makeCtx("sess-warn")); err != nil {
			t.Fatalf("ParseLine: %v", err)
		}
	}

	if len(warnings) != 2 {
		t.Fatalf("warnings: want 2 (one per unknown type), got %d: %v", len(warnings), warnings)
	}
	for _, want := range []string{"compacted", "event_msg"} {
		found := false
		for _, w := range warnings {
			if strings.Contains(w, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("no warning mentions %q: %v", want, warnings)
		}
	}
	if got := p.UnknownTypes(); len(got) != 2 {
		t.Errorf("UnknownTypes: want 2 got %v", got)
	}
}

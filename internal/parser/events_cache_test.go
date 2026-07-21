package parser

import "testing"

func TestParseLineDecodesCacheCreationBreakdown(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		cacheCreate int64
		create1h    int64
	}{
		{
			name:        "no breakdown keeps the combined total",
			line:        `{"type":"assistant","uuid":"u1","sessionId":"s1","timestamp":"2026-07-21T10:00:00Z","message":{"id":"msg_1","model":"claude-fable-5","usage":{"input_tokens":5,"output_tokens":7,"cache_creation_input_tokens":20780,"cache_read_input_tokens":11}}}`,
			cacheCreate: 20780,
			create1h:    0,
		},
		{
			name:        "breakdown splits the total",
			line:        `{"type":"assistant","uuid":"u2","sessionId":"s1","timestamp":"2026-07-21T10:00:00Z","message":{"id":"msg_2","model":"claude-fable-5","usage":{"input_tokens":5,"output_tokens":7,"cache_creation_input_tokens":30000,"cache_read_input_tokens":11,"cache_creation":{"ephemeral_5m_input_tokens":1417,"ephemeral_1h_input_tokens":28583}}}}`,
			cacheCreate: 30000,
			create1h:    28583,
		},
		{
			name:        "combined total absent falls back to the breakdown sum",
			line:        `{"type":"assistant","uuid":"u3","sessionId":"s1","timestamp":"2026-07-21T10:00:00Z","message":{"id":"msg_3","model":"claude-fable-5","usage":{"input_tokens":5,"output_tokens":7,"cache_creation":{"ephemeral_5m_input_tokens":100,"ephemeral_1h_input_tokens":900}}}}`,
			cacheCreate: 1000,
			create1h:    900,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := ParseLine([]byte(tt.line))
			if err != nil {
				t.Fatal(err)
			}
			a, ok := ev.(AssistantEvent)
			if !ok {
				t.Fatalf("got %T, want AssistantEvent", ev)
			}
			if a.CacheCreateTokens != tt.cacheCreate {
				t.Errorf("CacheCreateTokens = %d, want %d", a.CacheCreateTokens, tt.cacheCreate)
			}
			if a.CacheCreate1hTokens != tt.create1h {
				t.Errorf("CacheCreate1hTokens = %d, want %d", a.CacheCreate1hTokens, tt.create1h)
			}
		})
	}
}

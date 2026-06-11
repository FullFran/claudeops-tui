package parser

import (
	"testing"
)

func TestParseAssistant(t *testing.T) {
	line := []byte(`{
	  "type":"assistant","uuid":"u1","sessionId":"s1","cwd":"/p","version":"2.1.96",
	  "timestamp":"2026-04-08T13:10:57.123Z",
	  "message":{"role":"assistant","model":"claude-opus-4-6",
	    "usage":{"input_tokens":5,"cache_creation_input_tokens":20780,"cache_read_input_tokens":15718,"output_tokens":1101}}
	}`)
	ev, err := ParseLine(line)
	if err != nil {
		t.Fatal(err)
	}
	a, ok := ev.(AssistantEvent)
	if !ok {
		t.Fatalf("want AssistantEvent got %T", ev)
	}
	if a.Model != "claude-opus-4-6" || a.InTokens != 5 || a.OutTokens != 1101 ||
		a.CacheReadTokens != 15718 || a.CacheCreateTokens != 20780 {
		t.Errorf("usage decoded wrong: %+v", a)
	}
	if a.Session != "s1" || a.UUID != "u1" || a.CWD != "/p" {
		t.Errorf("common fields wrong: %+v", a.Common)
	}
}

func TestParseAssistantDecodesMessageAndRequestIDs(t *testing.T) {
	line := []byte(`{
	  "type":"assistant","uuid":"u1","sessionId":"s1","cwd":"/p","requestId":"req_1",
	  "timestamp":"2026-06-11T10:00:00Z",
	  "message":{"id":"msg_1","role":"assistant","model":"claude-fable-5",
	    "usage":{"input_tokens":1,"output_tokens":2}}
	}`)
	ev, err := ParseLine(line)
	if err != nil {
		t.Fatal(err)
	}
	a, ok := ev.(AssistantEvent)
	if !ok {
		t.Fatalf("want AssistantEvent got %T", ev)
	}
	if a.MessageID != "msg_1" {
		t.Errorf("MessageID: got %q want %q", a.MessageID, "msg_1")
	}
	if a.RequestID != "req_1" {
		t.Errorf("RequestID: got %q want %q", a.RequestID, "req_1")
	}
}

func TestAssistantDedupUUID(t *testing.T) {
	cases := []struct {
		name  string
		msgID string
		reqID string
		uuid  string
		want  string
	}{
		{"message and request ids combine", "msg_1", "req_1", "u1", "msg_1:req_1"},
		{"message id alone is sufficient", "msg_1", "", "u1", "msg_1"},
		{"no ids falls back to line uuid", "", "", "u1", "u1"},
		{"request id alone is not enough", "", "req_1", "u1", "u1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := AssistantEvent{MessageID: tc.msgID, RequestID: tc.reqID}
			a.UUID = tc.uuid
			if got := a.DedupUUID(); got != tc.want {
				t.Errorf("DedupUUID() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseUser(t *testing.T) {
	ev, err := ParseLine([]byte(`{"type":"user","uuid":"u","sessionId":"s","cwd":"/p","timestamp":"2026-04-08T13:10:57Z"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ev.(UserEvent); !ok {
		t.Errorf("want UserEvent got %T", ev)
	}
}

func TestParseUnknownTypeIsNotError(t *testing.T) {
	ev, err := ParseLine([]byte(`{"type":"permission-mode","uuid":"u","sessionId":"s","cwd":"/p","timestamp":"2026-04-08T13:10:57Z"}`))
	if err != nil {
		t.Fatal(err)
	}
	u, ok := ev.(UnknownEvent)
	if !ok {
		t.Fatalf("want UnknownEvent got %T", ev)
	}
	if u.Type != "permission-mode" {
		t.Errorf("type wrong: %q", u.Type)
	}
}

func TestParseMalformedJSON(t *testing.T) {
	if _, err := ParseLine([]byte(`{not json`)); err == nil {
		t.Errorf("expected error")
	}
	if _, err := ParseLine([]byte(``)); err == nil {
		t.Errorf("expected error on empty")
	}
	if _, err := ParseLine([]byte(`{"uuid":"x"}`)); err == nil {
		t.Errorf("expected error on missing type")
	}
}

func TestVersionInRange(t *testing.T) {
	cases := map[string]bool{
		"":       true,
		"2.1.96": true,
		"2.1.0":  true,
		"2.0.5":  false,
		"2.2.0":  false,
		"2.3.1":  false,
	}
	for v, want := range cases {
		if got := VersionInRange(v); got != want {
			t.Errorf("VersionInRange(%q): got %v want %v", v, got, want)
		}
	}
}

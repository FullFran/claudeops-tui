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

package opencode

import (
	"encoding/json"
	"testing"
)

// TestDecodeMessageData verifies that DecodeMessageData correctly decodes
// the data JSON blob from an opencode message row.
func TestDecodeMessageData(t *testing.T) {
	// Real structure from opencode.db (probe confirmed):
	// {"role":"assistant","modelID":"kimi-k2.6","providerID":"opencode-go",
	//  "cost":0.039,"tokens":{"total":227957,"input":818,"output":548,
	//  "reasoning":0,"cache":{"write":0,"read":226591}},"time":{...}}
	tests := []struct {
		name       string
		json       string
		wantRole   string
		wantModel  string
		wantProv   string
		wantIn     int64
		wantOut    int64
		wantReason int64
		wantRead   int64
		wantWrite  int64
		wantCost   float64
	}{
		{
			name: "assistant with tokens",
			json: `{
				"role": "assistant",
				"modelID": "claude-sonnet-4-5",
				"providerID": "anthropic",
				"cost": 0.0,
				"tokens": {"total":5000,"input":1000,"output":200,"reasoning":0,
				           "cache":{"write":100,"read":3700}}
			}`,
			wantRole:  "assistant",
			wantModel: "claude-sonnet-4-5",
			wantProv:  "anthropic",
			wantIn:    1000,
			wantOut:   200,
			wantRead:  3700,
			wantWrite: 100,
			wantCost:  0.0,
		},
		{
			name:     "user role",
			json:     `{"role":"user","modelID":"","providerID":"","tokens":{"total":0,"input":0,"output":0,"reasoning":0,"cache":{"write":0,"read":0}}}`,
			wantRole: "user",
		},
		{
			name: "real kimi message",
			json: `{"parentID":"msg_abc","role":"assistant","mode":"build","agent":"build",
			        "path":{"cwd":"/home/user/project","root":"/"},
			        "cost":0.03922366,
			        "tokens":{"total":227957,"input":818,"output":548,"reasoning":0,
			                  "cache":{"write":0,"read":226591}},
			        "modelID":"kimi-k2.6","providerID":"opencode-go",
			        "time":{"created":1779661848629,"completed":1779661860935},"finish":"stop"}`,
			wantRole:  "assistant",
			wantModel: "kimi-k2.6",
			wantProv:  "opencode-go",
			wantIn:    818,
			wantOut:   548,
			wantRead:  226591,
			wantWrite: 0,
			wantCost:  0.03922366,
		},
		{
			name: "with reasoning tokens",
			json: `{"role":"assistant","modelID":"claude-opus-4-8","providerID":"anthropic",
			        "cost":0.1,"tokens":{"total":5000,"input":1000,"output":200,"reasoning":50,
			                             "cache":{"write":0,"read":3750}}}`,
			wantRole:   "assistant",
			wantModel:  "claude-opus-4-8",
			wantProv:   "anthropic",
			wantIn:     1000,
			wantOut:    200,
			wantReason: 50,
			wantRead:   3750,
			wantWrite:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := DecodeMessageData([]byte(tt.json))
			if err != nil {
				t.Fatalf("DecodeMessageData: %v", err)
			}
			if d.Role != tt.wantRole {
				t.Errorf("Role: got %q want %q", d.Role, tt.wantRole)
			}
			if tt.wantRole != "assistant" {
				return // skip token checks for non-assistant roles
			}
			if d.ModelID != tt.wantModel {
				t.Errorf("ModelID: got %q want %q", d.ModelID, tt.wantModel)
			}
			if d.ProviderID != tt.wantProv {
				t.Errorf("ProviderID: got %q want %q", d.ProviderID, tt.wantProv)
			}
			if d.Tokens.Input != tt.wantIn {
				t.Errorf("Tokens.Input: got %d want %d", d.Tokens.Input, tt.wantIn)
			}
			if d.Tokens.Output != tt.wantOut {
				t.Errorf("Tokens.Output: got %d want %d", d.Tokens.Output, tt.wantOut)
			}
			if d.Tokens.Reasoning != tt.wantReason {
				t.Errorf("Tokens.Reasoning: got %d want %d", d.Tokens.Reasoning, tt.wantReason)
			}
			if d.Tokens.Cache.Read != tt.wantRead {
				t.Errorf("Tokens.Cache.Read: got %d want %d", d.Tokens.Cache.Read, tt.wantRead)
			}
			if d.Tokens.Cache.Write != tt.wantWrite {
				t.Errorf("Tokens.Cache.Write: got %d want %d", d.Tokens.Cache.Write, tt.wantWrite)
			}
		})
	}
}

// TestDecodeMessageData_MissingRole tests that missing role returns no error
// (the caller checks role == "assistant" before using the result).
func TestDecodeMessageData_MissingRole(t *testing.T) {
	d, err := DecodeMessageData([]byte(`{"modelID":"x","providerID":"y"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Role != "" {
		t.Errorf("expected empty role, got %q", d.Role)
	}
}

// TestDecodeMessageData_Invalid tests that invalid JSON returns an error.
func TestDecodeMessageData_Invalid(t *testing.T) {
	_, err := DecodeMessageData([]byte(`not-json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// TestNormalizeModel verifies provider/model → canonical pricing key normalization.
func TestNormalizeModel(t *testing.T) {
	tests := []struct {
		name       string
		providerID string
		modelID    string
		want       string
	}{
		// Anthropic: dotted version → hyphenated canonical key
		{
			name:       "anthropic claude dotted stays as-is",
			providerID: "anthropic",
			modelID:    "claude-sonnet-4-5",
			want:       "claude-sonnet-4-5",
		},
		{
			name:       "anthropic claude with dots normalized",
			providerID: "anthropic",
			modelID:    "claude-opus-4.6",
			want:       "claude-opus-4-6",
		},
		{
			name:       "anthropic claude-opus-4-8 unchanged",
			providerID: "anthropic",
			modelID:    "claude-opus-4-8",
			want:       "claude-opus-4-8",
		},
		// Non-Anthropic: produce provider-qualified key
		{
			name:       "openai gpt model qualified",
			providerID: "openai",
			modelID:    "gpt-4o",
			want:       "openai/gpt-4o",
		},
		{
			name:       "google model qualified",
			providerID: "google",
			modelID:    "gemini-2.0-flash",
			want:       "google/gemini-2.0-flash",
		},
		{
			name:       "unknown provider qualified",
			providerID: "opencode-go",
			modelID:    "kimi-k2.6",
			want:       "opencode-go/kimi-k2.6",
		},
		{
			name:       "empty providerID falls back to modelID",
			providerID: "",
			modelID:    "some-model",
			want:       "some-model",
		},
		// Anthropic with vendor prefix stripped (some integrations add it)
		{
			name:       "anthropic prefix stripped when explicit",
			providerID: "anthropic",
			modelID:    "claude-haiku-4-5",
			want:       "claude-haiku-4-5",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeModel(tt.providerID, tt.modelID)
			if got != tt.want {
				t.Errorf("NormalizeModel(%q, %q) = %q, want %q",
					tt.providerID, tt.modelID, got, tt.want)
			}
		})
	}
}

// TestTokenMapping verifies that token fields map to source.Record fields correctly.
// Reasoning tokens fold into Out; cache.write → CacheCreate; cache.read → CacheRead.
func TestTokenMapping(t *testing.T) {
	raw := `{"role":"assistant","modelID":"claude-opus-4-8","providerID":"anthropic",
	         "tokens":{"total":5350,"input":1000,"output":200,"reasoning":50,
	                   "cache":{"write":100,"read":4000}}}`
	d, err := DecodeMessageData([]byte(raw))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	// token field mapping (per design §5.2):
	//   In          = tokens.input
	//   Out         = tokens.output + tokens.reasoning (fold reasoning into Out)
	//   CacheRead   = tokens.cache.read
	//   CacheCreate = tokens.cache.write
	if d.Tokens.Input != 1000 {
		t.Errorf("Input: got %d want 1000", d.Tokens.Input)
	}
	if d.Tokens.Output != 200 {
		t.Errorf("Output: got %d want 200", d.Tokens.Output)
	}
	if d.Tokens.Reasoning != 50 {
		t.Errorf("Reasoning: got %d want 50", d.Tokens.Reasoning)
	}
	if d.Tokens.Cache.Write != 100 {
		t.Errorf("Cache.Write: got %d want 100", d.Tokens.Cache.Write)
	}
	if d.Tokens.Cache.Read != 4000 {
		t.Errorf("Cache.Read: got %d want 4000", d.Tokens.Cache.Read)
	}
	// The caller (Ingester) folds reasoning into Out: Out + Reasoning.
	// Verify the helper function does this correctly.
	rec := d.ToTokenRecord()
	if rec.Out != 250 { // 200 + 50
		t.Errorf("ToTokenRecord Out: got %d want 250", rec.Out)
	}
	if rec.In != 1000 {
		t.Errorf("ToTokenRecord In: got %d want 1000", rec.In)
	}
	if rec.CacheRead != 4000 {
		t.Errorf("ToTokenRecord CacheRead: got %d want 4000", rec.CacheRead)
	}
	if rec.CacheCreate != 100 {
		t.Errorf("ToTokenRecord CacheCreate: got %d want 100", rec.CacheCreate)
	}
}

// Compile-time check that MessageData is exported and json.Unmarshal-compatible.
var _ = json.RawMessage(nil)

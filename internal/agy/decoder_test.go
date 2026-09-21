package agy

import (
	"testing"
	"time"
)

// --- fixture builders --------------------------------------------------
//
// Every field number here matches the constants in decoder.go, which are in
// turn pinned to the verified agy 1.2.7 schema in odd/tasks/agy-support.md.
// None of this is copied from real user data (see wire_test.go's note on the
// privacy constraint).

func buildUsage(in, outTotal, cacheWrite, cacheRead int64, messageID string) []byte {
	var buf []byte
	if in != 0 {
		buf = encodeVarintField(buf, fieldUsageInput, uint64(in))
	}
	if outTotal != 0 {
		buf = encodeVarintField(buf, fieldUsageOutputTotal, uint64(outTotal))
	}
	if cacheWrite != 0 {
		buf = encodeVarintField(buf, fieldUsageCacheWrite, uint64(cacheWrite))
	}
	if cacheRead != 0 {
		buf = encodeVarintField(buf, fieldUsageCacheRead, uint64(cacheRead))
	}
	if messageID != "" {
		buf = encodeBytesField(buf, fieldUsageMessageID, []byte(messageID))
	}
	return buf
}

func buildKV(key, value string) []byte {
	var buf []byte
	buf = encodeBytesField(buf, fieldKVKey, []byte(key))
	buf = encodeBytesField(buf, fieldKVValue, []byte(value))
	return buf
}

func buildGenMessage(model string, usage []byte, kvs ...[]byte) []byte {
	var buf []byte
	if model != "" {
		buf = encodeBytesField(buf, fieldGenModelID, []byte(model))
	}
	if usage != nil {
		buf = encodeBytesField(buf, fieldGenUsage, usage)
	}
	for _, kv := range kvs {
		buf = encodeBytesField(buf, fieldGenKV, kv)
	}
	return buf
}

func buildGenMetadata(genMessage []byte) []byte {
	return encodeBytesField(nil, fieldGenMessage, genMessage)
}

func buildTimestamp(sec, nanos int64) []byte {
	var buf []byte
	buf = encodeVarintField(buf, fieldTimestampSeconds, uint64(sec))
	buf = encodeVarintField(buf, fieldTimestampNanos, uint64(nanos))
	return buf
}

func buildStepMetadata(created, usage []byte) []byte {
	var buf []byte
	if created != nil {
		buf = encodeBytesField(buf, fieldStepCreated, created)
	}
	if usage != nil {
		buf = encodeBytesField(buf, fieldStepUsage, usage)
	}
	return buf
}

// --- DecodeGenMetadata --------------------------------------------------

func TestDecodeGenMetadataFullRow(t *testing.T) {
	usage := buildUsage(15056, 111, 0, 0, "bot-97dbfe79-c8d4-4141-9346-190e590181e6")
	gen := buildGenMessage("gemini-3.8-flash-exp-a", usage, buildKV(kvLastStepIndex, "42"))
	blob := buildGenMetadata(gen)

	c, ok, err := DecodeGenMetadata(blob)
	if err != nil {
		t.Fatalf("DecodeGenMetadata: %v", err)
	}
	if !ok {
		t.Fatal("DecodeGenMetadata: ok=false, want true")
	}
	want := Call{
		Model:         "gemini-3.8-flash-exp-a",
		MessageID:     "bot-97dbfe79-c8d4-4141-9346-190e590181e6",
		In:            15056,
		Out:           111,
		LastStepIndex: "42",
	}
	if c != want {
		t.Errorf("DecodeGenMetadata: got %+v, want %+v", c, want)
	}
}

func TestDecodeGenMetadataCacheRead(t *testing.T) {
	usage := buildUsage(5600, 1204, 0, 12203, "")
	gen := buildGenMessage("claude-sonnet-4-6", usage)
	blob := buildGenMetadata(gen)

	c, ok, err := DecodeGenMetadata(blob)
	if err != nil {
		t.Fatalf("DecodeGenMetadata: %v", err)
	}
	if !ok {
		t.Fatal("DecodeGenMetadata: ok=false, want true")
	}
	if c.In != 5600 || c.Out != 1204 || c.CacheRead != 12203 {
		t.Errorf("DecodeGenMetadata: got In=%d Out=%d CacheRead=%d, want 5600/1204/12203", c.In, c.Out, c.CacheRead)
	}
	if c.MessageID != "" {
		t.Errorf("MessageID: got %q, want empty", c.MessageID)
	}
}

func TestDecodeGenMetadataCacheWrite(t *testing.T) {
	usage := buildUsage(100, 20, 30, 0, "bot-cache-write")
	gen := buildGenMessage("gpt-oss-120b", usage)
	blob := buildGenMetadata(gen)

	c, ok, err := DecodeGenMetadata(blob)
	if err != nil {
		t.Fatalf("DecodeGenMetadata: %v", err)
	}
	if !ok {
		t.Fatal("DecodeGenMetadata: ok=false, want true")
	}
	if c.CacheCreate != 30 {
		t.Errorf("CacheCreate: got %d, want 30", c.CacheCreate)
	}
}

func TestDecodeGenMetadataNoUsageMessage(t *testing.T) {
	gen := buildGenMessage("gemini-3.8-flash", nil) // no usage submessage
	blob := buildGenMetadata(gen)

	_, ok, err := DecodeGenMetadata(blob)
	if err != nil {
		t.Fatalf("DecodeGenMetadata: %v", err)
	}
	if ok {
		t.Error("DecodeGenMetadata: ok=true for a row with no usage message, want false")
	}
}

func TestDecodeGenMetadataNoGenMessage(t *testing.T) {
	// Top-level blob with an unrelated field only — no field 1 at all.
	blob := encodeVarintField(nil, 2, 7)

	_, ok, err := DecodeGenMetadata(blob)
	if err != nil {
		t.Fatalf("DecodeGenMetadata: %v", err)
	}
	if ok {
		t.Error("DecodeGenMetadata: ok=true for a blob with no generation message, want false")
	}
}

func TestDecodeGenMetadataMalformed(t *testing.T) {
	if _, _, err := DecodeGenMetadata([]byte{0xFF, 0xFF, 0xFF}); err == nil {
		t.Fatal("DecodeGenMetadata: want error for malformed bytes, got nil")
	}
}

func TestDecodeGenMetadataIgnoresUnknownKVKeys(t *testing.T) {
	usage := buildUsage(10, 5, 0, 0, "bot-x")
	gen := buildGenMessage("claude-sonnet-4-6", usage,
		buildKV("trajectory_id", "traj-1"),
		buildKV("request_id", "req-1"),
		buildKV(kvLastStepIndex, "7"),
		buildKV("used_claude", "true"),
	)
	blob := buildGenMetadata(gen)

	c, ok, err := DecodeGenMetadata(blob)
	if err != nil {
		t.Fatalf("DecodeGenMetadata: %v", err)
	}
	if !ok {
		t.Fatal("ok=false, want true")
	}
	if c.LastStepIndex != "7" {
		t.Errorf("LastStepIndex: got %q, want %q", c.LastStepIndex, "7")
	}
}

// --- DecodeStepMetadata --------------------------------------------------

func TestDecodeStepMetadataWithUsage(t *testing.T) {
	created := buildTimestamp(1790000000, 500)
	usage := buildUsage(1, 1, 0, 0, "bot-join-key")
	blob := buildStepMetadata(created, usage)

	msgID, ts, ok := DecodeStepMetadata(blob)
	if !ok {
		t.Fatal("DecodeStepMetadata: ok=false, want true")
	}
	if msgID != "bot-join-key" {
		t.Errorf("msgID: got %q, want %q", msgID, "bot-join-key")
	}
	want := time.Unix(1790000000, 500).UTC()
	if !ts.Equal(want) {
		t.Errorf("created: got %v, want %v", ts, want)
	}
}

func TestDecodeStepMetadataCreatedOnly(t *testing.T) {
	created := buildTimestamp(1790000100, 0)
	blob := buildStepMetadata(created, nil) // no usage submessage, as most steps have

	msgID, ts, ok := DecodeStepMetadata(blob)
	if !ok {
		t.Fatal("DecodeStepMetadata: ok=false, want true")
	}
	if msgID != "" {
		t.Errorf("msgID: got %q, want empty", msgID)
	}
	if ts.IsZero() {
		t.Error("created: got zero time, want the encoded timestamp")
	}
}

func TestDecodeStepMetadataEmpty(t *testing.T) {
	msgID, ts, ok := DecodeStepMetadata(nil)
	if !ok {
		t.Fatal("DecodeStepMetadata(nil): ok=false, want true (an empty message is valid protobuf)")
	}
	if msgID != "" || !ts.IsZero() {
		t.Errorf("DecodeStepMetadata(nil): got msgID=%q ts=%v, want empty/zero", msgID, ts)
	}
}

func TestDecodeStepMetadataMalformed(t *testing.T) {
	if _, _, ok := DecodeStepMetadata([]byte{0xFF, 0xFF, 0xFF}); ok {
		t.Error("DecodeStepMetadata: ok=true for malformed bytes, want false")
	}
}

// --- NormalizeModel --------------------------------------------------

func TestNormalizeModel(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"exp tag with alnum suffix", "gemini-3.8-flash-exp-a", "gemini-3.8-flash"},
		{"reasoning effort high", "gemini-3.8-flash-high", "gemini-3.8-flash"},
		{"reasoning effort medium", "gpt-oss-120b-medium", "gpt-oss-120b"},
		{"unchanged model", "claude-sonnet-4-6", "claude-sonnet-4-6"},
		{"empty input", "", ""},
		{"bare exp suffix", "gemini-3.1-pro-exp", "gemini-3.1-pro"},
		{"reasoning effort low", "gemini-3.1-pro-low", "gemini-3.1-pro"},
		{"reasoning effort minimal", "gemini-3.1-pro-minimal", "gemini-3.1-pro"},
		{"thinking suffix passes through (pricing handles it)", "claude-opus-4-6-thinking", "claude-opus-4-6-thinking"},
		{"nested effort inside exp tag reaches fixed point", "gemini-3.8-flash-exp-a-high", "gemini-3.8-flash"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeModel(tt.in); got != tt.want {
				t.Errorf("NormalizeModel(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDecodeStepMetadataEmptyTimestamp(t *testing.T) {
	created := buildTimestamp(0, 0)
	blob := buildStepMetadata(created, nil)

	msgID, ts, ok := DecodeStepMetadata(blob)
	if !ok {
		t.Fatal("DecodeStepMetadata: ok=false, want true")
	}
	if msgID != "" {
		t.Errorf("msgID: got %q, want empty", msgID)
	}
	if !ts.IsZero() {
		t.Errorf("created: got %v, want zero time", ts)
	}
}

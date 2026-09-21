package agy

import (
	"encoding/binary"
	"testing"
)

// --- test-only protobuf encoder -------------------------------------------
//
// agy has no public .proto schema, and reading the user's real conversation
// data is off-limits for this package's tests (see the privacy constraint in
// odd/tasks/agy-support.md). Every fixture blob used by this package's tests
// is built by this hand-written encoder from the field layout verified
// against a real agy 1.2.7 install — never copied from actual user data.

func appendVarint(buf []byte, v uint64) []byte {
	var tmp [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(tmp[:], v)
	return append(buf, tmp[:n]...)
}

func appendTag(buf []byte, num int, typ wireType) []byte {
	return appendVarint(buf, uint64(num)<<3|uint64(typ))
}

func encodeVarintField(buf []byte, num int, v uint64) []byte {
	buf = appendTag(buf, num, wireVarint)
	return appendVarint(buf, v)
}

func encodeBytesField(buf []byte, num int, b []byte) []byte {
	buf = appendTag(buf, num, wireBytes)
	buf = appendVarint(buf, uint64(len(b)))
	return append(buf, b...)
}

func encodeFixed32Field(buf []byte, num int, b [4]byte) []byte {
	buf = appendTag(buf, num, wireFixed32)
	return append(buf, b[:]...)
}

func encodeFixed64Field(buf []byte, num int, b [8]byte) []byte {
	buf = appendTag(buf, num, wireFixed64)
	return append(buf, b[:]...)
}

// --- tests ------------------------------------------------------------------

func TestParseFieldsWireTypes(t *testing.T) {
	var buf []byte
	buf = encodeVarintField(buf, 1, 150)
	buf = encodeBytesField(buf, 2, []byte("hello"))
	buf = encodeFixed64Field(buf, 3, [8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	buf = encodeFixed32Field(buf, 4, [4]byte{9, 8, 7, 6})

	fields, err := parseFields(buf)
	if err != nil {
		t.Fatalf("parseFields: %v", err)
	}
	if len(fields) != 4 {
		t.Fatalf("len(fields) = %d, want 4", len(fields))
	}

	if v, ok := firstVarint(fields, 1); !ok || v != 150 {
		t.Errorf("field 1 varint: got (%d, %v), want (150, true)", v, ok)
	}
	if b, ok := firstBytes(fields, 2); !ok || string(b) != "hello" {
		t.Errorf("field 2 bytes: got (%q, %v), want (\"hello\", true)", b, ok)
	}
	if fields[2].number != 3 || fields[2].typ != wireFixed64 || len(fields[2].raw) != 8 {
		t.Errorf("field 3 fixed64: got %+v", fields[2])
	}
	if fields[3].number != 4 || fields[3].typ != wireFixed32 || len(fields[3].raw) != 4 {
		t.Errorf("field 4 fixed32: got %+v", fields[3])
	}
}

func TestParseFieldsRejectsFieldNumberZero(t *testing.T) {
	// Tag with field number 0, wire type varint: 0<<3|0 = 0.
	buf := appendVarint(nil, 0)
	buf = appendVarint(buf, 1)
	if _, err := parseFields(buf); err == nil {
		t.Fatal("parseFields: want error for field number 0, got nil")
	}
}

func TestParseFieldsRejectsTruncation(t *testing.T) {
	tests := []struct {
		name string
		buf  []byte
	}{
		{"truncated tag", []byte{0xFF}},
		{"truncated varint value", encodeVarintField(nil, 1, 300)[:1]},
		{"truncated length-delimited length", func() []byte {
			b := appendTag(nil, 1, wireBytes)
			return b // no length varint at all
		}()},
		{"truncated length-delimited payload", func() []byte {
			b := appendTag(nil, 1, wireBytes)
			b = appendVarint(b, 10) // claims 10 bytes
			return append(b, []byte("short")...)
		}()},
		{"truncated fixed64", func() []byte {
			b := appendTag(nil, 1, wireFixed64)
			return append(b, 1, 2, 3)
		}()},
		{"truncated fixed32", func() []byte {
			b := appendTag(nil, 1, wireFixed32)
			return append(b, 1, 2)
		}()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseFields(tt.buf); err == nil {
				t.Fatal("parseFields: want error, got nil")
			}
		})
	}
}

func TestParseFieldsRejectsUnknownWireType(t *testing.T) {
	// Wire type 3 (start group) — deprecated, never emitted by agy.
	buf := appendVarint(nil, uint64(1)<<3|3)
	if _, err := parseFields(buf); err == nil {
		t.Fatal("parseFields: want error for wire type 3, got nil")
	}
}

func TestAllBytesRepeatedField(t *testing.T) {
	var buf []byte
	buf = encodeBytesField(buf, 20, []byte("a"))
	buf = encodeBytesField(buf, 20, []byte("b"))
	buf = encodeBytesField(buf, 20, []byte("c"))

	fields, err := parseFields(buf)
	if err != nil {
		t.Fatalf("parseFields: %v", err)
	}
	got := allBytes(fields, 20)
	if len(got) != 3 {
		t.Fatalf("allBytes: got %d entries, want 3", len(got))
	}
	for i, want := range []string{"a", "b", "c"} {
		if string(got[i]) != want {
			t.Errorf("allBytes[%d] = %q, want %q", i, got[i], want)
		}
	}
}

func TestFirstVarintAbsent(t *testing.T) {
	fields, err := parseFields(encodeVarintField(nil, 1, 5))
	if err != nil {
		t.Fatalf("parseFields: %v", err)
	}
	if _, ok := firstVarint(fields, 99); ok {
		t.Error("firstVarint: want ok=false for an absent field")
	}
}

func TestFirstBytesAbsent(t *testing.T) {
	fields, err := parseFields(encodeBytesField(nil, 1, []byte("x")))
	if err != nil {
		t.Fatalf("parseFields: %v", err)
	}
	if _, ok := firstBytes(fields, 99); ok {
		t.Error("firstBytes: want ok=false for an absent field")
	}
}

func TestParseFieldsEmptyInput(t *testing.T) {
	fields, err := parseFields(nil)
	if err != nil {
		t.Fatalf("parseFields(nil): %v", err)
	}
	if len(fields) != 0 {
		t.Errorf("parseFields(nil): got %d fields, want 0", len(fields))
	}
}

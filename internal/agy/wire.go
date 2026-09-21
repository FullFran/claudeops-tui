// Package agy implements source.Ingester for Google Antigravity CLI's local
// data. Unlike opencode's single shared database, agy writes one SQLite file
// per conversation under ~/.gemini/antigravity-cli/conversations, each
// carrying a protobuf-encoded blob per model call in its gen_metadata table.
// There is no public .proto schema and adding a codegen dependency would
// change go.mod, so this package parses the wire format by hand (wire.go)
// against the field layout verified from a real agy 1.2.7 install (decoder.go).
package agy

import (
	"encoding/binary"
	"fmt"
)

// wireType is protobuf's 3-bit wire type tag (the low 3 bits of a field tag).
type wireType uint64

const (
	wireVarint  wireType = 0
	wireFixed64 wireType = 1
	wireBytes   wireType = 2
	wireFixed32 wireType = 5
)

// field is one decoded top-level field of a protobuf message, kept generic
// (number + wire type + raw payload) because this package has no .proto
// schema to generate strongly-typed accessors from. Callers look fields up by
// number using firstVarint/firstBytes/allBytes below.
type field struct {
	number int
	typ    wireType
	varint uint64
	raw    []byte // populated for wireBytes, wireFixed64 (8 bytes), wireFixed32 (4 bytes)
}

// parseFields decodes a flat sequence of protobuf fields from data. It makes
// no assumption about which fields are expected — the caller matches by field
// number — which is what lets one parser serve every message shape in the agy
// schema (the generation message, its usage submessage, kv pairs, step
// metadata, and the Timestamp submessage).
//
// It rejects field number 0 (never valid in protobuf), truncated input, and
// wire types other than varint/fixed64/bytes/fixed32. Groups (wire types 3
// and 4) are long-deprecated and never appear in agy's schema, so treating
// them as an error surfaces schema drift instead of silently dropping fields.
func parseFields(data []byte) ([]field, error) {
	var out []field
	i := 0
	for i < len(data) {
		tag, n := binary.Uvarint(data[i:])
		if n <= 0 {
			return nil, fmt.Errorf("agy: truncated field tag at offset %d", i)
		}
		i += n

		num := int(tag >> 3)
		typ := wireType(tag & 0x7)
		if num == 0 {
			return nil, fmt.Errorf("agy: invalid field number 0 at offset %d", i)
		}

		switch typ {
		case wireVarint:
			v, n := binary.Uvarint(data[i:])
			if n <= 0 {
				return nil, fmt.Errorf("agy: truncated varint for field %d at offset %d", num, i)
			}
			i += n
			out = append(out, field{number: num, typ: typ, varint: v})

		case wireFixed64:
			if i+8 > len(data) {
				return nil, fmt.Errorf("agy: truncated fixed64 for field %d at offset %d", num, i)
			}
			out = append(out, field{number: num, typ: typ, raw: data[i : i+8]})
			i += 8

		case wireBytes:
			l, n := binary.Uvarint(data[i:])
			if n <= 0 {
				return nil, fmt.Errorf("agy: truncated length for field %d at offset %d", num, i)
			}
			i += n
			if l > uint64(len(data)-i) {
				return nil, fmt.Errorf("agy: truncated length-delimited field %d at offset %d", num, i)
			}
			out = append(out, field{number: num, typ: typ, raw: data[i : i+int(l)]})
			i += int(l)

		case wireFixed32:
			if i+4 > len(data) {
				return nil, fmt.Errorf("agy: truncated fixed32 for field %d at offset %d", num, i)
			}
			out = append(out, field{number: num, typ: typ, raw: data[i : i+4]})
			i += 4

		default:
			return nil, fmt.Errorf("agy: unsupported wire type %d for field %d at offset %d", typ, num, i)
		}
	}
	return out, nil
}

// firstVarint returns the value of the first varint field with the given
// number, or ok=false when there is none.
func firstVarint(fields []field, num int) (uint64, bool) {
	for _, f := range fields {
		if f.number == num && f.typ == wireVarint {
			return f.varint, true
		}
	}
	return 0, false
}

// firstBytes returns the raw payload of the first length-delimited field with
// the given number, or ok=false when there is none. It also serves as the
// accessor for embedded (length-delimited) submessages: the caller decodes
// the returned bytes with a second parseFields call.
func firstBytes(fields []field, num int) ([]byte, bool) {
	for _, f := range fields {
		if f.number == num && f.typ == wireBytes {
			return f.raw, true
		}
	}
	return nil, false
}

// allBytes returns the raw payload of every length-delimited field with the
// given number, in encounter order — the accessor for a repeated message
// field (agy's field 20 key/value pairs).
func allBytes(fields []field, num int) [][]byte {
	var out [][]byte
	for _, f := range fields {
		if f.number == num && f.typ == wireBytes {
			out = append(out, f.raw)
		}
	}
	return out
}

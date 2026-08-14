package dcbor

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// TestRoundTrip checks the two canonical-form properties on every table
// entry: Decode(Encode(v)) == v and Encode(Decode(b)) == b.
func TestRoundTrip(t *testing.T) {
	for _, tc := range allTables() {
		t.Run(tc.name, func(t *testing.T) {
			want := mustHex(t, tc.hex)

			decoded, err := Decode(want)
			if err != nil {
				t.Fatalf("Decode: unexpected error: %v", err)
			}
			if !Equal(decoded, tc.val) {
				t.Errorf("Decode(%s) = %#v, want %#v", tc.hex, decoded, tc.val)
			}

			reencoded, err := Encode(decoded)
			if err != nil {
				t.Fatalf("Encode(Decode(b)): unexpected error: %v", err)
			}
			if hex.EncodeToString(reencoded) != hex.EncodeToString(want) {
				t.Errorf("Encode(Decode(b)) = %s, want %s", hex.EncodeToString(reencoded), hex.EncodeToString(want))
			}
		})
	}
}

// TestDecodeRejects covers every rejection rule of Dialog's dCBOR profile.
func TestDecodeRejects(t *testing.T) {
	tests := []struct {
		name string
		hex  string
		want string
	}{
		// Truncated input.
		{"empty input", "", "unexpected end of input"},
		{"truncated one-byte argument", "18", "unexpected end of input"},
		{"truncated two-byte argument", "1901", "unexpected end of input"},
		{"truncated eight-byte argument", "1b00000001", "unexpected end of input"},
		{"truncated text string", "6261", "exceeds"},
		{"truncated byte string", "5820", "exceeds"},
		{"truncated array", "8201", "exceeds"},
		{"truncated map value", "a16161", "unexpected end of input"},
		{"absurd length", "5bffffffffffffffff", "exceeds"},

		// Non-shortest encodings.
		{"non-shortest uint one byte", "1817", "shortest form"},
		{"non-shortest uint two bytes", "190017", "shortest form"},
		{"non-shortest uint four bytes", "1a00000017", "shortest form"},
		{"non-shortest uint eight bytes", "1b0000000000000017", "shortest form"},
		{"non-shortest uint boundary 255", "1900ff", "shortest form"},
		{"non-shortest uint boundary 65535", "1a0000ffff", "shortest form"},
		{"non-shortest uint boundary 2^32-1", "1b00000000ffffffff", "shortest form"},
		{"non-shortest negative", "3817", "shortest form"},
		{"non-shortest text length", "7817" + strings.Repeat("78", 23), "shortest form"},
		{"non-shortest array length", "980141", "shortest form"},

		// Floats and the rest of major type 7.
		{"half float", "f93c00", "floating-point"},
		{"single float", "fa47c35000", "floating-point"},
		{"double float", "fb3ff199999999999a", "floating-point"},
		{"false", "f4", "boolean"},
		{"true", "f5", "boolean"},
		{"undefined", "f7", "undefined"},
		{"simple value 0", "e0", "simple value 0"},
		{"one-byte simple value", "f8ff", "simple value"},
		{"lone break", "ff", "break"},

		// Tags. Tag 4 is the sole exception (spec/03-encoding.md rule 6).
		{"tag 0 (date/time string)", "c06161", "except tag 4"},
		{"tag 1 (epoch time)", "c11a514b67b0", "except tag 4"},
		{"tag 2 (bignum)", "c249010000000000000000", "except tag 4"},
		{"tag 5 (bigfloat)", "c58221196ab3", "except tag 4"},
		{"tag 42 (CID)", "d82a49000102030405060708", "except tag 4"},
		{"non-shortest tag 4", "d8048221196ab3", "shortest form"},

		// Tag 4 content rules (spec/03-encoding.md, "Decimal fractions").
		{"tag 4 content not an array", "c41901f4", "must be an array of exactly two integers"},
		{"tag 4 content map", "c4a1616100", "must be an array of exactly two integers"},
		{"tag 4 array of 1", "c4812100", "want exactly 2"},
		{"tag 4 array of 3", "c4832119013a00", "want exactly 2"},
		{"tag 4 array of 0", "c480", "want exactly 2"},
		{"tag 4 indefinite array", "c49f2119013aff", "indefinite-length array"},
		{"tag 4 float exponent", "c482f93c0019013a", "exponent must be an integer"},
		{"tag 4 float mantissa", "c48221fb3ff199999999999a", "mantissa must be an integer"},
		{"tag 4 text mantissa", "c4822163616263", "mantissa must be an integer"},
		{"tag 4 nested tag", "c482c4822119013a01", "exponent must be an integer"},
		{"tag 4 non-shortest mantissa", "c4822119003a", "shortest form"},
		{"tag 4 truncated array", "c48221", "exceeds"},
		{"tag 4 truncated mantissa", "c482211901", "unexpected end of input"},
		{"tag 4 truncated after head", "c4", "unexpected end of input in tag 4 content"},
		// Component range (spec/03-encoding.md: both components MUST lie in
		// -2^63 .. 2^63-1); each component is checked in both directions.
		{"tag 4 mantissa above int64 range", "c482201bffffffffffffffff", "outside the int64 range"},
		{"tag 4 mantissa below int64 range", "c482203bffffffffffffffff", "outside the int64 range"},
		{"tag 4 exponent above int64 range", "c4821bffffffffffffffff01", "outside the int64 range"},
		{"tag 4 exponent below int64 range", "c4823bffffffffffffffff01", "outside the int64 range"},
		{"tag 4 mantissa just above int64 range", "c482201b8000000000000000", "outside the int64 range"},
		{"tag 4 exponent just below int64 range", "c4823b800000000000000001", "outside the int64 range"},

		// Tag 4 canonicalization rules.
		{"tag 4 zero exponent", "c4820019013a", "is not negative"},
		{"tag 4 positive exponent", "c4820219013a", "is not negative"},
		{"tag 4 zero mantissa", "c4822100", "mantissa is zero"},
		{"tag 4 mantissa divisible by 10", "c48222190c44", "divisible by 10"},
		{"tag 4 negative mantissa divisible by 10", "c48222390c43", "divisible by 10"},

		// Indefinite lengths.
		{"indefinite byte string", "5f42010243030405ff", "indefinite-length byte string"},
		{"indefinite text string", "7f657374726561646d696e67ff", "indefinite-length text string"},
		{"indefinite array", "9f01ff", "indefinite-length array"},
		{"indefinite map", "bf61610101ff", "indefinite-length map"},

		// Map key rules.
		{"non-text key (uint)", "a10102", "map keys must be text strings"},
		{"non-text key (bytes)", "a1410102", "map keys must be text strings"},
		{"unsorted keys", "a2616200616100", "not in bytewise lexicographic order"},
		{"unsorted keys by encoded head", "a2626161006162 01", "not in bytewise lexicographic order"},
		{"duplicate keys", "a26161006161 00", `duplicate map key "a"`},

		// UTF-8 well-formedness.
		{"invalid UTF-8 text", "6180", "text string is not valid UTF-8"},
		{"invalid UTF-8 map key", "a1618000", "map key is not valid UTF-8"},

		// Reserved additional information.
		{"reserved ai 28", "1c", "reserved additional information value 28"},
		{"reserved ai 30 in major 7", "fe", "reserved additional information value 30"},
		{"indefinite in integer", "1f", "additional information 31"},

		// Framing.
		{"trailing bytes", "0000", "trailing byte"},
		{"trailing bytes after map", "a1616100f6", "trailing byte"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, err := Decode(mustHex(t, tc.hex))
			if err == nil {
				t.Fatalf("Decode(%s) = %#v, want an error containing %q", tc.hex, v, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Decode(%s) error = %v, want it to contain %q", tc.hex, err, tc.want)
			}
			var se *SyntaxError
			if !errors.As(err, &se) {
				t.Errorf("Decode error is %T, want *SyntaxError", err)
			}
		})
	}
}

func TestDecodeRejectsDeepNesting(t *testing.T) {
	deep := strings.Repeat("81", MaxDepth+2) + "80"
	_, err := Decode(mustHex(t, deep))
	if err == nil {
		t.Fatal("Decode: expected an error for excessive nesting")
	}
	if !strings.Contains(err.Error(), "nesting deeper than") {
		t.Errorf("Decode error = %v, want a nesting-depth error", err)
	}

	// One level inside the limit still decodes.
	ok := strings.Repeat("81", MaxDepth) + "80"
	if _, err := Decode(mustHex(t, ok)); err != nil {
		t.Errorf("Decode at the depth limit: unexpected error: %v", err)
	}
}

func TestSyntaxErrorOffset(t *testing.T) {
	// {"a": <double float>} — the offending item starts at byte 3.
	_, err := Decode(mustHex(t, "a16161fb3ff199999999999a"))
	var se *SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("Decode error = %v, want *SyntaxError", err)
	}
	if se.Offset != 3 {
		t.Errorf("SyntaxError.Offset = %d, want 3", se.Offset)
	}
	if !strings.Contains(se.Error(), "offset 3") {
		t.Errorf("SyntaxError.Error() = %q, want it to report the offset", se.Error())
	}
}

func TestDecodedMapIsCanonicallyOrdered(t *testing.T) {
	v, err := Decode(mustHex(t, "a3616100616201616302"))
	if err != nil {
		t.Fatal(err)
	}
	m, ok := v.(Map)
	if !ok {
		t.Fatalf("Decode returned %T, want Map", v)
	}
	want := []string{"a", "b", "c"}
	for i, e := range m {
		if e.Key != want[i] {
			t.Errorf("entry %d key = %q, want %q", i, e.Key, want[i])
		}
	}
}

// TestDecodeCopiesInput checks that a decoded byte string does not alias the
// caller's buffer: digests computed from decoded values must not change when
// the input buffer is reused.
func TestDecodeCopiesInput(t *testing.T) {
	in := mustHex(t, "43010203")
	v, err := Decode(in)
	if err != nil {
		t.Fatal(err)
	}
	in[1] = 0xff
	if !Equal(v, Bytes{1, 2, 3}) {
		t.Errorf("decoded byte string aliases the input buffer: %#v", v)
	}
}

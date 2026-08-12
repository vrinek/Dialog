package dcbor

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

// FuzzRoundTrip asserts the canonical-form property that Dialog's content
// addressing depends on: every byte string Decode accepts is the only
// encoding of the value it decodes to, so re-encoding it must reproduce it
// byte for byte.
func FuzzRoundTrip(f *testing.F) {
	for _, tc := range allTables() {
		b, err := hex.DecodeString(strings.ReplaceAll(tc.hex, " ", ""))
		if err != nil {
			f.Fatalf("bad hex literal in table entry %q: %v", tc.name, err)
		}
		f.Add(b)
	}
	// A few non-canonical and out-of-profile seeds, to give the fuzzer
	// starting points near the rejection boundaries.
	for _, s := range []string{
		"", "1817", "f5", "fb3ff199999999999a", "c101", "9f01ff",
		"a2616200616100", "a26161006161 00", "a10102", "6180", "0000", "ff",
		// Tag 4 decimal fractions: the canonical form and each way of
		// violating the canonicalization rules of spec/03-encoding.md.
		"c4822119013a", "c482213901 39", "c4822001", "c4820019013a",
		"c4820219013a", "c4822100", "c48222190c44", "c48121", "c4832119013a00",
		"c49f2119013aff", "c482f93c0019013a", "c48221fb3ff199999999999a",
		"c4822119003a", "d8048221196ab3", "c482201bffffffffffffffff",
	} {
		b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
		if err != nil {
			f.Fatalf("bad hex seed %q: %v", s, err)
		}
		f.Add(b)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		v, err := Decode(data)
		if err != nil {
			return // rejected input carries no round-trip obligation
		}

		encoded, err := Encode(v)
		if err != nil {
			t.Fatalf("Encode failed for a value Decode accepted: %v (input %x)", err, data)
		}
		if !bytes.Equal(encoded, data) {
			t.Fatalf("Encode(Decode(b)) = %x, want %x", encoded, data)
		}

		again, err := Decode(encoded)
		if err != nil {
			t.Fatalf("Decode rejected its own output: %v (input %x)", err, data)
		}
		if !Equal(again, v) {
			t.Fatalf("Decode(Encode(v)) != v (input %x)", data)
		}
	})
}

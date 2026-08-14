package dcbor

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

// Dialog's identities are hashes of these bytes, so an encoder that produces
// two different byte strings for one value produces two different entities for
// one entity. The property is stated in spec/03-encoding.md, "Deterministic
// CBOR": the encoding of a value is unique.
//
// The failure this file exists to catch is not a wrong encoding — the tables in
// encode_test.go and the conformance vectors cover that — but an unstable one:
// an encoder that consults a Go map, a pointer address, or anything else the
// runtime is free to vary between one call and the next. Such a bug reproduces
// on roughly one run in n and never in the run that reviews the change, which
// is why it is worth a thousand iterations of otherwise redundant assertions.

// determinismIterations is how many times each value is re-encoded. Go
// randomises map iteration order per range statement, so any dependence on it
// shows up within a handful of iterations; a thousand is cheap (the whole test
// is a few milliseconds) and leaves no doubt.
const determinismIterations = 1000

// determinismValues covers every shape of the profile, with the emphasis on
// maps: they are the only place an ordering decision is made.
func determinismValues() []struct {
	name string
	val  Value
} {
	return []struct {
		name string
		val  Value
	}{
		{"uint zero", Uint(0)},
		{"uint max", Uint(^uint64(0))},
		{"neg", Neg(0)},
		{"neg min int64", Int(-9223372036854775808)},
		{"decimal", Decimal{Exponent: -1, Mantissa: 314}},
		{"text ascii", Text("France")},
		{"text unicode", Text("Ελλάδα — 日本 — é")},
		{"bytes", Bytes{0x01, 0x71, 0x12, 0x20}},
		{"empty text", Text("")},
		{"empty bytes", Bytes{}},
		{"empty array", Array{}},
		{"empty map", Map{}},
		{"null", Null},

		// A map given in canonical order and the same map given backwards must
		// encode identically: the encoder sorts, and sorting is the whole
		// ordering rule (spec/03-encoding.md rule 2).
		{"map in order", Map{
			{Key: "a", Value: Uint(1)},
			{Key: "b", Value: Uint(2)},
			{Key: "c", Value: Uint(3)},
		}},
		{"map reversed", Map{
			{Key: "c", Value: Uint(3)},
			{Key: "b", Value: Uint(2)},
			{Key: "a", Value: Uint(1)},
		}},
		// Keys that differ only in length sort by their encoded head first,
		// which is where a bytewise comparison of the plain strings would
		// disagree with the specification.
		{"map length-ordered keys", Map{
			{Key: "aaaaaaaaaa", Value: Uint(1)},
			{Key: "b", Value: Uint(2)},
			{Key: "aa", Value: Uint(3)},
		}},
		// The block-shaped nesting the protocol actually encodes: maps inside
		// arrays inside maps.
		{"nested", Map{
			{Key: "ops", Value: Array{
				Map{{Key: "type", Value: Text("create_atom")}, {Key: "description", Value: Text("France")}},
				Map{{Key: "type", Value: Text("create_bond")}, {Key: "template", Value: Text("_A_ is the capital of _B_")}},
			}},
			{Key: "refs", Value: Array{Bytes(bytes.Repeat([]byte{0x11}, 32))}},
			{Key: "ts", Value: Uint(1740067200)},
			{Key: "v", Value: Uint(1)},
		}},
		{"deep", deepArray(MaxDepth - 1)},
	}
}

// TestEncodeIsDeterministic re-encodes each value determinismIterations times
// in one process and requires every encoding, and every digest taken over one,
// to be identical to the first.
func TestEncodeIsDeterministic(t *testing.T) {
	for _, tc := range determinismValues() {
		t.Run(tc.name, func(t *testing.T) {
			want, err := Encode(tc.val)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			wantDigest := sha256.Sum256(want)

			for i := range determinismIterations {
				got, err := Encode(tc.val)
				if err != nil {
					t.Fatalf("Encode on iteration %d: %v", i, err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("iteration %d encoded to %x, first encoding was %x", i, got, want)
				}
				if got := sha256.Sum256(got); got != wantDigest {
					t.Fatalf("iteration %d hashes to %x, first encoding hashed to %x", i, got, wantDigest)
				}
			}
		})
	}
}

// TestEncodeDecodeEncodeIsStable checks the fixed point that content addressing
// rests on: bytes that survive a decode and a re-encode unchanged. If they did
// not, an entity's identity would depend on whether it had been through the
// wire, and two implementations could agree on the value and disagree on the
// hash.
func TestEncodeDecodeEncodeIsStable(t *testing.T) {
	for _, tc := range determinismValues() {
		t.Run(tc.name, func(t *testing.T) {
			first, err := Encode(tc.val)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			decoded, err := Decode(first)
			if err != nil {
				t.Fatalf("Decode of our own encoding: %v", err)
			}
			again, err := Encode(decoded)
			if err != nil {
				t.Fatalf("Encode(Decode(Encode(v))): %v", err)
			}
			if !bytes.Equal(again, first) {
				t.Fatalf("Encode(Decode(Encode(v))) = %x, want %x", again, first)
			}
			if !Equal(decoded, tc.val) {
				t.Errorf("Decode(Encode(v)) = %#v, want %#v", decoded, tc.val)
			}
		})
	}
}

// TestEncodeIgnoresMapEntryOrder encodes every permutation of a map's entries
// and requires one answer. Order-independence is what makes the canonical form
// canonical: two implementations that build the same map by different routes
// must produce the same bytes and therefore the same digest.
func TestEncodeIgnoresMapEntryOrder(t *testing.T) {
	entries := []MapEntry{
		{Key: "bond", Value: Bytes(bytes.Repeat([]byte{0x22}, 32))},
		{Key: "fillers", Value: Array{Map{{Key: "type", Value: Uint(0)}, {Key: "value", Value: Bytes{0x01}}}}},
		{Key: "ts", Value: Uint(1740067200)},
		{Key: "v", Value: Uint(1)},
	}
	want, err := Encode(Map(entries))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	permutations := 0
	permute(entries, 0, func(p []MapEntry) {
		permutations++
		got, err := Encode(Map(p))
		if err != nil {
			t.Fatalf("Encode of a permutation: %v", err)
		}
		if !bytes.Equal(got, want) {
			keys := make([]string, len(p))
			for i, e := range p {
				keys[i] = e.Key
			}
			t.Fatalf("the entry order %v encoded to %x, want %x", keys, got, want)
		}
	})
	if want := 24; permutations != want { // 4!
		t.Errorf("tried %d permutations, want %d", permutations, want)
	}
}

// permute calls fn with every ordering of entries. The slice is restored
// before each call returns, so fn sees a fresh order and nothing else changes.
func permute(entries []MapEntry, i int, fn func([]MapEntry)) {
	if i == len(entries) {
		fn(entries)
		return
	}
	for j := i; j < len(entries); j++ {
		entries[i], entries[j] = entries[j], entries[i]
		permute(entries, i+1, fn)
		entries[i], entries[j] = entries[j], entries[i]
	}
}

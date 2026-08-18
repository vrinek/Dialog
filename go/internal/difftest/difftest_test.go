package difftest

import (
	"encoding/hex"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/vrinek/Dialog/go/dcbor"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatalf("%q is not hex: %v", s, err)
	}
	return b
}

// TestOracleAgreesWhereItCan is the complement of the allowlist, and it is as
// much of the deliverable as the allowlist is. Every case here is a byte
// string dcbor rejects for a rule the oracle *can* be configured to enforce —
// through its options or through the canonicity round-trip — so both reject
// it and no divergence class is involved.
//
// It exists so that a weakening of the oracle's configuration is caught. If
// someone drops DupMapKeyEnforcedAPF, or the round-trip check, these cases
// stop being agreements and start being unexplained divergences; without this
// test that would show up only as a fuzz failure on a nightly run, or not at
// all if a class happened to cover it.
func TestOracleAgreesWhereItCan(t *testing.T) {
	cases := []struct {
		name  string
		bytes string
		rule  string
	}{
		{"non-shortest one-byte argument", "1817", "rule 1 (shortest integer encoding), via the canonicity round-trip"},
		{"non-shortest two-byte argument", "1900ff", "rule 1, via the canonicity round-trip"},
		{"non-shortest four-byte argument", "1a0000ffff", "rule 1, via the canonicity round-trip"},
		{"non-shortest eight-byte argument", "1b00000000ffffffff", "rule 1, via the canonicity round-trip"},
		{"non-shortest string length", "78176161616161616161616161616161616161616161616161", "rule 1, via the canonicity round-trip"},
		{"map keys out of order", "a2616200616101", "rule 2 (sorted map keys), via the canonicity round-trip"},
		{"map keys in length-first order", "a2626161006162 01", "rule 2: bytewise, so \"b\" precedes \"aa\""},
		{"duplicate map key", "a2616100616101", "rule 3 (no duplicate map keys), via DupMapKeyEnforcedAPF"},
		{"indefinite-length array", "9f01ff", "rule 4 (no indefinite-length items), via IndefLengthForbidden"},
		{"indefinite-length map", "bf6161 01ff", "rule 4, via IndefLengthForbidden"},
		{"indefinite-length byte string", "5f42010243030405ff", "rule 4, via IndefLengthForbidden"},
		{"indefinite-length text string", "7f61616161ff", "rule 4, via IndefLengthForbidden"},
		{"NaN", "f97e00", "rule 5 (no floating-point values), via NaNDecodeForbidden"},
		{"positive infinity", "f97c00", "rule 5, via InfDecodeForbidden"},
		{"negative infinity", "f9fc00", "rule 5, via InfDecodeForbidden"},
		{"bignum tag 2", "c249010000000000000000", "rule 6 (no tags but 4), via BignumTagForbidden"},
		{"bignum tag 3", "c349010000000000000000", "rule 6, via BignumTagForbidden"},
		{"date tag 0", "c074323031332d30332d32315432303a30343a30305a", "rule 6: tag 0 decodes to a time and re-encodes as an integer, failing the round-trip"},
		{"epoch tag 1", "c11a514b67b0", "rule 6: as tag 0"},
		{"false", "f4", "rule 7 (null is the only simple value), via the simple value registry"},
		{"true", "f5", "rule 7, via the registry"},
		{"undefined", "f7", "rule 7, via the registry"},
		{"simple value 16", "f0", "rule 7, via the registry"},
		{"simple value 255", "f8ff", "rule 7, via the registry"},
		{"invalid UTF-8", "62c328", "\"Text strings and Unicode\", via UTF8RejectInvalid"},
		{"invalid UTF-8 in a map key", "a162c32800", "\"Text strings and Unicode\", via UTF8RejectInvalid"},
		{"truncated array", "8201", "well-formedness"},
		{"truncated head", "19", "well-formedness"},
		{"trailing bytes", "0000", "one top-level item"},
		{"reserved additional information 28", "1c", "well-formedness"},
		{"lone break stop code", "ff", "well-formedness"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := mustHex(t, tc.bytes)
			if _, err := dcbor.Decode(data); err == nil {
				t.Fatalf("dcbor accepted %x, which %s forbids", data, tc.rule)
			}
			if verdict := TheOracle().Decode(data); verdict.Accepted {
				t.Fatalf("the oracle accepted %x, which dcbor rejects under %s.\n"+
					"That makes it a divergence with no allowlist entry: either the oracle's "+
					"configuration was weakened, or this case belongs in divergence.go.",
					data, tc.rule)
			}
		})
	}
}

// TestEncodeAgreementAtHeadBoundaries walks the values where a CBOR head
// changes size, in every major type Dialog uses. The fuzzer reaches most of
// them through generate.go's value pools; the two that cost 64 KiB of input
// each — the four-byte length head of a byte string and of a text string — are
// only here, because paying for them on every fuzz execution would cost more
// executions than the case is worth.
func TestEncodeAgreementAtHeadBoundaries(t *testing.T) {
	values := []dcbor.Value{
		dcbor.Uint(0), dcbor.Uint(23), dcbor.Uint(24), dcbor.Uint(255), dcbor.Uint(256),
		dcbor.Uint(65535), dcbor.Uint(65536), dcbor.Uint(4294967295), dcbor.Uint(4294967296),
		dcbor.Uint(math.MaxInt64), dcbor.Uint(math.MaxUint64),
		dcbor.Neg(0), dcbor.Neg(23), dcbor.Neg(24), dcbor.Neg(255), dcbor.Neg(256),
		dcbor.Neg(65535), dcbor.Neg(65536), dcbor.Neg(4294967295), dcbor.Neg(4294967296),
		dcbor.Neg(math.MaxInt64), dcbor.Neg(math.MaxInt64 + 1), dcbor.Neg(math.MaxUint64),
		dcbor.Null,
		dcbor.Decimal{Exponent: -1, Mantissa: 1},
		dcbor.Decimal{Exponent: -2, Mantissa: 314},
		dcbor.Decimal{Exponent: math.MinInt64, Mantissa: math.MaxInt64},
		dcbor.Decimal{Exponent: -1, Mantissa: math.MinInt64},
	}
	for _, n := range []int{0, 1, 23, 24, 255, 256, 65535, 65536} {
		arr := make(dcbor.Array, n)
		for i := range arr {
			arr[i] = dcbor.Uint(0)
		}
		values = append(values,
			dcbor.Bytes(make([]byte, n)),
			dcbor.Text(strings.Repeat("a", n)),
			arr,
		)
	}
	// A map at each entry-count boundary, with keys long enough that the key
	// heads change size too.
	for _, n := range []int{0, 1, 23, 24, 255, 256} {
		m := make(dcbor.Map, 0, n)
		for i := range n {
			m = append(m, dcbor.MapEntry{Key: strings.Repeat("k", i) + strconv.Itoa(i), Value: dcbor.Uint(1)})
		}
		values = append(values, m)
	}

	for i, v := range values {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			checkEncodeAgreement(t, "table", v)
		})
	}
}

// TestEncodeAgreementOnKeyOrdering hands both encoders key sets chosen so that
// a sort by the string, by the string's length, or by the encoded key all give
// different answers, in both input orders.
//
// One thing it deliberately does not claim: that it would catch the oracle
// being set to SortCanonical instead of SortCoreDeterministic. It would not,
// and nothing could. A CBOR text head is monotonic in the string's length, so
// bytewise order over encoded text keys already compares length first; the
// RFC 7049 length-first order and the RFC 8949 §4.2.1 bytewise order are the
// same order on every text-keyed map, and rule 9 makes every Dialog map
// text-keyed. What this test pins is that the two implementations sort by the
// encoded key rather than by the raw string — "b" against "aa" separates those
// — and that they agree on where the head-size boundaries fall.
func TestEncodeAgreementOnKeyOrdering(t *testing.T) {
	pairs := [][]string{
		{"b", "aa"},
		{"z", "aaa", "y", "bb"},
		{"", "a", "aa", "b", "ba", "c"},
		{strings.Repeat("a", 24), "b"},
		{strings.Repeat("a", 256), "b", strings.Repeat("b", 25)},
		{"ÿ", "aa"},
	}
	for i, keys := range pairs {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			m := make(dcbor.Map, 0, len(keys))
			for j, k := range keys {
				//nolint:gosec // G115: j is a small loop index.
				m = append(m, dcbor.MapEntry{Key: k, Value: dcbor.Uint(j)})
			}
			checkEncodeAgreement(t, "key ordering", m)

			// And the same key set the other way round, so that neither
			// encoder can be right by having been handed sorted input.
			reversed := make(dcbor.Map, 0, len(m))
			for j := len(m) - 1; j >= 0; j-- {
				reversed = append(reversed, m[j])
			}
			checkEncodeAgreement(t, "key ordering, reversed", reversed)
		})
	}
}

// TestEncodeAgreementAtTheDepthBound builds a document nested exactly at
// MaxDepth, which no mutator would assemble, and checks both encoders write
// the same bytes for it.
func TestEncodeAgreementAtTheDepthBound(t *testing.T) {
	var v dcbor.Value = dcbor.Decimal{Exponent: -2, Mantissa: 314}
	// The decimal is one container, so MaxDepth-1 arrays around it put it at
	// exactly MaxDepth (spec/03-encoding.md rule 10).
	for range dcbor.MaxDepth - 1 {
		v = dcbor.Array{v}
	}
	checkEncodeAgreement(t, "nested at MaxDepth", v)

	if _, err := dcbor.Encode(dcbor.Array{v}); err == nil {
		t.Fatal("dcbor.Encode accepted a document one level past MaxDepth")
	}
}

// TestGenerateIsTotalAndDeterministic pins the two properties
// FuzzEncodeAgreement relies on: every generated value encodes, and the same
// entropy always produces the same value. A generator that failed the first
// would report its own bugs as codec disagreements; one that failed the second
// would make a fuzz crasher unreproducible.
func TestGenerateIsTotalAndDeterministic(t *testing.T) {
	entropies := [][]byte{nil, {}, {0}, {0xff}, {1, 2, 3}}
	for i := range 512 {
		e := make([]byte, i%37+1)
		for j := range e {
			// A cheap spread; the point is coverage of the kind selector, not
			// statistical quality.
			e[j] = byte(i*31 + j*17)
		}
		entropies = append(entropies, e)
	}

	kinds := map[string]bool{}
	for _, e := range entropies {
		v := Generate(e)
		b, err := dcbor.Encode(v)
		if err != nil {
			t.Fatalf("Generate(%x) produced a value dcbor.Encode refuses: %v\nvalue: %#v", e, err, v)
		}
		if again := Generate(e); !dcbor.Equal(again, v) {
			t.Fatalf("Generate(%x) is not deterministic", e)
		}
		back, err := dcbor.Decode(b)
		if err != nil {
			t.Fatalf("dcbor.Decode rejected Generate(%x)'s own encoding: %v", e, err)
		}
		if !dcbor.Equal(back, v) {
			t.Fatalf("Generate(%x) does not survive an encode/decode round trip", e)
		}
		recordKinds(v, kinds)
	}

	// The generator is worth nothing if it only ever emits integers.
	for _, want := range []string{"uint", "neg", "decimal", "text", "bytes", "null", "array", "map"} {
		if !kinds[want] {
			t.Errorf("no generated value contained a %s; the generator's kind selection is broken", want)
		}
	}
}

func recordKinds(v dcbor.Value, kinds map[string]bool) {
	switch t := v.(type) {
	case dcbor.Uint:
		kinds["uint"] = true
	case dcbor.Neg:
		kinds["neg"] = true
	case dcbor.Decimal:
		kinds["decimal"] = true
	case dcbor.Text:
		kinds["text"] = true
	case dcbor.Bytes:
		kinds["bytes"] = true
	case dcbor.NullValue:
		kinds["null"] = true
	case dcbor.Array:
		kinds["array"] = true
		for _, item := range t {
			recordKinds(item, kinds)
		}
	case dcbor.Map:
		kinds["map"] = true
		for _, e := range t {
			recordKinds(e.Value, kinds)
		}
	}
}

// TestAllowlistIsWellFormed checks the list itself: names are unique and
// non-empty, and every entry cites a rule and explains itself. The allowlist
// is documentation that the fuzz target enforces, so an entry with an empty
// citation is a class with no justification.
func TestAllowlistIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range Allowlist {
		switch {
		case d.Name == "":
			t.Errorf("a divergence class has no name")
		case seen[d.Name]:
			t.Errorf("duplicate divergence class %q", d.Name)
		case d.Rule == "":
			t.Errorf("divergence class %q cites no specification rule", d.Name)
		case !strings.Contains(d.Rule, "spec/03-encoding.md"):
			t.Errorf("divergence class %q cites %q, which is not a rule of spec/03-encoding.md", d.Name, d.Rule)
		case len(d.Why) < 100:
			t.Errorf("divergence class %q explains itself in %d characters; say what the oracle does and why its options cannot do otherwise", d.Name, len(d.Why))
		}
		seen[d.Name] = true
	}
	if len(Allowlist) == 0 {
		t.Fatal("the allowlist is empty")
	}
}

// TestToOracleMatchesTheOracleDecoder checks the converter against the thing
// it is meant to imitate: for a byte string both implementations accept,
// converting dcbor's value must produce exactly what the oracle's decoder
// produced. A converter that drifted would turn every both-accept comparison
// into a false alarm, or worse, into a comparison of nothing.
func TestToOracleMatchesTheOracleDecoder(t *testing.T) {
	for _, s := range []string{
		"00", "17", "1818", "1903e8", "1a000f4240", "1b0000000100000000",
		"1b7fffffffffffffff", "1bffffffffffffffff",
		"20", "37", "3818", "3b7fffffffffffffff", "3bffffffffffffffff",
		"40", "4401020304", "60", "6441424344", "80", "83010203", "a0",
		"a26474797065006576616c756541ff",
		"f6", "c4822119013a", "c48238633a0001869e",
		"a163666f6f81c4822119013a",
	} {
		t.Run(s, func(t *testing.T) {
			data := mustHex(t, s)
			value, err := dcbor.Decode(data)
			if err != nil {
				t.Fatalf("dcbor.Decode: %v", err)
			}
			verdict := TheOracle().Decode(data)
			if !verdict.Accepted {
				t.Fatalf("the oracle rejected %x: %s", data, verdict.Reason)
			}
			converted, err := ToOracle(value)
			if err != nil {
				t.Fatalf("ToOracle: %v", err)
			}
			if !OracleEqual(converted, verdict.Value) {
				t.Errorf("ToOracle(%#v) = %#v, want %#v", value, converted, verdict.Value)
			}
			if !OracleEqual(verdict.Value, converted) {
				t.Errorf("OracleEqual is not symmetric for %x", data)
			}
		})
	}
}

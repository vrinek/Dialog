package vectors

import (
	"strings"

	"github.com/vrinek/Dialog/go/dcbor"
	"github.com/vrinek/Dialog/go/entity"
)

// The rules of spec/03-encoding.md, named exactly as that document numbers
// them. A vector cites the rule it violates so that a failing implementation
// knows which paragraph to read, and so that a rule with no vector is visible.
const (
	ruleShortestInt  = "spec/03-encoding.md, Deterministic CBOR rule 1 (shortest integer encoding)"
	ruleSortedKeys   = "spec/03-encoding.md, Deterministic CBOR rule 2 (sorted map keys)"
	ruleUniqueKeys   = "spec/03-encoding.md, Deterministic CBOR rule 3 (no duplicate map keys)"
	ruleDefinite     = "spec/03-encoding.md, Deterministic CBOR rule 4 (no indefinite-length items)"
	ruleNoFloats     = "spec/03-encoding.md, Deterministic CBOR rule 5 (no floating-point values)"
	ruleOnlyTag4     = "spec/03-encoding.md, Deterministic CBOR rule 6 (no tags, with one exception)"
	ruleOnlyNull     = "spec/03-encoding.md, Deterministic CBOR rule 7 (null is the only simple value)"
	ruleTextKeys     = "spec/03-encoding.md, Deterministic CBOR rule 9 (text map keys)"
	ruleDepth        = "spec/03-encoding.md, Deterministic CBOR rule 10 (bounded nesting depth)"
	ruleUTF8         = "spec/03-encoding.md, Text strings and Unicode (text strings MUST be well-formed UTF-8)"
	ruleDecimal      = "spec/03-encoding.md, Decimal fractions"
	ruleDecimalRange = "spec/03-encoding.md, Decimal fractions (both components lie in -2^63 … 2^63-1)"
	ruleWellFormed   = "RFC 8949 §3 (a Dialog document is exactly one well-formed data item)"
)

// maxNestingDepth is the bound of spec/03-encoding.md, "Deterministic CBOR"
// rule 10, counted as that rule counts: the outermost container is at depth 1.
// It is written out here rather than read from dcbor.MaxDepth, because these
// vectors pin the specification's number and not this implementation's
// constant.
const maxNestingDepth = 64

// nestedArrays puts v inside n arrays, so that v's own depth is n+1.
func nestedArrays(v dcbor.Value, n int) dcbor.Value {
	for i := 0; i < n; i++ {
		v = dcbor.Array{v}
	}
	return v
}

// deepArrayHex is the encoding of n nested arrays whose innermost is empty:
// n-1 one-byte array heads and an empty array, the innermost at depth n. It
// builds the bytes directly because the cases past the bound are ones this
// implementation's encoder refuses to produce.
func deepArrayHex(n int) string { return strings.Repeat("81", n-1) + "80" }

// dcborDocument builds vectors/dcbor.json.
func dcborDocument() Document {
	return Document{
		Vectors:     Format,
		Area:        "dcbor",
		Description: "Dialog's deterministic CBOR profile: the encoding-reference table of the specification, the decimal-fraction canonicalization rules, a canonical/non-canonical pair list, and the byte strings a conforming decoder MUST reject.",
		Spec:        []string{"spec/03-encoding.md"},
		Sections: []Section{
			{
				Name:        "encoding_reference",
				Description: "The \"CBOR encoding reference\" table of spec/03-encoding.md, one case per row, each shown in a value the protocol actually encodes rather than as a bare head byte.",
				Cases:       encodingReferenceCases(),
			},
			{
				Name:        "canonical",
				Description: "Values whose encoding exercises a boundary of the profile: integer argument widths, key ordering, nesting, and the raw-UTF-8 rule. Encode(value) MUST produce these bytes and Decode(bytes) MUST produce this value.",
				Cases:       canonicalCases(),
			},
			{
				Name:        "decimal_fractions",
				Description: "Tag 4, the only tag in the profile. The canonicalization rules of spec/03-encoding.md, \"Decimal fractions\", make one byte string per value; the non-canonical spellings are in the invalid section.",
				Cases:       decimalCases(),
			},
			{
				Name:        "invalid",
				Description: "Byte strings a conforming decoder MUST reject, each naming the rule it violates. An implementation that accepts any of them computes identifiers for structures another implementation would refuse.",
				Cases:       invalidDCBORCases(),
			},
		},
	}
}

// franceDigestValue is the 32-byte digest of the atom {"description":
// "France"} — the worked example of spec/03-encoding.md — used wherever a
// case needs a byte string of the size a reference takes.
func franceDigestValue() dcbor.Bytes {
	return dcbor.Bytes(entity.MustAtom("France").Digest().Bytes())
}

func dcase(name, note string, v dcbor.Value) DCBORCase {
	return DCBORCase{Name: name, Note: note, Value: describe(v), DCBOR: hexOf(dcbor.MustEncode(v))}
}

func encodingReferenceCases() []DCBORCase {
	return []DCBORCase{
		dcase("map_with_1_key", "Head byte a1: major type 5, additional information 1. This is the atom of spec/03-encoding.md, \"Encoding an atom\".",
			dcbor.Map{{Key: "description", Value: dcbor.Text("France")}}),
		dcase("map_with_2_keys", "Head byte a2. This is an atom filler (spec/01-data-model.md, \"Filler types\").",
			dcbor.Map{
				{Key: "type", Value: dcbor.Uint(0)},
				{Key: "value", Value: franceDigestValue()},
			}),
		dcase("short_text_string", "Major type 3 with the length in the head byte, for lengths below 24: 66 for six bytes.",
			dcbor.Text("France")),
		dcase("longer_text_string", "Major type 3 with a one-byte length argument, for lengths 24-255: 78 1c for 28 bytes.",
			dcbor.Text("Paris, the capital of France")),
		dcase("byte_string_32", "5820: the encoding every internal reference takes (spec/03-encoding.md, \"Internal references\").",
			franceDigestValue()),
		dcase("null", "f6, the only simple value the profile admits. This is the prev field of a genesis block.", dcbor.Null),
		dcase("integer_0", "00: major type 0, value in the head byte.", dcbor.Uint(0)),
		dcase("integer_1", "01. This is the v field of a v1 block.", dcbor.Uint(1)),
		dcase("empty_array", "80: major type 4, length 0. This is the refs list of a block that references nothing.", dcbor.Array{}),
		dcase("decimal_fraction", "c482 followed by exponent and mantissa: 3.14 = 314 × 10^-2.",
			mustDecimal(-2, 314)),
	}
}

func canonicalCases() []DCBORCase {
	// Every integer boundary at which the argument width changes, in both
	// directions. Getting one of these wrong produces bytes that decode to the
	// right number and hash to the wrong digest.
	cases := []DCBORCase{
		dcase("uint_23_head", "The largest value that fits in the head byte.", dcbor.Uint(23)),
		dcase("uint_24_one_byte_argument", "The first value needing a one-byte argument: 18 18.", dcbor.Uint(24)),
		dcase("uint_255", "", dcbor.Uint(255)),
		dcase("uint_256_two_byte_argument", "", dcbor.Uint(256)),
		dcase("uint_65535", "", dcbor.Uint(65535)),
		dcase("uint_65536_four_byte_argument", "", dcbor.Uint(65536)),
		dcase("uint_4294967295", "", dcbor.Uint(4294967295)),
		dcase("uint_4294967296_eight_byte_argument", "", dcbor.Uint(4294967296)),
		dcase("uint_max", "2^64-1, the largest CBOR unsigned integer.", dcbor.Uint(^uint64(0))),
		dcase("uint_timestamp", "1740067200, the timestamp of the worked examples: 1a 67b75180.", dcbor.Uint(1740067200)),
		dcase("neg_minus_1", "Major type 1 encodes -1-n, so -1 is 20.", dcbor.Int(-1)),
		dcase("neg_minus_24", "", dcbor.Int(-24)),
		dcase("neg_minus_25", "The first negative value needing a one-byte argument.", dcbor.Int(-25)),
		dcase("neg_int64_min", "-2^63, the most negative value a signed 64-bit integer holds.", dcbor.Int(-1<<63)),
		dcase("neg_min", "-2^64, the most negative CBOR integer. It does not fit in a signed 64-bit integer; a reader that needs one rejects it where the protocol does not permit it.", dcbor.Neg(^uint64(0))),
		dcase("empty_text", "60: a text string of length 0.", dcbor.Text("")),
		dcase("empty_bytes", "40: a byte string of length 0.", dcbor.Bytes{}),
		dcase("empty_map", "a0: a map with no entries.", dcbor.Map{}),
	}

	// The two spellings of "é" — U+00E9 and U+0065 U+0301 — encode
	// differently and therefore name different entities. Normalization is
	// forbidden (spec/03-encoding.md, "Text strings and Unicode"), and these
	// two cases are what an implementation that normalizes fails.
	cases = append(cases,
		dcase("text_precomposed_e_acute", "\"é\" as U+00E9: two UTF-8 bytes. The profile forbids Unicode normalization, so this is a different string from the decomposed spelling below and the atoms containing them are different entities.",
			dcbor.Text("é")),
		dcase("text_decomposed_e_acute", "\"é\" as U+0065 U+0301: three UTF-8 bytes, rendering identically to the case above and encoding differently.",
			dcbor.Text("é")),
		dcase("text_four_byte_rune", "A code point outside the basic multilingual plane, encoded as four UTF-8 bytes.",
			dcbor.Text("\U0001f5ff")),
	)

	// Key ordering is bytewise over the *encoded* key, so the length byte
	// leads: every shorter key sorts before every longer one, whatever the
	// characters say.
	cases = append(cases,
		dcase("map_key_order_length_first", "The encoded keys sort 6162 (\"b\") before 626161 (\"aa\"): the head byte carries the length, so a shorter key always sorts first. The entries are listed here in canonical order.",
			dcbor.Map{
				{Key: "aa", Value: dcbor.Uint(1)},
				{Key: "b", Value: dcbor.Uint(2)},
			}),
		dcase("map_key_order_bytewise", "Keys of equal length sort by their UTF-8 bytes; uppercase sorts before lowercase.",
			dcbor.Map{
				{Key: "b", Value: dcbor.Uint(2)},
				{Key: "B", Value: dcbor.Uint(1)},
				{Key: "a", Value: dcbor.Uint(0)},
			}),
		dcase("block_key_order", "The eight keys of a public block map, in the canonical order every implementation must produce: v, ts, ops, pub, sig, prev, refs, type.",
			dcbor.Map{
				{Key: "type", Value: dcbor.Text("public")},
				{Key: "refs", Value: dcbor.Array{}},
				{Key: "prev", Value: dcbor.Null},
				{Key: "sig", Value: dcbor.Uint(0)},
				{Key: "pub", Value: dcbor.Uint(0)},
				{Key: "ops", Value: dcbor.Array{}},
				{Key: "ts", Value: dcbor.Uint(1740067200)},
				{Key: "v", Value: dcbor.Uint(1)},
			}),
		dcase("nesting_depth_64", "The deepest structure the profile admits: 63 arrays around an empty one, so the innermost container is at nesting depth 64 (spec/03-encoding.md, \"Deterministic CBOR\" rule 10, which counts the outermost container as depth 1). One level deeper is in the invalid section.",
			nestedArrays(dcbor.Array{}, maxNestingDepth-1)),
		dcase("nested_structure", "Arrays and maps nest; a molecule's fillers list is an array of maps.",
			dcbor.Map{
				{Key: "fillers", Value: dcbor.Array{
					dcbor.Map{{Key: "type", Value: dcbor.Uint(0)}, {Key: "value", Value: franceDigestValue()}},
					dcbor.Map{{Key: "type", Value: dcbor.Uint(4)}, {Key: "value", Value: dcbor.Map{{Key: "value", Value: dcbor.Int(42)}}}},
				}},
				{Key: "bond", Value: franceDigestValue()},
			}),
	)
	return cases
}

func decimalCases() []DCBORCase {
	return []DCBORCase{
		dcase("pi_two_places", "3.14 = 314 × 10^-2, the worked example of spec/03-encoding.md.", mustDecimal(-2, 314)),
		dcase("negative_mantissa", "-3.14. The sign lives in the mantissa; the exponent stays negative.", mustDecimal(-2, -314)),
		dcase("one_thousandth", "0.001 = 1 × 10^-3.", mustDecimal(-3, 1)),
		dcase("mantissa_not_divisible_by_10", "31.4 = 314 × 10^-1. The same digits as 3.14 with a different exponent denote a different value.", mustDecimal(-1, 314)),
		dcase("large_mantissa", "The mantissa may use the whole signed 64-bit range: 2^63-1 × 10^-1.", mustDecimal(-1, 1<<63-1)),
		dcase("smallest_exponent", "The exponent may reach -2^63.", mustDecimal(-1<<63, 3)),
	}
}

// mustDecimal builds a canonical tag 4 value, panicking on one that violates
// the canonicalization rules — which would be a bug in this file, not input.
func mustDecimal(exponent, mantissa int64) dcbor.Value {
	v, err := dcbor.NewDecimal(exponent, mantissa)
	if err != nil {
		panic("vectors: " + err.Error())
	}
	return v
}

func invalidDCBORCases() []InvalidCase {
	// name, the rule it violates, why, and the bytes.
	type bad struct{ name, rule, reason, bytes string }
	list := []bad{
		// Rule 1 — shortest integer encoding.
		{"uint_24_in_one_byte_argument", ruleShortestInt, "23 encoded as 18 17; values below 24 belong in the head byte.", "1817"},
		{"uint_two_byte_argument_for_small_value", ruleShortestInt, "23 encoded as 19 0017.", "190017"},
		{"uint_eight_byte_argument_for_small_value", ruleShortestInt, "23 encoded as 1b 0000000000000017.", "1b0000000000000017"},
		{"uint_255_in_two_byte_argument", ruleShortestInt, "255 fits in a one-byte argument.", "1900ff"},
		{"neg_non_shortest", ruleShortestInt, "-24 encoded with a one-byte argument.", "3817"},
		{"text_length_non_shortest", ruleShortestInt, "A 23-byte text string with a one-byte length argument.", "7817" + strings.Repeat("78", 23)},
		{"array_length_non_shortest", ruleShortestInt, "A one-element array with a one-byte length argument.", "980141"},
		{"tag_4_non_shortest", ruleShortestInt, "Tag 4 written as d8 04 rather than c4.", "d8048221196ab3"},

		// Rule 2 — sorted map keys.
		{"map_keys_unsorted", ruleSortedKeys, "\"b\" before \"a\".", "a2616200616100"},
		{"map_keys_unsorted_by_length", ruleSortedKeys, "\"aa\" (626161) before \"b\" (6162): the ordering is over the encoded key, so the shorter key comes first.", "a262616100616201"},

		// Rule 3 — no duplicate map keys.
		{"map_duplicate_key", ruleUniqueKeys, "The key \"a\" appears twice.", "a2616100616100"},

		// Rule 4 — no indefinite-length items.
		{"indefinite_byte_string", ruleDefinite, "Major type 2 with additional information 31.", "5f42010243030405ff"},
		{"indefinite_text_string", ruleDefinite, "Major type 3 with additional information 31.", "7f657374726561646d696e67ff"},
		{"indefinite_array", ruleDefinite, "Major type 4 with additional information 31.", "9f01ff"},
		{"indefinite_map", ruleDefinite, "Major type 5 with additional information 31.", "bf61610101ff"},

		// Rule 5 — no floating-point values.
		{"half_float", ruleNoFloats, "Major type 7, additional information 25.", "f93c00"},
		{"single_float", ruleNoFloats, "Major type 7, additional information 26.", "fa47c35000"},
		{"double_float", ruleNoFloats, "Major type 7, additional information 27.", "fb3ff199999999999a"},

		// Rule 6 — no tags, with one exception.
		{"tag_0_date_time", ruleOnlyTag4, "Tag 0 (RFC 3339 date/time string).", "c06161"},
		{"tag_1_epoch_time", ruleOnlyTag4, "Tag 1 (epoch-based date/time). A Dialog timestamp is a plain integer.", "c11a514b67b0"},
		{"tag_2_bignum", ruleOnlyTag4, "Tag 2 (unsigned bignum).", "c249010000000000000000"},
		{"tag_42_cid", ruleOnlyTag4, "Tag 42, the IPLD CID tag. Dialog carries references as plain 32-byte byte strings.", "d82a49000102030405060708"},

		// Rule 7 — null is the only simple value.
		{"false", ruleOnlyNull, "The profile admits no booleans.", "f4"},
		{"true", ruleOnlyNull, "The profile admits no booleans.", "f5"},
		{"undefined", ruleOnlyNull, "Major type 7, simple value 23.", "f7"},
		{"simple_value_0", ruleOnlyNull, "Major type 7, simple value 0.", "e0"},
		{"one_byte_simple_value", ruleOnlyNull, "Major type 7, additional information 24.", "f8ff"},

		// Map keys.
		{"map_key_uint", ruleTextKeys, "A map keyed by an integer.", "a10102"},
		{"map_key_bytes", ruleTextKeys, "A map keyed by a byte string.", "a1410102"},

		// Rule 10 — bounded nesting depth. The accepted boundary case is
		// canonical/nesting_depth_64.
		{"nesting_depth_65", ruleDepth, "65 nested arrays: one container past the bound. The outermost array is at depth 1, so the innermost is at depth 65.", deepArrayHex(maxNestingDepth + 1)},
		{"nesting_depth_65_decimal", ruleDepth, "A decimal fraction inside 64 arrays. Tag 4 and its content array are one container, so the decimal fraction is at depth 65.", strings.Repeat("81", maxNestingDepth) + "c4822119013a"},
		{"nesting_far_beyond_the_bound", ruleDepth, "256 nested arrays, four times the bound. A decoder MUST reject this as malformed input rather than exhaust its stack on it.", deepArrayHex(4 * maxNestingDepth)},

		// UTF-8.
		{"text_invalid_utf8", ruleUTF8, "A one-byte text string holding 0x80, a bare continuation byte.", "6180"},
		{"map_key_invalid_utf8", ruleUTF8, "A map key that is not well-formed UTF-8.", "a1618000"},

		// Decimal fractions.
		{"decimal_exponent_zero", ruleDecimal, "#6.4([0, 314]): a whole number must be a plain integer, so the exponent MUST be negative.", "c4820019013a"},
		{"decimal_exponent_positive", ruleDecimal, "#6.4([2, 314]): the exponent MUST be negative.", "c4820219013a"},
		{"decimal_mantissa_zero", ruleDecimal, "#6.4([-1, 0]): zero is a whole number and is encoded as the integer 0.", "c4822000"},
		{"decimal_mantissa_trailing_zero", ruleDecimal, "#6.4([-3, 3140]) denotes 3.14, which is #6.4([-2, 314]): trailing zeros MUST be absorbed into the exponent.", "c48222190c44"},
		{"decimal_array_of_one", ruleDecimal, "Tag 4 content MUST be an array of exactly two integers.", "c4812100"},
		{"decimal_array_of_three", ruleDecimal, "Tag 4 content MUST be an array of exactly two integers.", "c4832119013a00"},
		{"decimal_indefinite_array", ruleDecimal, "Tag 4 content with an indefinite-length array.", "c49f2119013aff"},
		{"decimal_float_exponent", ruleDecimal, "Neither component may be a float.", "c482f93c0019013a"},
		{"decimal_nested_tag", ruleDecimal, "Neither component may itself be a tag.", "c482c4822119013a01"},
		{"decimal_mantissa_above_int64", ruleDecimalRange, "A mantissa of 2^64-1.", "c482201bffffffffffffffff"},
		{"decimal_exponent_below_int64", ruleDecimalRange, "An exponent of -2^64.", "c4823bffffffffffffffff01"},

		// Well-formedness and framing.
		{"empty_input", ruleWellFormed, "No data item at all.", ""},
		{"truncated_argument", ruleWellFormed, "A one-byte argument with no byte after it.", "18"},
		{"truncated_text_string", ruleWellFormed, "A two-byte text string with one byte of content.", "6261"},
		{"truncated_byte_string", ruleWellFormed, "A 32-byte byte string with no content.", "5820"},
		{"declared_length_beyond_input", ruleWellFormed, "A byte string declaring 2^64-1 bytes.", "5bffffffffffffffff"},
		{"trailing_bytes", ruleWellFormed, "Two data items where one is expected.", "0000"},
		{"trailing_bytes_after_map", ruleWellFormed, "A complete map followed by a null.", "a1616100f6"},
		{"reserved_additional_information", ruleWellFormed, "Additional information 28 is reserved.", "1c"},
		{"break_outside_indefinite", ruleWellFormed, "A break byte with no indefinite-length item to close.", "ff"},
	}

	out := make([]InvalidCase, 0, len(list))
	for _, c := range list {
		out = append(out, InvalidCase{Name: c.name, Rule: c.rule, Reason: c.reason, Bytes: c.bytes})
	}
	return out
}

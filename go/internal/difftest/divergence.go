package difftest

import (
	"math"
	"math/big"

	"github.com/fxamacker/cbor/v2"
	"github.com/vrinek/Dialog/go/dcbor"
)

// A Divergence is one class of input on which Dialog's dCBOR profile is
// stricter than generic deterministic CBOR: the oracle accepts it, Dialog's
// decoder rejects it, and that is the correct outcome on both sides.
//
// The set of them — [Allowlist] — is the point of this module. Every entry is
// a restriction spec/03-encoding.md adds on top of RFC 8949 §4.2.1 that the
// oracle has no option to express, together with the rule that makes Dialog
// stricter. FuzzDecodeAgreement fails on any disagreement it cannot attribute
// to one of these, so the list is not documentation *about* the difference
// between the two profiles: it is the difference, enforced.
type Divergence struct {
	// Name identifies the class in a failure message and in tests.
	Name string
	// Rule cites the specification text that makes Dialog stricter.
	Rule string
	// Why says what the oracle does, why its options cannot be made to do
	// otherwise, and what Dialog does instead.
	Why string
}

// The divergence classes. Adding one is a claim that Dialog is deliberately
// stricter than deterministic CBOR in a way no oracle option can express;
// removing one is a claim that the oracle can now be configured to agree.
// Neither should happen quietly.
var (
	// DivergenceFloat is rule 5: Dialog has no floating-point values at all.
	DivergenceFloat = Divergence{
		Name: "float",
		Rule: `spec/03-encoding.md, "Deterministic CBOR" rule 5`,
		Why: "Dialog admits no floating-point value: every number is an integer, " +
			"and a non-integer scalar is a tag 4 decimal fraction. RFC 8949 §4.2.1 " +
			"has no such restriction — it only pins which float encoding to prefer — " +
			"and the oracle's options reach only the two degenerate cases, NaN " +
			"(NaNDecodeForbidden) and infinity (InfDecodeForbidden). There is no knob " +
			"that forbids a finite float, so f9/fa/fb heads carrying a finite value are " +
			"accepted by the oracle and rejected by dcbor.",
	}

	// DivergenceTagOtherThanFour is rule 6: tag 4 and nothing else.
	DivergenceTagOtherThanFour = Divergence{
		Name: "tag-other-than-4",
		Rule: `spec/03-encoding.md, "Deterministic CBOR" rule 6`,
		Why: "Dialog permits exactly one tag, 4 (decimal fraction), and requires every " +
			"other tag to be rejected. The oracle's TagsMd option is all-or-nothing: " +
			"TagsForbidden would reject tag 4 along with the rest, so the harness sets " +
			"TagsAllowed and classifies here. An unrecognized tag decodes to an opaque " +
			"cbor.Tag and re-encodes unchanged, so it survives the canonicity round-trip. " +
			"Tags 0 and 1 (date/time) do not reach this class — they decode to time.Time " +
			"and re-encode as a bare integer, which fails the round-trip — and tags 2 and 3 " +
			"(bignum) do not either, because BignumTagForbidden rejects them outright.",
	}

	// DivergenceDecimalNotCanonical is the canonicalization of tag 4, which
	// exists because a CID is a hash of bytes and a value with two encodings
	// would have two identifiers.
	DivergenceDecimalNotCanonical = Divergence{
		Name: "decimal-not-canonical",
		Rule: `spec/03-encoding.md, "Decimal fractions"`,
		Why: "The oracle has no decimal type. Tag 4 decodes to an opaque cbor.Tag whose " +
			"content is whatever CBOR followed the tag head, and re-encodes unchanged, so " +
			"every well-formed tag 4 passes the canonicity round-trip. Dialog requires the " +
			"content to be a definite-length array of exactly two shortest-form major type " +
			"0 or 1 integers, each inside the signed 64-bit range, with a negative exponent " +
			"and a mantissa that is neither zero nor divisible by 10. Everything else — " +
			"#6.4([0, 3]), #6.4([-2, 3140]), #6.4([-1, 314, 0]), a float or a nested tag as " +
			"a component, a component beyond int64 — is accepted by the oracle and rejected " +
			"by dcbor as non-canonical.",
	}

	// DivergenceNonTextMapKey is rule 9, the schema-free half of closed maps.
	DivergenceNonTextMapKey = Divergence{
		Name: "non-text-map-key",
		Rule: `spec/03-encoding.md, "Deterministic CBOR" rule 9`,
		Why: "Every Dialog map key is a text string, whether or not the decoder holds the " +
			"definition of the map. RFC 8949 places no restriction on key types and the " +
			"oracle has no option that does: decoding into an empty interface produces a " +
			"map[any]any, which takes any hashable key, and the oracle goes out of its way " +
			"to keep a byte-string key usable by converting it to cbor.ByteString. Integer, " +
			"byte-string, null, float, array-free tag and every other key type is therefore " +
			"accepted by the oracle and rejected by dcbor.",
	}

	// DivergenceDepth is rule 10, the defensive bound that makes hostile
	// nesting a rejection everywhere instead of a stack overflow somewhere.
	DivergenceDepth = Divergence{
		Name: "depth",
		Rule: `spec/03-encoding.md, "Deterministic CBOR" rule 10`,
		Why: "Dialog bounds container nesting at 64 and fixes the bound in the specification " +
			"so that every implementation rejects the same bytes. The oracle's " +
			"MaxNestedLevels is a resource guard rather than a profile rule: it caps at " +
			"65535, it counts a tag as a level of its own where Dialog counts tag 4 and its " +
			"content array as one, and any value it is given is the oracle's own policy and " +
			"not RFC 8949's. The harness sets it to the maximum on purpose, so that the " +
			"whole band from depth 65 upward surfaces here instead of being hidden by a " +
			"limit chosen to match.",
	}
)

// Allowlist is every divergence class, in the order they are declared above.
// A disagreement FuzzDecodeAgreement cannot attribute to at least one of these
// fails the target.
var Allowlist = []Divergence{
	DivergenceFloat,
	DivergenceTagOtherThanFour,
	DivergenceDecimalNotCanonical,
	DivergenceNonTextMapKey,
	DivergenceDepth,
}

// Divergences returns every class the oracle-side value v exhibits, in
// Allowlist order. An empty result on a value dcbor rejected is a genuine
// disagreement: either dcbor is rejecting something the profile permits, or
// the profile is stricter in a way this list does not record.
//
// It is deliberately a classification of the decoded *value* rather than of
// the bytes. Re-deriving the classes from the bytes would mean writing a third
// CBOR decoder inside the harness that verifies the other two, which is one
// decoder too many; the oracle has already parsed the input, and every class
// above is visible in what it parsed it into.
//
// One limitation is worth stating rather than discovering. This reports the
// classes an input *exhibits*, not the reason dcbor gave, so an input that
// contains a float somewhere and is also rejected for a reason nobody
// anticipated would be attributed to the float and pass. Narrowing that would
// mean matching against dcbor's error text, which would make the harness
// depend on the wording of messages rather than on behaviour. What keeps the
// blind spot small is that the classes are structural and rare: an input
// carrying one is an input the fuzzer built around that feature, and a dcbor
// bug hiding behind one would have to co-occur with it on every input the
// fuzzer found.
func Divergences(v any) []Divergence {
	found := map[string]bool{}
	if dialogDepth(v) > dcbor.MaxDepth {
		found[DivergenceDepth.Name] = true
	}
	classify(v, found)

	out := make([]Divergence, 0, len(found))
	for _, d := range Allowlist {
		if found[d.Name] {
			out = append(out, d)
		}
	}
	return out
}

// classify walks an oracle-side value, recording the classes it exhibits.
func classify(v any, found map[string]bool) {
	switch t := v.(type) {
	case float64, float32:
		found[DivergenceFloat.Name] = true
	case cbor.Tag:
		if t.Number == tagDecimalFraction {
			if !canonicalDecimal(t.Content) {
				found[DivergenceDecimalNotCanonical.Name] = true
			}
		} else {
			found[DivergenceTagOtherThanFour.Name] = true
		}
		classify(t.Content, found)
	case []any:
		for _, item := range t {
			classify(item, found)
		}
	case map[any]any:
		for key, val := range t {
			if _, ok := key.(string); !ok {
				found[DivergenceNonTextMapKey.Name] = true
			}
			classify(key, found)
			classify(val, found)
		}
	}
}

// canonicalDecimal reports whether the content of a tag 4 is the canonical
// decimal fraction of spec/03-encoding.md, "Decimal fractions".
//
// It does not need to check shortest-form encoding of the two integers: a
// non-shortest argument anywhere in the input already fails the oracle's
// canonicity round-trip, so such bytes never reach classification.
func canonicalDecimal(content any) bool {
	arr, ok := content.([]any)
	if !ok || len(arr) != 2 {
		return false
	}
	exponent, ok := asInt64(arr[0])
	if !ok || exponent >= 0 {
		return false
	}
	mantissa, ok := asInt64(arr[1])
	return ok && mantissa != 0 && mantissa%10 != 0
}

// asInt64 reports the value of an oracle-side integer, and whether it is one
// that fits the signed 64-bit range spec/03-encoding.md bounds a decimal
// fraction's components to. A big.Int here is a major type 1 integer below
// math.MinInt64: outside the range, and so not a component of any decimal
// fraction Dialog accepts.
func asInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case uint64:
		if t > math.MaxInt64 {
			return 0, false
		}
		//nolint:gosec // G115: the guard above is the range check.
		return int64(t), true
	case int64:
		return t, true
	case big.Int:
		return 0, false
	default:
		return 0, false
	}
}

// dialogDepth is the nesting depth of an oracle-side value under
// spec/03-encoding.md rule 10: a container is an array or a map, the outermost
// one is at depth 1, non-containers add nothing, and a tag 4 with its content
// array counts as exactly one container rather than two.
//
// A tag other than 4 is transparent here. Dialog has no depth rule for it
// because Dialog has no such tag at all, and any value containing one already
// carries [DivergenceTagOtherThanFour]; making the tag transparent keeps this
// function from inventing a depth Dialog never assigns.
func dialogDepth(v any) int {
	switch t := v.(type) {
	case []any:
		return 1 + maxDepthOf(t)
	case map[any]any:
		deepest := 0
		for key, val := range t {
			deepest = max(deepest, dialogDepth(key), dialogDepth(val))
		}
		return 1 + deepest
	case cbor.Tag:
		if t.Number != tagDecimalFraction {
			return dialogDepth(t.Content)
		}
		if arr, ok := t.Content.([]any); ok {
			return 1 + maxDepthOf(arr)
		}
		return 1 + dialogDepth(t.Content)
	default:
		return 0
	}
}

func maxDepthOf(items []any) int {
	deepest := 0
	for _, item := range items {
		deepest = max(deepest, dialogDepth(item))
	}
	return deepest
}

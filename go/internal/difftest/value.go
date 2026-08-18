package difftest

import (
	"bytes"
	"fmt"
	"math"
	"math/big"

	"github.com/fxamacker/cbor/v2"
	"github.com/vrinek/Dialog/go/dcbor"
)

// tagDecimalFraction is CBOR tag 4, the only tag inside Dialog's profile
// (spec/03-encoding.md, "Deterministic CBOR" rule 6).
const tagDecimalFraction uint64 = 4

// ToOracle converts a dcbor.Value into the Go value the oracle uses for the
// same CBOR item.
//
// The mapping is chosen so that it is exact in both directions: for every b
// that both implementations accept, ToOracle(dcbor.Decode(b)) is what the
// oracle's decoder produces for b, and the oracle's encoder turns it back into
// b. That is what lets the same function serve the encode target (where the
// value is generated) and the decode target (where the two decoders' outputs
// are compared).
//
//	dcbor.Uint      -> uint64                 (major type 0, whole range)
//	dcbor.Neg       -> int64, or big.Int      (major type 1; below MinInt64 no
//	                                           Go native type covers it, and
//	                                           BigIntConvertShortest makes the
//	                                           oracle emit major type 1 for it
//	                                           rather than a bignum tag)
//	dcbor.Text      -> string
//	dcbor.Bytes     -> []byte
//	dcbor.Array     -> []any
//	dcbor.Map       -> map[any]any            (text keys; see below)
//	dcbor.NullValue -> nil
//	dcbor.Decimal   -> cbor.Tag{4, []any{exponent, mantissa}}
//
// Two things about that last line are worth being precise about, because they
// are the one place where the comparison is narrower than it looks.
//
// First, the oracle has no decimal type. It treats tag 4 as an opaque tag and
// encodes whatever content it is handed, so what the encode target compares
// for a Decimal is the *framing*: the tag head, the two-element array head and
// the two integer heads, each in shortest form. It is not a check that the
// oracle would have canonicalized the decimal the same way, because the oracle
// would not have canonicalized it at all.
//
// Second, the canonicalization itself — exponent negative, mantissa non-zero
// and not divisible by 10, both components inside int64 — is Dialog-specific
// (spec/03-encoding.md, "Decimal fractions"). dcbor.Decimal can only hold a
// canonical pair, since dcbor.Encode rejects anything else, so the harness
// only ever hands the oracle canonical content and the two encoders agree.
// The other side of that rule — that the oracle would happily accept
// #6.4([-2, 3140]) on decode — is the divergence class
// [DivergenceDecimalNotCanonical], where it is asserted rather than hidden.
//
// The integer components follow the same uint64/int64 split as top-level
// integers, because that is what the oracle's decoder produces for them and
// the decode target compares the two trees for equality.
func ToOracle(v dcbor.Value) (any, error) {
	switch val := v.(type) {
	case dcbor.Uint:
		return uint64(val), nil
	case dcbor.Neg:
		if i, ok := val.Int64(); ok {
			return i, nil
		}
		// -1 - n, for n above math.MaxInt64.
		bi := new(big.Int).SetUint64(uint64(val))
		bi.Neg(bi)
		bi.Sub(bi, big.NewInt(1))
		return *bi, nil
	case dcbor.Decimal:
		return cbor.Tag{
			Number:  tagDecimalFraction,
			Content: []any{intToOracle(val.Exponent), intToOracle(val.Mantissa)},
		}, nil
	case dcbor.Text:
		return string(val), nil
	case dcbor.Bytes:
		return []byte(val), nil
	case dcbor.Array:
		items := make([]any, 0, len(val))
		for _, item := range val {
			converted, err := ToOracle(item)
			if err != nil {
				return nil, err
			}
			items = append(items, converted)
		}
		return items, nil
	case dcbor.Map:
		// A map[any]any loses both order and duplicates. Neither is a loss
		// here: Dialog's canonical order is a function of the keys, so the
		// oracle's encoder recomputes it, and dcbor.Encode rejects a duplicate
		// key outright, so a Map that survives to be compared has none. The
		// key type is any rather than string because that is what the oracle's
		// decoder produces, and the decode target compares the two trees.
		m := make(map[any]any, len(val))
		for _, e := range val {
			converted, err := ToOracle(e.Value)
			if err != nil {
				return nil, err
			}
			if _, dup := m[e.Key]; dup {
				return nil, fmt.Errorf("difftest: duplicate map key %q", e.Key)
			}
			m[e.Key] = converted
		}
		return m, nil
	case dcbor.NullValue:
		return nil, nil //nolint:nilnil // CBOR null is the nil empty interface.
	case nil:
		return nil, fmt.Errorf("difftest: nil dcbor.Value")
	default:
		return nil, fmt.Errorf("difftest: %T is not a dcbor.Value", v)
	}
}

// intToOracle splits an int64 the way the oracle's decoder does with
// IntDecConvertNone: major type 0 becomes uint64, major type 1 becomes int64.
func intToOracle(i int64) any {
	if i < 0 {
		return i
	}
	//nolint:gosec // G115: i >= 0 here.
	return uint64(i)
}

// OracleEqual reports whether two oracle-side values are the same CBOR value.
//
// It exists because reflect.DeepEqual is not quite the right relation: big.Int
// carries an internal representation that DeepEqual compares field by field,
// and a []byte and a cbor.ByteString holding the same bytes are the same CBOR
// item written two ways. Everything else is compared structurally.
func OracleEqual(a, b any) bool {
	switch av := a.(type) {
	case nil:
		return b == nil
	case uint64:
		bv, ok := b.(uint64)
		return ok && av == bv
	case int64:
		bv, ok := b.(int64)
		return ok && av == bv
	case big.Int:
		bv, ok := b.(big.Int)
		return ok && av.Cmp(&bv) == 0
	case float64:
		bv, ok := b.(float64)
		// Bit equality, not ==: the oracle rejects NaN on decode, so the only
		// float that reaches here is finite, and for a finite float the two
		// agree.
		return ok && math.Float64bits(av) == math.Float64bits(bv)
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case cbor.ByteString:
		return equalBytes([]byte(av), b)
	case []byte:
		return equalBytes(av, b)
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !OracleEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	case map[any]any:
		bv, ok := b.(map[any]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			other, found := bv[k]
			if !found || !OracleEqual(v, other) {
				return false
			}
		}
		return true
	case cbor.Tag:
		bv, ok := b.(cbor.Tag)
		return ok && av.Number == bv.Number && OracleEqual(av.Content, bv.Content)
	default:
		return false
	}
}

// equalBytes compares a byte string against either Go spelling of one.
func equalBytes(a []byte, b any) bool {
	switch bv := b.(type) {
	case []byte:
		return bytes.Equal(a, bv)
	case cbor.ByteString:
		return bytes.Equal(a, []byte(bv))
	default:
		return false
	}
}

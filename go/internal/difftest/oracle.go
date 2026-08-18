package difftest

import (
	"bytes"
	"fmt"
	"math"
	"sync"

	"github.com/fxamacker/cbor/v2"
)

// OracleModule and OracleVersion name the implementation under comparison.
// They are constants so that a failure report can say which oracle produced
// it: an option's meaning is a property of a version, and a bump is a reason
// to re-read this file rather than to assume the configuration still says what
// it said.
const (
	OracleModule  = "github.com/fxamacker/cbor/v2"
	OracleVersion = "v2.9.3"
)

// simpleValueNull is the only simple value Dialog's profile admits
// (spec/03-encoding.md, "Deterministic CBOR" rule 7).
const simpleValueNull = 22

// reservedSimpleValueLo and reservedSimpleValueHi bound the simple values RFC
// 8949 reserves (24..31). They are not well-formed as simple values at all, so
// the oracle refuses to register a behaviour for them and there is nothing to
// reject.
const (
	reservedSimpleValueLo = 24
	reservedSimpleValueHi = 31
)

// oracleMaxNestedLevels is the largest value fxamacker/cbor accepts for
// DecOptions.MaxNestedLevels. It is set deliberately to the maximum rather
// than to Dialog's MaxDepth: the point of the comparison is to *find* the
// depth divergence and classify it (see [DivergenceDepth]), not to hide it by
// giving the oracle Dialog's bound.
const oracleMaxNestedLevels = 65535

// oracleMaxElements is the largest value fxamacker/cbor accepts for
// MaxArrayElements and MaxMapPairs. Dialog's decoder has no such limit — it
// bounds a count by the bytes remaining in the input instead — so leaving the
// oracle's defaults (131072) in place would manufacture a divergence that is
// about the oracle's resource policy and not about either profile. The
// oracle validates well-formedness over the whole buffer before allocating,
// so a hostile count cannot turn into a hostile allocation here.
const oracleMaxElements = math.MaxInt32

// An Oracle is the independent CBOR implementation, in the strictest
// configuration its options allow.
type Oracle struct {
	// Enc is RFC 8949 §4.2.1 Core Deterministic Encoding.
	Enc cbor.EncMode
	// Dec is the decoder. Its options carry every Dialog rule fxamacker has a
	// knob for; the rest is carried by the canonicity round-trip in
	// [Oracle.Decode].
	Dec cbor.DecMode
}

// TheOracle returns the process-wide oracle, building it once.
//
// It panics rather than returning an error: the configuration is a set of
// constants in this file, so a failure to build it is a programming error in
// this package and not a condition any caller could handle.
func TheOracle() *Oracle {
	o, err := loadOracle()
	if err != nil {
		panic(fmt.Sprintf("difftest: building the %s %s oracle: %v", OracleModule, OracleVersion, err))
	}
	return o
}

var loadOracle = sync.OnceValues(newOracle)

func newOracle() (*Oracle, error) {
	enc, err := encOptions().EncMode()
	if err != nil {
		return nil, fmt.Errorf("encode options: %w", err)
	}
	opts, err := decOptions()
	if err != nil {
		return nil, err
	}
	dec, err := opts.DecMode()
	if err != nil {
		return nil, fmt.Errorf("decode options: %w", err)
	}
	return &Oracle{Enc: enc, Dec: dec}, nil
}

// encOptions is RFC 8949 §4.2.1 Core Deterministic Encoding, which is the half
// of Dialog's profile that is not a restriction: shortest-form arguments,
// definite lengths, and map keys sorted by the bytewise lexicographic order of
// their encodings — SortCoreDeterministic, the order spec/03-encoding.md rule 2
// requires.
//
// SortCanonical, the other candidate, is RFC 7049's length-first order, and it
// is worth recording that it would have made no difference. A CBOR text head is
// monotonic in the string's length — 0x60+n for n below 24, then 0x78, 0x79,
// 0x7a with the length behind it — so for two text keys the bytewise comparison
// decides on length before it ever reaches the content, which is what
// length-first does explicitly. The two orders coincide on every text-keyed map,
// and Dialog's rule 9 says every Dialog map is text-keyed. The choice here is
// therefore about saying the right thing, not about the bytes: no input can
// distinguish the two settings, and no test in this module claims to.
func encOptions() cbor.EncOptions {
	opts := cbor.CoreDetEncOptions()

	// Tag 4 is inside Dialog's profile, so the encoder has to be able to emit
	// a tag. TagsMd is all-or-nothing in both directions; see
	// [DivergenceTagOtherThanFour] for what that costs on the decode side.
	opts.TagsMd = cbor.TagsAllowed

	// A negative integer below math.MinInt64 has no Go native type, so the
	// converter hands the oracle a big.Int for it. Shortest makes the oracle
	// encode it as major type 1 with a 64-bit argument — which is what Dialog
	// does — rather than as a bignum (tag 3), which Dialog's rule 6 forbids.
	opts.BigIntConvert = cbor.BigIntConvertShortest

	// The converter never produces a nil slice or map, but the default
	// (NilContainerAsNull) would silently turn one into f6, which is a
	// different value rather than a different encoding of the same value.
	// Asking for the empty container removes the ambiguity outright.
	opts.NilContainers = cbor.NilContainerAsEmpty

	return opts
}

// decOptions is the strictest decoder fxamacker/cbor can be configured into
// for Dialog's purposes. Each field is a Dialog rule the oracle happens to
// have a knob for; the rules it has no knob for are the allowlist in
// divergence.go.
func decOptions() (cbor.DecOptions, error) {
	// Rule 7: null is the only simple value. The registry is the one place
	// where an option expresses a Dialog restriction exactly — false, true,
	// undefined and all 244 unassigned simple values are rejected, and null
	// is not. The reserved range 24..31 is not well-formed as a simple value,
	// so the registry refuses to hold an entry for it and the well-formedness
	// check rejects it first.
	rejects := make([]func(*cbor.SimpleValueRegistry) error, 0, 247)
	for sv := 0; sv <= math.MaxUint8; sv++ {
		if sv == simpleValueNull || (sv >= reservedSimpleValueLo && sv <= reservedSimpleValueHi) {
			continue
		}
		//nolint:gosec // G115: the loop bound is math.MaxUint8.
		rejects = append(rejects, cbor.WithRejectedSimpleValue(cbor.SimpleValue(sv)))
	}
	registry, err := cbor.NewSimpleValueRegistryFromDefaults(rejects...)
	if err != nil {
		return cbor.DecOptions{}, fmt.Errorf("simple value registry: %w", err)
	}

	return cbor.DecOptions{
		// Rule 3: no duplicate map keys. APF is the enforcing mode.
		DupMapKey: cbor.DupMapKeyEnforcedAPF,
		// Rule 4: no indefinite-length items.
		IndefLength: cbor.IndefLengthForbidden,
		// Rule 6, as far as a knob reaches: tags must be allowed for tag 4 to
		// decode at all. Everything else about rule 6 is classification.
		TagsMd: cbor.TagsAllowed,
		// Integers decode the way Dialog models them: major type 0 to uint64
		// over the whole range, major type 1 to int64 where it fits and to
		// big.Int below math.MinInt64 — which is Dialog's Neg, whose range
		// runs to -2^64.
		IntDec:    cbor.IntDecConvertNone,
		BigIntDec: cbor.BigIntDecodeValue,
		// Rule 6 again, from the other side: tags 2 and 3 (bignum) are the
		// only tags besides 0, 1 and 4 the oracle understands natively, and
		// forbidding them makes the oracle agree with Dialog outright instead
		// of turning them into a divergence class.
		BignumTag: cbor.BignumTagForbidden,
		// "Text strings and Unicode": text must be well-formed UTF-8.
		UTF8: cbor.UTF8RejectInvalid,
		// Rule 5, as far as a knob reaches. There is no option that forbids a
		// finite float; see [DivergenceFloat].
		NaN: cbor.NaNDecodeForbidden,
		Inf: cbor.InfDecodeForbidden,
		// Rule 7, exactly.
		SimpleValues: registry,
		// See the constants above.
		MaxNestedLevels:  oracleMaxNestedLevels,
		MaxArrayElements: oracleMaxElements,
		MaxMapPairs:      oracleMaxElements,
	}, nil
}

// Encode returns the oracle's Core Deterministic encoding of an oracle-side
// value — one built by [ToOracle] or returned by [Oracle.Decode].
func (o *Oracle) Encode(v any) ([]byte, error) {
	return o.Enc.Marshal(v)
}

// A Verdict is the oracle's answer about a byte string.
type Verdict struct {
	// Accepted reports whether the oracle both decoded the input and agreed
	// that it is the deterministic encoding of what it decoded.
	Accepted bool
	// Value is the decoded value when Accepted; nil otherwise. Its Go type is
	// the one fxamacker/cbor produces for an empty-interface destination:
	// uint64, int64, big.Int, float64, string, []byte, cbor.ByteString (a map
	// key that was a byte string), []any, map[any]any, cbor.Tag, or nil for
	// CBOR null.
	Value any
	// Reason says why the input was not accepted. It is prose for a failure
	// message, never something to branch on.
	Reason string
}

// Decode is the oracle's accept/reject decision on b.
//
// Acceptance is decoding *plus* a canonicity round-trip: the decoded value is
// re-encoded under Core Deterministic and the result must equal b byte for
// byte. The round-trip is what makes the oracle strict about the half of RFC
// 8949 §4.2.1 its decoder does not check — non-shortest arguments, map keys
// out of order — and it is why a rejection reason may say "not canonical"
// where a hand-written decoder would have named the specific rule.
//
// A value that decodes but cannot be re-encoded at all counts as rejected for
// the same reason: the oracle has no deterministic encoding for it, so it
// cannot be a deterministic encoding of it either. CBOR tags 0 and 1 land
// here — they decode to time.Time and re-encode as a bare integer — which is
// why the tag divergence class never sees them.
func (o *Oracle) Decode(b []byte) Verdict {
	var v any
	if err := o.Dec.Unmarshal(b, &v); err != nil {
		return Verdict{Reason: err.Error()}
	}
	round, err := o.Enc.Marshal(v)
	if err != nil {
		return Verdict{Reason: fmt.Sprintf("decoded to %T, which has no deterministic encoding: %v", v, err)}
	}
	if !bytes.Equal(round, b) {
		return Verdict{Reason: fmt.Sprintf("not the deterministic encoding of what it decodes to: re-encodes as %x", round)}
	}
	return Verdict{Accepted: true, Value: v}
}

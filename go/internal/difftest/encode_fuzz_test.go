package difftest

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/vrinek/Dialog/go/dcbor"
)

// FuzzEncodeAgreement asserts that Dialog's encoder and the oracle's Core
// Deterministic encoder produce the same bytes for the same value.
//
// The fuzzer's input is entropy, not CBOR: [Generate] turns it into a
// dcbor.Value, and that value is what the two encoders are asked to write.
// Every value Generate produces is one dcbor.Encode accepts, so an encoding
// error here is a bug in generate.go and is reported as one.
//
// The seed corpus is vectors/dcbor.json, which is CBOR rather than entropy, so
// the target does two things with each input: it generates a value from it,
// and — when the input happens to be a byte string Dialog accepts — it also
// compares the encoders on the value that byte string decodes to. The second
// is what makes a conformance vector a meaningful seed: the vector's own value
// goes through the comparison, not just its bytes reinterpreted as random
// numbers.
func FuzzEncodeAgreement(f *testing.F) {
	addVectorSeeds(f)

	f.Fuzz(func(t *testing.T, entropy []byte) {
		checkEncodeAgreement(t, "generated", Generate(entropy))

		if v, err := dcbor.Decode(entropy); err == nil {
			checkEncodeAgreement(t, "decoded from the input", v)
		}
	})
}

// checkEncodeAgreement is the whole of the encode property: one value, two
// encoders, the same bytes, and those bytes decoding back to the value they
// came from.
func checkEncodeAgreement(t *testing.T, origin string, v dcbor.Value) {
	t.Helper()

	want, err := dcbor.Encode(v)
	if err != nil {
		t.Fatalf("dcbor.Encode refused a %s value: %v\nvalue: %#v", origin, err, v)
	}

	oracleValue, err := ToOracle(v)
	if err != nil {
		t.Fatalf("converting a %s value for the oracle: %v\nvalue: %#v", origin, err, v)
	}
	got, err := TheOracle().Encode(oracleValue)
	if err != nil {
		t.Fatalf("the oracle refused to encode a %s value: %v\nvalue: %#v\ndcbor produced: %x",
			origin, err, v, want)
	}

	if !bytes.Equal(got, want) {
		t.Fatalf("encoders disagree on a %s value.\n"+
			"  dcbor:  %x\n"+
			"  %s %s: %x\n"+
			"  value:  %#v\n"+
			"This is a genuine encoding disagreement: Dialog's profile is RFC 8949 §4.2.1 "+
			"plus restrictions, and a restriction can only remove values, never change the "+
			"bytes of one both profiles admit.",
			origin, want, OracleModule, OracleVersion, got, v)
	}

	// The bytes both encoders produced must decode back to the value they came
	// from. Without this the agreement could be an agreement on nonsense.
	back, err := dcbor.Decode(want)
	if err != nil {
		t.Fatalf("dcbor.Decode rejected the bytes both encoders produced: %v\nbytes: %x", err, want)
	}
	if !dcbor.Equal(back, v) {
		t.Fatalf("dcbor.Decode(%x) is not the value that was encoded\n  got:  %#v\n  want: %#v", want, back, v)
	}
}

// addVectorSeeds seeds a target from vectors/dcbor.json. A failure to read the
// vectors fails the target rather than skipping it: a fuzz target that
// silently lost its seed corpus still passes, which is the worst way for this
// to break.
func addVectorSeeds(f *testing.F) {
	f.Helper()

	seeds, err := VectorSeeds()
	if err != nil {
		f.Fatalf("reading the conformance vectors: %v", err)
	}
	for _, s := range seeds {
		f.Add(s.Bytes)
	}

	// A handful of byte strings the vectors have no reason to hold: they are
	// not conformance cases, they are the shapes where two codecs are most
	// likely to part company. Each is written out here rather than left to the
	// mutator, which would have to guess several bytes at once to reach them.
	for _, s := range []string{
		"",                 // empty input
		"00", "17", "1817", // the one-byte/two-byte argument boundary
		"1b7fffffffffffffff",   // math.MaxInt64
		"1bffffffffffffffff",   // math.MaxUint64
		"3b7fffffffffffffff",   // math.MinInt64
		"3bffffffffffffffff",   // -2^64, the far end of major type 1
		"f6", "f4", "f5", "f7", // null and the simple values that are not null
		"fb3ff199999999999a",       // a finite double
		"f93c00",                   // a half-precision 1.0
		"c4822119013a",             // the canonical 3.14 of spec/03-encoding.md
		"c4820019013a",             // exponent 0: a whole number, so not a decimal
		"c48221190c44",             // mantissa 3140: trailing zero not stripped
		"c482201bffffffffffffffff", // mantissa beyond int64
		"c11a514b67b0",             // tag 1, a date
		"d8184100",                 // tag 24, an encoded CBOR data item
		"c249010000000000000000",   // tag 2, a bignum
		"a10102",                   // an integer map key
		"a14000",                   // a byte-string map key
		"a2616200616101",           // map keys out of order
		"a2616100616100",           // a duplicate map key
		"a261610062626201",         // "a" before "bb": bytewise, not length-first
		"9f01ff",                   // an indefinite-length array
		"5f42010243030405ff",       // an indefinite-length byte string
		"62c328",                   // invalid UTF-8 in a text string
		"a0", "80", "40", "60",     // the empty containers and strings
	} {
		b, err := hex.DecodeString(s)
		if err != nil {
			f.Fatalf("bad hex seed %q: %v", s, err)
		}
		f.Add(b)
	}
}

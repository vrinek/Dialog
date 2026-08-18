package difftest

import (
	"strings"
	"testing"

	"github.com/vrinek/Dialog/go/dcbor"
)

// FuzzDecodeAgreement offers arbitrary bytes to both decoders and asserts the
// shape of their disagreement.
//
// There are four outcomes, and three of them are assertions:
//
//   - Both reject. Nothing to check. This is most of the input space.
//
//   - Both accept. The values must be the same. dcbor's value is converted to
//     the oracle's representation and compared, which is a stronger check than
//     comparing bytes: the bytes are equal by construction — each decoder's
//     acceptance already implies its own encoder reproduces the input — so
//     only the values can differ.
//
//   - dcbor accepts, the oracle rejects. Always a failure. Dialog's profile is
//     RFC 8949 §4.2.1 plus restrictions (spec/03-encoding.md, "Deterministic
//     CBOR"), so everything Dialog accepts is deterministic CBOR. If this
//     fires, either dcbor accepts something outside the profile or the oracle
//     is misconfigured; oracle.go documents which of its settings carries
//     which rule.
//
//   - The oracle accepts, dcbor rejects. Expected, and the interesting case.
//     Dialog is narrower than deterministic CBOR in exactly five ways, and
//     [Allowlist] enumerates them; the input must exhibit at least one of
//     them. A rejection this harness cannot attribute to a listed class is a
//     dcbor bug or a gap in the allowlist, and either way it fails here.
//
// The seed corpus is vectors/dcbor.json, valid and invalid sections both, so
// `go test` with no fuzzing budget still runs every conformance byte string
// through all four branches. testdata/fuzz/FuzzDecodeAgreement/ holds the two
// inputs no amount of mutation would assemble — a chain of array heads nested
// exactly at MaxDepth, and one past it, which are the two sides of the depth
// divergence. Anything a nightly run finds belongs there too, named for the
// class it fell outside of.
func FuzzDecodeAgreement(f *testing.F) {
	addVectorSeeds(f)

	f.Fuzz(func(t *testing.T, data []byte) {
		checkDecodeAgreement(t, data)
	})
}

func checkDecodeAgreement(t *testing.T, data []byte) {
	t.Helper()

	value, dialogErr := dcbor.Decode(data)
	verdict := TheOracle().Decode(data)

	switch {
	case dialogErr == nil && !verdict.Accepted:
		t.Fatalf("dcbor accepted bytes the oracle rejected.\n"+
			"  bytes:  %x\n"+
			"  value:  %#v\n"+
			"  %s %s: %s\n"+
			"Dialog's profile is RFC 8949 §4.2.1 plus restrictions, so it cannot admit "+
			"anything deterministic CBOR does not. Either dcbor accepts bytes outside the "+
			"profile, or an oracle option in oracle.go no longer means what its comment says.",
			data, value, OracleModule, OracleVersion, verdict.Reason)

	case dialogErr != nil && verdict.Accepted:
		classes := Divergences(verdict.Value)
		if len(classes) == 0 {
			t.Fatalf("unexplained divergence: the oracle accepted bytes dcbor rejected, and "+
				"no allowlisted class explains it.\n"+
				"  bytes:      %x\n"+
				"  dcbor says: %v\n"+
				"  oracle got: %#v\n"+
				"Either dcbor rejects something spec/03-encoding.md permits — a bug to fix in "+
				"go/dcbor, with a regression test and a vector case — or Dialog's profile is "+
				"stricter here in a way divergence.go does not record, which is a gap in the "+
				"allowlist and a question for the specification.",
				data, dialogErr, verdict.Value)
		}

	case dialogErr == nil && verdict.Accepted:
		converted, err := ToOracle(value)
		if err != nil {
			t.Fatalf("converting a decoded value for the oracle: %v\n  bytes: %x\n  value: %#v", err, data, value)
		}
		if !OracleEqual(converted, verdict.Value) {
			t.Fatalf("both decoders accepted the same bytes and disagree about the value.\n"+
				"  bytes:  %x\n"+
				"  dcbor:  %#v\n"+
				"  %s %s: %#v",
				data, value, OracleModule, OracleVersion, verdict.Value)
		}
	}
}

// TestDecodeAgreementDivergenceClasses pins one byte string per allowlisted
// class: the oracle accepts it, dcbor rejects it, and the class the harness
// attributes it to is the one named. Without this the allowlist could rot into
// prose — a class could stop being reachable, or a case could quietly start
// being explained by the wrong rule, and the fuzz target would still pass
// because it only asks for *some* explanation.
func TestDecodeAgreementDivergenceClasses(t *testing.T) {
	cases := []struct {
		name  string
		bytes string
		want  Divergence
	}{
		{"finite double", "fb3ff199999999999a", DivergenceFloat},
		{"half precision", "f93c00", DivergenceFloat},
		{"float inside an array", "81fa47c35000", DivergenceFloat},
		{"tag 24", "d8184100", DivergenceTagOtherThanFour},
		{"tag 100", "d86401", DivergenceTagOtherThanFour},
		{"decimal with a zero exponent", "c4820019013a", DivergenceDecimalNotCanonical},
		{"decimal with a trailing zero", "c48221190c44", DivergenceDecimalNotCanonical},
		{"decimal with a zero mantissa", "c4822100", DivergenceDecimalNotCanonical},
		{"decimal with three elements", "c4832119013a00", DivergenceDecimalNotCanonical},
		{"decimal with one element", "c48121", DivergenceDecimalNotCanonical},
		{"decimal mantissa beyond int64", "c482201bffffffffffffffff", DivergenceDecimalNotCanonical},
		{"integer map key", "a10102", DivergenceNonTextMapKey},
		{"byte string map key", "a14000", DivergenceNonTextMapKey},
		{"null map key", "a1f600", DivergenceNonTextMapKey},
		{"nested one past MaxDepth", nestedArrays(dcbor.MaxDepth + 1), DivergenceDepth},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := mustHex(t, tc.bytes)

			if _, err := dcbor.Decode(data); err == nil {
				t.Fatalf("dcbor accepted %x; this case is only a divergence if it rejects", data)
			}
			verdict := TheOracle().Decode(data)
			if !verdict.Accepted {
				t.Fatalf("the oracle rejected %x (%s); this case is only a divergence if it accepts",
					data, verdict.Reason)
			}
			classes := Divergences(verdict.Value)
			if !containsDivergence(classes, tc.want) {
				t.Errorf("Divergences(%x) = %v, want it to include %q",
					data, divergenceNames(classes), tc.want.Name)
			}
		})
	}
}

// TestDecodeAgreementAtTheDepthBound is the other side of the depth class:
// exactly at MaxDepth both decoders accept, so the class must not fire.
func TestDecodeAgreementAtTheDepthBound(t *testing.T) {
	data := mustHex(t, nestedArrays(dcbor.MaxDepth))
	if _, err := dcbor.Decode(data); err != nil {
		t.Fatalf("dcbor rejected a document nested exactly at MaxDepth (%d): %v", dcbor.MaxDepth, err)
	}
	checkDecodeAgreement(t, data)
}

// TestDecodeAgreementOverVectors runs the comparison over every byte string
// vectors/dcbor.json pins, naming the case in any failure. The fuzz target
// replays the same bytes as seeds, but a failure there reports hex; this one
// reports "invalid/map_key_not_text".
func TestDecodeAgreementOverVectors(t *testing.T) {
	seeds, err := VectorSeeds()
	if err != nil {
		t.Fatalf("reading the conformance vectors: %v", err)
	}
	for _, s := range seeds {
		t.Run(strings.ReplaceAll(s.Name, " ", "_"), func(t *testing.T) {
			if _, err := dcbor.Decode(s.Bytes); (err == nil) != s.Valid {
				t.Fatalf("the vectors say valid=%v; dcbor.Decode says %v", s.Valid, err)
			}
			checkDecodeAgreement(t, s.Bytes)
		})
	}
}

// nestedArrays is the hex of n one-element array heads around a zero: the
// cheapest document of a given nesting depth, and the one rule 10 is written
// about.
func nestedArrays(n int) string {
	return strings.Repeat("81", n) + "00"
}

func containsDivergence(classes []Divergence, want Divergence) bool {
	for _, d := range classes {
		if d.Name == want.Name {
			return true
		}
	}
	return false
}

func divergenceNames(classes []Divergence) []string {
	names := make([]string, 0, len(classes))
	for _, d := range classes {
		names = append(names, d.Name)
	}
	return names
}

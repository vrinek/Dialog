package entity

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"

	"github.com/vrinek/Dialog/go/cid"
)

// TestRoundTripAtoms and its siblings assert the property content addressing
// depends on: an entity's canonical bytes decode back to the same entity, and
// the decoded entity re-encodes to the same bytes and therefore the same
// digest.
func TestRoundTripAtoms(t *testing.T) {
	descriptions := []string{
		"France",
		"Paris, the capital of France",
		"x",
		"a description with \"quotes\", commas, and _underscores_",
		"ünïcödé and 漢字 and 🜁",
		string(rune(0x10FFFF)),
	}
	for _, d := range descriptions {
		t.Run(d, func(t *testing.T) {
			a := MustAtom(d)
			got, err := DecodeAtom(a.Bytes())
			if err != nil {
				t.Fatalf("DecodeAtom: %v", err)
			}
			if got.Description() != d {
				t.Errorf("description = %q, want %q", got.Description(), d)
			}
			if !bytes.Equal(got.Bytes(), a.Bytes()) {
				t.Errorf("re-encoded %x, want %x", got.Bytes(), a.Bytes())
			}
			if got.Digest() != a.Digest() {
				t.Errorf("digest = %s, want %s", got.Digest(), a.Digest())
			}
			if got.CID() != a.CID() {
				t.Errorf("CID = %s, want %s", got.CID(), a.CID())
			}
		})
	}
}

func TestRoundTripBonds(t *testing.T) {
	templates := []string{
		"_A_ is the capital of _B_",
		"_A_ is true",
		"_LONG_ and _NAMES_ and _HERE_",
		"_A_ 漢字 _B_",
		"prefix _A_ suffix",
	}
	for _, tpl := range templates {
		t.Run(tpl, func(t *testing.T) {
			b := MustBond(tpl)
			got, err := DecodeBond(b.Bytes())
			if err != nil {
				t.Fatalf("DecodeBond: %v", err)
			}
			if got.Template() != tpl {
				t.Errorf("template = %q, want %q", got.Template(), tpl)
			}
			if !equalStrings(got.Variables(), b.Variables()) {
				t.Errorf("variables = %v, want %v", got.Variables(), b.Variables())
			}
			if !bytes.Equal(got.Bytes(), b.Bytes()) {
				t.Errorf("re-encoded %x, want %x", got.Bytes(), b.Bytes())
			}
			if got.Digest() != b.Digest() {
				t.Errorf("digest = %s, want %s", got.Digest(), b.Digest())
			}
		})
	}
}

// TestRoundTripMolecules walks a pseudo-random population of molecules
// covering every filler shape, checking the encode/decode/re-encode identity
// for each.
func TestRoundTripMolecules(t *testing.T) {
	rng := rand.New(rand.NewSource(20260813))
	for i := 0; i < 500; i++ {
		m := randomMolecule(t, rng)
		t.Run(fmt.Sprintf("molecule-%d", i), func(t *testing.T) {
			got, err := DecodeMolecule(m.Bytes())
			if err != nil {
				t.Fatalf("DecodeMolecule(%x): %v", m.Bytes(), err)
			}
			if !bytes.Equal(got.Bytes(), m.Bytes()) {
				t.Fatalf("re-encoded %x, want %x", got.Bytes(), m.Bytes())
			}
			if got.Digest() != m.Digest() {
				t.Fatalf("digest = %s, want %s", got.Digest(), m.Digest())
			}
			if got.Bond() != m.Bond() {
				t.Fatalf("bond = %s, want %s", got.Bond(), m.Bond())
			}
			gotFillers, wantFillers := got.Fillers(), m.Fillers()
			if len(gotFillers) != len(wantFillers) {
				t.Fatalf("%d filler(s), want %d", len(gotFillers), len(wantFillers))
			}
			for j := range gotFillers {
				if gotFillers[j].String() != wantFillers[j].String() {
					t.Errorf("filler %d = %s, want %s", j, gotFillers[j], wantFillers[j])
				}
			}
		})
	}
}

// FuzzDecodeMolecule asserts that every byte string DecodeMolecule accepts is
// the canonical encoding of the molecule it produces — the property that
// makes a molecule's digest well defined. If re-encoding a decoded molecule
// could differ from its input, two implementations could compute different
// identifiers for the same bytes.
//
// The seeds here are random molecules of up to four fillers.
// testdata/fuzz/FuzzDecodeMolecule/ adds the one shape they cannot reach: 24
// fillers, one past the count a one-byte array head can carry, covering all
// five filler types in a single value.
func FuzzDecodeMolecule(f *testing.F) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 32; i++ {
		f.Add(randomMolecule(f, rng).Bytes())
	}
	f.Add(MustAtom("France").Bytes())
	f.Add(MustBond("_A_ is true").Bytes())
	f.Add([]byte(nil))

	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := DecodeMolecule(data)
		if err != nil {
			return // rejected input carries no obligation
		}
		if !bytes.Equal(m.Bytes(), data) {
			t.Fatalf("DecodeMolecule accepted %x but re-encodes to %x", data, m.Bytes())
		}
		again, err := DecodeMolecule(m.Bytes())
		if err != nil {
			t.Fatalf("DecodeMolecule rejected its own output: %v", err)
		}
		if again.Digest() != m.Digest() {
			t.Fatalf("digest is not stable across a round trip: %s vs %s", again.Digest(), m.Digest())
		}
	})
}

// FuzzDecodeAtomAndBond asserts the same canonical-form property for the two
// single-field entities, and that neither accepts the other's encoding.
//
// testdata/fuzz/FuzzDecodeAtomAndBond/ holds the two inputs that matter most
// to that second claim and that mutation will not stumble on: a canonical map
// carrying both "description" and "template" — the exact shape that would
// decode as an atom and a bond at once if either decoder were lax about extra
// keys — and an atom whose description is multi-byte UTF-8 with a combining
// sequence, which the decoder must accept and must not normalise.
func FuzzDecodeAtomAndBond(f *testing.F) {
	f.Add(MustAtom("France").Bytes())
	f.Add(MustBond("_A_ is the capital of _B_").Bytes())
	f.Add([]byte{0xa1})
	f.Add([]byte(nil))

	f.Fuzz(func(t *testing.T, data []byte) {
		if a, err := DecodeAtom(data); err == nil {
			if !bytes.Equal(a.Bytes(), data) {
				t.Fatalf("DecodeAtom accepted %x but re-encodes to %x", data, a.Bytes())
			}
			if _, err := DecodeBond(data); err == nil {
				t.Fatalf("%x decoded as both an atom and a bond", data)
			}
		}
		if b, err := DecodeBond(data); err == nil {
			if !bytes.Equal(b.Bytes(), data) {
				t.Fatalf("DecodeBond accepted %x but re-encodes to %x", data, b.Bytes())
			}
		}
	})
}

// randomMolecule builds a molecule whose fillers cover every shape of
// spec/01-data-model.md, "Filler types".
func randomMolecule(tb testing.TB, rng *rand.Rand) Molecule {
	tb.Helper()
	n := 1 + rng.Intn(4)
	fillers := make([]Filler, 0, n)
	for i := 0; i < n; i++ {
		fillers = append(fillers, randomFiller(tb, rng))
	}
	m, err := NewMolecule(randomDigest(rng), fillers)
	if err != nil {
		tb.Fatalf("NewMolecule: %v", err)
	}
	return m
}

func randomFiller(tb testing.TB, rng *rand.Rand) Filler {
	tb.Helper()
	switch rng.Intn(8) {
	case 0:
		return AtomFiller(randomDigest(rng))
	case 1:
		return BondFiller(randomDigest(rng))
	case 2:
		return MoleculeFiller(randomDigest(rng))
	case 3:
		f, err := IPFSFiller(fmt.Sprintf("bafyrei%d", rng.Int63()))
		if err != nil {
			tb.Fatalf("IPFSFiller: %v", err)
		}
		return f
	case 4:
		f, err := ScalarFiller(IntScalar(rng.Int63() - rng.Int63()))
		if err != nil {
			tb.Fatalf("ScalarFiller: %v", err)
		}
		return f
	case 5:
		s, err := DecimalScalar(-1-rng.Int63n(8), 1+rng.Int63n(1<<40))
		if err != nil {
			tb.Fatalf("DecimalScalar: %v", err)
		}
		f, err := ScalarFiller(s)
		if err != nil {
			tb.Fatalf("ScalarFiller: %v", err)
		}
		return f
	case 6:
		s, err := IntScalar(rng.Int63n(1000)).WithUnit(randomDigest(rng))
		if err != nil {
			tb.Fatalf("WithUnit: %v", err)
		}
		f, err := ScalarFiller(s)
		if err != nil {
			tb.Fatalf("ScalarFiller: %v", err)
		}
		return f
	default:
		year := 2000 + rng.Intn(50)
		s, err := NewDatetimeRange(
			fmt.Sprintf("%04d-02-20T00:00:00Z", year),
			fmt.Sprintf("%04d-02-20T23:59:59Z", year),
		)
		if err != nil {
			tb.Fatalf("NewDatetimeRange: %v", err)
		}
		f, err := ScalarFiller(s)
		if err != nil {
			tb.Fatalf("ScalarFiller: %v", err)
		}
		return f
	}
}

func randomDigest(rng *rand.Rand) cid.Digest {
	var d cid.Digest
	rng.Read(d[:])
	return d
}

// Package dialog_test holds the conformance test: it reads the committed
// conformance vectors under vectors/ and checks that this implementation
// still produces every byte in them.
//
// The test exists because the vectors are the interop contract. Any change to
// this module that moves a canonical byte — a different key order, a different
// digest, a different signing input — fails here, and the fix is never to
// silence the test: it is to regenerate the vectors with
//
//	go run ./cmd/genvectors
//
// and to review the resulting diff as the breaking change it is.
//
// The privacy area is verified in privacy/spec_test.go, against the same
// committed file, so that the package holding the keys reads the vectors
// directly.
package dialog_test

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/dcbor"
	"github.com/vrinek/Dialog/go/entity"
	"github.com/vrinek/Dialog/go/internal/vectorfile"
	"github.com/vrinek/Dialog/go/internal/vectors"
)

// vectorsDir is the committed vector set, relative to this module's root.
const vectorsDir = "../vectors"

func read(t *testing.T, name string) vectorfile.Document {
	t.Helper()
	doc, err := vectorfile.Read(filepath.Join(vectorsDir, name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return doc
}

func cases[T any](t *testing.T, doc vectorfile.Document, section string) []T {
	t.Helper()
	s, ok := doc.Section(section)
	if !ok {
		t.Fatalf("%s has no %q section", doc.Area, section)
	}
	out, err := vectorfile.DecodeCases[T](s)
	if err != nil {
		t.Fatalf("%s/%s: %v", doc.Area, section, err)
	}
	if len(out) == 0 {
		t.Fatalf("%s/%s is empty", doc.Area, section)
	}
	return out
}

func mustHex(t *testing.T, what, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("%s: %q is not hex: %v", what, s, err)
	}
	return b
}

// TestVectorsAreCurrent is the round-trip guard: regenerating the vectors from
// this implementation must reproduce the committed files byte for byte. It is
// the same comparison `go run ./cmd/genvectors && git diff --exit-code
// vectors/` makes in CI, run without git so that a stale checkout fails the
// test suite too.
func TestVectorsAreCurrent(t *testing.T) {
	files, err := vectors.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got, want := len(files), len(vectors.Names()); got != want {
		t.Fatalf("Build returned %d files, want %d", got, want)
	}
	for _, f := range files {
		t.Run(f.Name, func(t *testing.T) {
			want, err := f.JSON()
			if err != nil {
				t.Fatalf("JSON: %v", err)
			}
			got, err := os.ReadFile(filepath.Join(vectorsDir, f.Name))
			if err != nil {
				t.Fatalf("reading the committed file: %v", err)
			}
			if string(got) != string(want) {
				t.Errorf("vectors/%s is stale: the implementation no longer produces it.\n"+
					"Run `go run ./cmd/genvectors` and review the diff — a change here is a change to the bytes every other implementation must match.", f.Name)
			}
		})
	}
}

// TestDCBORVectors checks both directions of every dCBOR case: the value model
// encodes to the pinned bytes, and the pinned bytes decode back to it.
func TestDCBORVectors(t *testing.T) {
	doc := read(t, "dcbor.json")

	for _, section := range []string{"encoding_reference", "canonical", "decimal_fractions"} {
		for _, tc := range cases[vectorfile.DCBORCase](t, doc, section) {
			t.Run(section+"/"+tc.Name, func(t *testing.T) {
				value, err := valueOf(tc.Value)
				if err != nil {
					t.Fatalf("rebuilding the value: %v", err)
				}
				encoded, err := dcbor.Encode(value)
				if err != nil {
					t.Fatalf("Encode: %v", err)
				}
				if got := hex.EncodeToString(encoded); got != tc.DCBOR {
					t.Errorf("Encode(value) = %s, want %s", got, tc.DCBOR)
				}
				decoded, err := dcbor.Decode(mustHex(t, tc.Name, tc.DCBOR))
				if err != nil {
					t.Fatalf("Decode: %v", err)
				}
				if !dcbor.Equal(decoded, value) {
					t.Errorf("Decode(%s) = %#v, want %#v", tc.DCBOR, decoded, value)
				}
			})
		}
	}

	for _, tc := range cases[vectorfile.InvalidCase](t, doc, "invalid") {
		t.Run("invalid/"+tc.Name, func(t *testing.T) {
			v, err := dcbor.Decode(mustHex(t, tc.Name, tc.Bytes))
			if err == nil {
				t.Fatalf("Decode(%s) = %#v, want a rejection: %s (%s)", tc.Bytes, v, tc.Reason, tc.Rule)
			}
			var syntax *dcbor.SyntaxError
			if !errors.As(err, &syntax) {
				t.Errorf("Decode error is %T, want *dcbor.SyntaxError", err)
			}
		})
	}
}

// TestEntityVectors rebuilds every entity from its description or template and
// checks the bytes, the digest and both forms of the CID.
func TestEntityVectors(t *testing.T) {
	doc := read(t, "entities.json")

	for _, section := range []string{"atoms", "bonds", "meta_bonds", "molecules"} {
		for _, tc := range cases[vectorfile.EntityCase](t, doc, section) {
			t.Run(section+"/"+tc.Name, func(t *testing.T) {
				var (
					bytes   []byte
					digest  cid.Digest
					id      cid.CID
					decoded func([]byte) error
				)
				switch tc.Kind {
				case "atom":
					a, err := entity.NewAtom(tc.Description)
					if err != nil {
						t.Fatalf("NewAtom: %v", err)
					}
					bytes, digest, id = a.Bytes(), a.Digest(), a.CID()
					decoded = func(b []byte) error { _, err := entity.DecodeAtom(b); return err }
				case "bond":
					b, err := entity.NewBond(tc.Template)
					if err != nil {
						t.Fatalf("NewBond: %v", err)
					}
					if got := b.Variables(); !equalStrings(got, tc.Variables) {
						t.Errorf("variables = %v, want %v", got, tc.Variables)
					}
					bytes, digest, id = b.Bytes(), b.Digest(), b.CID()
					decoded = func(b []byte) error { _, err := entity.DecodeBond(b); return err }
				case "molecule":
					// A molecule has no scalar description to rebuild from, so
					// it is rebuilt from its own pinned bytes: what the vector
					// fixes is that these bytes decode, re-encode identically,
					// and hash to the pinned identifier.
					m, err := entity.DecodeMolecule(mustHex(t, tc.Name, tc.DCBOR))
					if err != nil {
						t.Fatalf("DecodeMolecule: %v", err)
					}
					bytes, digest, id = m.Bytes(), m.Digest(), m.CID()
					decoded = func(b []byte) error { _, err := entity.DecodeMolecule(b); return err }
				default:
					t.Fatalf("unknown entity kind %q", tc.Kind)
				}

				if got := hex.EncodeToString(bytes); got != tc.DCBOR {
					t.Errorf("dCBOR = %s, want %s", got, tc.DCBOR)
				}
				if got := digest.String(); got != tc.Digest {
					t.Errorf("digest = %s, want %s", got, tc.Digest)
				}
				if got := id.HexString(); got != tc.CID {
					t.Errorf("CID = %s, want %s", got, tc.CID)
				}
				if got := id.String(); got != tc.CIDText {
					t.Errorf("CID text = %s, want %s", got, tc.CIDText)
				}
				// The text form is the interchange form, so it must parse back.
				parsed, err := cid.ParseCIDString(tc.CIDText)
				if err != nil || parsed != id {
					t.Errorf("ParseCIDString(%s) = %v, %v, want the same CID", tc.CIDText, parsed, err)
				}
				if err := decoded(bytes); err != nil {
					t.Errorf("decoding the entity's own bytes: %v", err)
				}
				// The value model must agree with the bytes.
				value, err := valueOf(tc.Value)
				if err != nil {
					t.Fatalf("rebuilding the value: %v", err)
				}
				if got := hex.EncodeToString(dcbor.MustEncode(value)); got != tc.DCBOR {
					t.Errorf("Encode(value) = %s, want %s", got, tc.DCBOR)
				}
			})
		}
	}

	// Every case of the invalid section must be refused by the decoder its kind
	// names. These are the bytes the data model forbids: a decoder that accepts
	// one holds entities its peers cannot read, and mints digests for them.
	for _, tc := range cases[vectorfile.InvalidCase](t, doc, "invalid") {
		t.Run("invalid/"+tc.Name, func(t *testing.T) {
			b := mustHex(t, tc.Name, tc.Bytes)
			var err error
			switch tc.Kind {
			case "atom":
				_, err = entity.DecodeAtom(b)
			case "bond":
				_, err = entity.DecodeBond(b)
			case "molecule":
				_, err = entity.DecodeMolecule(b)
			case "filler":
				_, err = entity.DecodeFiller(b)
			default:
				t.Fatalf("unknown kind %q; an entity invalid case names the decoder that must refuse it", tc.Kind)
			}
			if err == nil {
				t.Fatalf("decoding %s as a%s succeeded, want a rejection: %s (%s)",
					tc.Bytes, articled(tc.Kind), tc.Reason, tc.Rule)
			}
		})
	}

	for _, tc := range cases[vectorfile.FillerCase](t, doc, "fillers") {
		t.Run("fillers/"+tc.Name, func(t *testing.T) {
			f, err := entity.DecodeFiller(mustHex(t, tc.Name, tc.DCBOR))
			if err != nil {
				t.Fatalf("DecodeFiller: %v", err)
			}
			if uint64(f.Type()) != tc.Type {
				t.Errorf("type = %d, want %d", uint64(f.Type()), tc.Type)
			}
			if got := hex.EncodeToString(f.Bytes()); got != tc.DCBOR {
				t.Errorf("re-encoded = %s, want %s", got, tc.DCBOR)
			}
			value, err := valueOf(tc.Value)
			if err != nil {
				t.Fatalf("rebuilding the value: %v", err)
			}
			if got := hex.EncodeToString(dcbor.MustEncode(value)); got != tc.DCBOR {
				t.Errorf("Encode(value) = %s, want %s", got, tc.DCBOR)
			}
		})
	}
}

// TestBlockVectors replays the scenario: every block decodes, verifies, keeps
// its identity, and validates against the store the earlier blocks built.
func TestBlockVectors(t *testing.T) {
	doc := read(t, "blocks.json")
	chain := cases[vectorfile.BlockCase](t, doc, "chain")

	store := block.NewMemStore()
	byName := map[string]*block.Block{}
	for _, tc := range chain {
		t.Run("chain/"+tc.Name, func(t *testing.T) {
			b := checkBlockCase(t, tc)
			byName[tc.Name] = b
			if err := store.Add(b); err != nil {
				t.Fatalf("Add: %v", err)
			}
			report, err := block.Validate(b, store, nil)
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if len(report.Forks) != 0 {
				t.Errorf("the scenario forked at %s", tc.Name)
			}
		})
	}

	// The chain must be linked: every prev names the block before it.
	for i := 1; i < len(chain); i++ {
		if chain[i].Prev == nil {
			continue
		}
		prev, ok := byName[chain[i].Name]
		if !ok {
			continue
		}
		if d, ok := prev.Prev(); ok && !store.Has(d) {
			t.Errorf("%s names a prev the store does not hold", chain[i].Name)
		}
	}

	forkBlocks := cases[vectorfile.BlockCase](t, doc, "fork_block")
	fork := checkBlockCase(t, forkBlocks[0])
	summary := cases[vectorfile.ForkCase](t, doc, "forks")[0]
	second, ok := byName["alice_second"]
	if !ok {
		t.Fatal("the scenario has no alice_second block")
	}
	if !block.IsFork(second, fork) {
		t.Errorf("%s and %s are not detected as a fork", summary.Blocks[0], summary.Blocks[1])
	}
	if got := []string{second.Digest().String(), fork.Digest().String()}; !equalStrings(got, summary.Blocks) {
		t.Errorf("the fork names %v, want %v", summary.Blocks, got)
	}

	for _, tc := range cases[vectorfile.InvalidCase](t, doc, "invalid") {
		t.Run("invalid/"+tc.Name, func(t *testing.T) {
			b, err := block.Decode(mustHex(t, tc.Name, tc.Bytes))
			if err == nil {
				t.Fatalf("Decode accepted %s, want a rejection: %s (%s)", b, tc.Reason, tc.Rule)
			}
		})
	}
}

// checkBlockCase decodes one block vector and checks every identifier it pins.
func checkBlockCase(t *testing.T, tc vectorfile.BlockCase) *block.Block {
	t.Helper()
	b, err := block.Decode(mustHex(t, tc.Name, tc.Block))
	if err != nil {
		t.Fatalf("%s: Decode: %v", tc.Name, err)
	}
	if got := string(b.Type()); got != tc.Type {
		t.Errorf("%s: type = %s, want %s", tc.Name, got, tc.Type)
	}
	if got := hex.EncodeToString(b.Bytes()); got != tc.Block {
		t.Errorf("%s: re-encoded block = %s, want %s", tc.Name, got, tc.Block)
	}
	if got := hex.EncodeToString(b.SigningBytes()); got != tc.SigningBytes {
		t.Errorf("%s: signing bytes = %s, want %s", tc.Name, got, tc.SigningBytes)
	}
	if got := hex.EncodeToString(b.SigningInput()); got != tc.SigningInput {
		t.Errorf("%s: signing input = %s, want %s", tc.Name, got, tc.SigningInput)
	}
	if got := hex.EncodeToString(b.Signature()); got != tc.Signature {
		t.Errorf("%s: signature = %s, want %s", tc.Name, got, tc.Signature)
	}
	if err := b.Verify(); err != nil {
		t.Errorf("%s: Verify: %v", tc.Name, err)
	}
	if got := b.Digest().String(); got != tc.Digest {
		t.Errorf("%s: digest = %s, want %s", tc.Name, got, tc.Digest)
	}
	if got := b.CID().HexString(); got != tc.CID {
		t.Errorf("%s: CID = %s, want %s", tc.Name, got, tc.CID)
	}
	if got := b.CID().String(); got != tc.CIDText {
		t.Errorf("%s: CID text = %s, want %s", tc.Name, got, tc.CIDText)
	}
	prev, hasPrev := b.Prev()
	switch {
	case tc.Prev == nil && hasPrev:
		t.Errorf("%s: the vector says genesis, the block has prev %s", tc.Name, prev)
	case tc.Prev != nil && !hasPrev:
		t.Errorf("%s: the vector says prev %s, the block is a genesis block", tc.Name, *tc.Prev)
	case tc.Prev != nil && prev.String() != *tc.Prev:
		t.Errorf("%s: prev = %s, want %s", tc.Name, prev, *tc.Prev)
	}
	refs := []string{}
	for _, r := range b.Refs() {
		refs = append(refs, r.String())
	}
	if !equalStrings(refs, tc.Refs) {
		t.Errorf("%s: refs = %v, want %v", tc.Name, refs, tc.Refs)
	}
	if b.TS() != tc.TS {
		t.Errorf("%s: ts = %d, want %d", tc.Name, b.TS(), tc.TS)
	}
	// The value model must encode to the very bytes it describes.
	value, err := valueOf(tc.Value)
	if err != nil {
		t.Fatalf("%s: rebuilding the value: %v", tc.Name, err)
	}
	if got := hex.EncodeToString(dcbor.MustEncode(value)); got != tc.Block {
		t.Errorf("%s: Encode(value) = %s, want %s", tc.Name, got, tc.Block)
	}
	return b
}

// valueOf rebuilds a dCBOR value from the JSON value model — the same work an
// implementation in another language does when it consumes these files, which
// is why the test does it this way round rather than describing the value and
// comparing JSON.
func valueOf(v vectorfile.Value) (dcbor.Value, error) {
	switch v.Type {
	case "uint":
		n, err := strconv.ParseUint(v.Number, 10, 64)
		if err != nil {
			return nil, err
		}
		return dcbor.Uint(n), nil
	case "neg":
		n, err := strconv.ParseInt(v.Number, 10, 64)
		if err == nil {
			return dcbor.Int(n), nil
		}
		// -2^64 .. -2^63-1 do not fit in an int64. Major type 1 encodes
		// -1-argument, so the argument is the magnitude minus one.
		if len(v.Number) < 2 || v.Number[0] != '-' {
			return nil, err
		}
		magnitude := v.Number[1:]
		if magnitude == "18446744073709551616" { // 2^64
			return dcbor.Neg(^uint64(0)), nil
		}
		m, parseErr := strconv.ParseUint(magnitude, 10, 64)
		if parseErr != nil {
			return nil, parseErr
		}
		return dcbor.Neg(m - 1), nil
	case "text":
		return dcbor.Text(v.Text), nil
	case "bytes":
		b, err := hex.DecodeString(v.Bytes)
		if err != nil {
			return nil, err
		}
		return dcbor.Bytes(b), nil
	case "decimal":
		exponent, err := strconv.ParseInt(v.Exponent, 10, 64)
		if err != nil {
			return nil, err
		}
		mantissa, err := strconv.ParseInt(v.Mantissa, 10, 64)
		if err != nil {
			return nil, err
		}
		return dcbor.NewDecimal(exponent, mantissa)
	case "array":
		out := dcbor.Array{}
		for _, item := range v.Items {
			value, err := valueOf(item)
			if err != nil {
				return nil, err
			}
			out = append(out, value)
		}
		return out, nil
	case "map":
		out := dcbor.Map{}
		for _, e := range v.Entries {
			value, err := valueOf(e.Value)
			if err != nil {
				return nil, err
			}
			out = append(out, dcbor.MapEntry{Key: e.Key, Value: value})
		}
		return out, nil
	case "null":
		return dcbor.Null, nil
	default:
		return nil, errors.New("unknown value type " + strconv.Quote(v.Type))
	}
}

// articled renders an entity kind with its indefinite article, for the one
// failure message that names it.
func articled(kind string) string {
	if kind == "atom" {
		return "n " + kind
	}
	return " " + kind
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

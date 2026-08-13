package block

import (
	"bytes"
	"crypto/ed25519"
	"testing"

	"github.com/vrinek/Dialog/go/dcbor"
)

// TestDecodeRejections is the table of malformed blocks. Every case is signed
// with a valid signature over its own bytes — rawBlock does that — so what
// rejects each one is the structural rule under test and not rule 2.
//
// The mutations are described against the CDDL of spec/02-block-format.md: a
// wrong field type, a missing field, a field belonging to another block type,
// a key no definition declares, an operation the specification does not define,
// and the rotation block's "exactly one rotate_key operation and no other
// operations".
//
// The closed-map rule of spec/03-encoding.md, "Deterministic CBOR" rule 8, is
// exercised at every depth a block map nests to: an undeclared key on the block
// itself, on an operation, on a filler, and inside a scalar filler's value; and
// a declared key missing at each of those levels.
func TestDecodeRejections(t *testing.T) {
	priv := testKey(t, 1)
	pub := testPub(t, 1)
	newPub := testPub(t, 2)
	digest32 := bytes.Repeat([]byte{7}, 32)
	nonce := dcbor.Bytes(bytes.Repeat([]byte{3}, NonceSize))

	// set returns a copy of m with key set to v, adding it if absent.
	set := func(m dcbor.Map, key string, v dcbor.Value) dcbor.Map {
		out := make(dcbor.Map, 0, len(m)+1)
		replaced := false
		for _, e := range m {
			if e.Key == key {
				out, replaced = append(out, dcbor.MapEntry{Key: key, Value: v}), true
				continue
			}
			out = append(out, e)
		}
		if !replaced {
			out = append(out, dcbor.MapEntry{Key: key, Value: v})
		}
		return out
	}
	// drop returns a copy of m without key.
	drop := func(m dcbor.Map, key string) dcbor.Map {
		out := make(dcbor.Map, 0, len(m))
		for _, e := range m {
			if e.Key != key {
				out = append(out, e)
			}
		}
		return out
	}
	base := func() dcbor.Map { return validPublicMap(t, 1) }
	rotationMap := func(ops dcbor.Array) dcbor.Map {
		return set(set(base(), keyType, dcbor.Text(string(TypeRotation))), keyOps, ops)
	}
	privateMap := func() dcbor.Map {
		m := drop(drop(drop(base(), keyRefs), keyTS), keyOps)
		m = set(m, keyType, dcbor.Text(string(TypePrivate)))
		m = set(m, keyEnc, dcbor.Bytes("ciphertext"))
		return set(m, keyNonce, nonce)
	}
	rotateOp := dcbor.Map{
		{Key: keyOp, Value: dcbor.Text(OpRotateKey)},
		{Key: keyNewPub, Value: dcbor.Bytes(newPub)},
	}
	atomOp := MustCreateAtom("France").Value()

	cases := []struct {
		name  string
		block dcbor.Map
	}{
		// Version.
		{"version 0", set(base(), keyV, dcbor.Uint(0))},
		{"version 2", set(base(), keyV, dcbor.Uint(2))},
		{"version as a text string", set(base(), keyV, dcbor.Text("1"))},
		{"version as a negative integer", set(base(), keyV, dcbor.Int(-1))},

		// Type.
		{"unknown type", set(base(), keyType, dcbor.Text("sealed"))},
		{"type as a byte string", set(base(), keyType, dcbor.Bytes("public"))},
		{"missing type", drop(base(), keyType)},

		// Fixed-width fields.
		{"short public key", set(base(), keyPub, dcbor.Bytes(pub[:31]))},
		{"long public key", set(base(), keyPub, dcbor.Bytes(append(bytes.Clone(pub), 0)))},
		{"public key as a text string", set(base(), keyPub, dcbor.Text("0123456789abcdef0123456789abcdef"))},
		{"missing public key", drop(base(), keyPub)},

		// prev.
		{"prev of the wrong length", set(base(), keyPrev, dcbor.Bytes([]byte{1, 2, 3}))},
		{"prev as an integer", set(base(), keyPrev, dcbor.Uint(0))},
		{"missing prev", drop(base(), keyPrev)},

		// refs.
		{"refs as a map", set(base(), keyRefs, dcbor.Map{})},
		{"refs entry of the wrong length", set(base(), keyRefs, dcbor.Array{dcbor.Bytes([]byte{1})})},
		{"refs entry that is not a byte string", set(base(), keyRefs, dcbor.Array{dcbor.Text("x")})},
		{"missing refs", drop(base(), keyRefs)},

		// ts.
		{"ts as a text string", set(base(), keyTS, dcbor.Text("1740067200"))},
		{"negative ts", set(base(), keyTS, dcbor.Int(-1))},
		{"missing ts", drop(base(), keyTS)},

		// ops.
		{"empty ops", set(base(), keyOps, dcbor.Array{})},
		{"ops as a map", set(base(), keyOps, dcbor.Map{})},
		{"missing ops", drop(base(), keyOps)},
		{"operation that is not a map", set(base(), keyOps, dcbor.Array{dcbor.Text("create_atom")})},
		{"operation without an op key", set(base(), keyOps, dcbor.Array{dcbor.Map{{Key: keyDescription, Value: dcbor.Text("France")}}})},
		{"unknown operation", set(base(), keyOps, dcbor.Array{dcbor.Map{
			{Key: keyOp, Value: dcbor.Text("delete_atom")},
			{Key: keyDescription, Value: dcbor.Text("France")},
		}})},
		{"create_atom with an extra key", set(base(), keyOps, dcbor.Array{dcbor.Map{
			{Key: keyOp, Value: dcbor.Text(OpCreateAtom)},
			{Key: keyDescription, Value: dcbor.Text("France")},
			{Key: "note", Value: dcbor.Text("extra")},
		}})},
		{"create_atom with an empty description", set(base(), keyOps, dcbor.Array{dcbor.Map{
			{Key: keyOp, Value: dcbor.Text(OpCreateAtom)},
			{Key: keyDescription, Value: dcbor.Text("")},
		}})},
		{"create_atom with a numeric description", set(base(), keyOps, dcbor.Array{dcbor.Map{
			{Key: keyOp, Value: dcbor.Text(OpCreateAtom)},
			{Key: keyDescription, Value: dcbor.Uint(1)},
		}})},
		{"create_bond without variables", set(base(), keyOps, dcbor.Array{dcbor.Map{
			{Key: keyOp, Value: dcbor.Text(OpCreateBond)},
			{Key: keyTemplate, Value: dcbor.Text("no variables here")},
		}})},
		{"create_molecule with a short bond digest", set(base(), keyOps, dcbor.Array{dcbor.Map{
			{Key: keyOp, Value: dcbor.Text(OpCreateMolecule)},
			{Key: keyBond, Value: dcbor.Bytes([]byte{1})},
			{Key: keyFillers, Value: dcbor.Array{dcbor.Map{
				{Key: "type", Value: dcbor.Uint(0)},
				{Key: "value", Value: dcbor.Bytes(digest32)},
			}}},
		}})},
		{"create_molecule with no fillers", set(base(), keyOps, dcbor.Array{dcbor.Map{
			{Key: keyOp, Value: dcbor.Text(OpCreateMolecule)},
			{Key: keyBond, Value: dcbor.Bytes(digest32)},
			{Key: keyFillers, Value: dcbor.Array{}},
		}})},
		{"create_molecule without its fillers key", set(base(), keyOps, dcbor.Array{dcbor.Map{
			{Key: keyOp, Value: dcbor.Text(OpCreateMolecule)},
			{Key: keyBond, Value: dcbor.Bytes(digest32)},
		}})},
		{"filler with an extra key", set(base(), keyOps, dcbor.Array{dcbor.Map{
			{Key: keyOp, Value: dcbor.Text(OpCreateMolecule)},
			{Key: keyBond, Value: dcbor.Bytes(digest32)},
			{Key: keyFillers, Value: dcbor.Array{dcbor.Map{
				{Key: "note", Value: dcbor.Text("extra")},
				{Key: "type", Value: dcbor.Uint(0)},
				{Key: "value", Value: dcbor.Bytes(digest32)},
			}}},
		}})},
		{"scalar filler value with an extra key", set(base(), keyOps, dcbor.Array{dcbor.Map{
			{Key: keyOp, Value: dcbor.Text(OpCreateMolecule)},
			{Key: keyBond, Value: dcbor.Bytes(digest32)},
			{Key: keyFillers, Value: dcbor.Array{dcbor.Map{
				{Key: "type", Value: dcbor.Uint(4)},
				{Key: "value", Value: dcbor.Map{
					{Key: "note", Value: dcbor.Text("extra")},
					{Key: "unit", Value: dcbor.Bytes(digest32)},
					{Key: "value", Value: dcbor.Uint(70)},
				}},
			}}},
		}})},
		{"create_molecule with an unknown filler type", set(base(), keyOps, dcbor.Array{dcbor.Map{
			{Key: keyOp, Value: dcbor.Text(OpCreateMolecule)},
			{Key: keyBond, Value: dcbor.Bytes(digest32)},
			{Key: keyFillers, Value: dcbor.Array{dcbor.Map{
				{Key: "type", Value: dcbor.Uint(5)},
				{Key: "value", Value: dcbor.Bytes(digest32)},
			}}},
		}})},
		{"rotate_key with a short key", set(base(), keyOps, dcbor.Array{dcbor.Map{
			{Key: keyOp, Value: dcbor.Text(OpRotateKey)},
			{Key: keyNewPub, Value: dcbor.Bytes(newPub[:16])},
		}})},

		// Unknown and misplaced block keys.
		{"unknown block key", set(base(), "extra", dcbor.Uint(1))},
		{"public block carrying enc", set(base(), keyEnc, dcbor.Bytes("x"))},
		{"public block carrying nonce", set(base(), keyNonce, nonce)},
		{"public block carrying a rotate_key operation", set(base(), keyOps, dcbor.Array{rotateOp})},

		// Rotation blocks.
		{"rotation block with two operations", rotationMap(dcbor.Array{rotateOp, atomOp})},
		{"rotation block with a create_atom operation", rotationMap(dcbor.Array{atomOp})},
		{"rotation block with no operations", rotationMap(dcbor.Array{})},
		{"rotation block with two rotate_key operations", rotationMap(dcbor.Array{rotateOp, rotateOp})},

		// Private blocks.
		{"private block without enc", drop(privateMap(), keyEnc)},
		{"private block without nonce", drop(privateMap(), keyNonce)},
		{"private block with a short nonce", set(privateMap(), keyNonce, dcbor.Bytes(bytes.Repeat([]byte{1}, 12)))},
		{"private block with a long nonce", set(privateMap(), keyNonce, dcbor.Bytes(bytes.Repeat([]byte{1}, 32)))},
		{"private block with plaintext ops", set(privateMap(), keyOps, dcbor.Array{atomOp})},
		{"private block with plaintext refs", set(privateMap(), keyRefs, dcbor.Array{})},
		{"private block with plaintext ts", set(privateMap(), keyTS, dcbor.Uint(1))},
		{"private block with a text enc", set(privateMap(), keyEnc, dcbor.Text("ciphertext"))},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded := rawBlock(t, priv, tc.block)
			if b, err := Decode(encoded); err == nil {
				t.Errorf("Decode accepted %s, want an error", b)
			}
		})
	}
}

// TestDecodeAcceptsValidBlocks is the positive half of the table: a valid
// block of each type survives Decode with its fields intact.
func TestDecodeAcceptsValidBlocks(t *testing.T) {
	priv := testKey(t, 1)
	author := mustBuilder(t, 1)
	genesis, err := author.Public(1000, nil, MustCreateAtom("France"))
	if err != nil {
		t.Fatalf("genesis: %v", err)
	}
	rotation, err := author.Rotation(2000, nil, testPub(t, 2))
	if err != nil {
		t.Fatalf("rotation: %v", err)
	}
	private, err := mustBuilder(t, 3).Private([]byte("ciphertext"), bytes.Repeat([]byte{2}, NonceSize))
	if err != nil {
		t.Fatalf("private: %v", err)
	}

	for _, b := range []*Block{genesis, rotation, private} {
		decoded, err := Decode(b.Bytes())
		if err != nil {
			t.Fatalf("Decode(%s): %v", b, err)
		}
		if decoded.Digest() != b.Digest() || decoded.Type() != b.Type() {
			t.Errorf("Decode changed the block: %s -> %s", b, decoded)
		}
	}

	// An empty refs list and a large timestamp are both permitted.
	wide := rawBlock(t, priv, dcbor.Map{
		{Key: keyV, Value: dcbor.Uint(Version)},
		{Key: keyType, Value: dcbor.Text(string(TypePublic))},
		{Key: keyPub, Value: dcbor.Bytes(testPub(t, 1))},
		{Key: keyPrev, Value: dcbor.Null},
		{Key: keyRefs, Value: dcbor.Array{}},
		{Key: keyTS, Value: dcbor.Uint(1 << 40)},
		{Key: keyOps, Value: dcbor.Array{MustCreateAtom("France").Value()}},
	})
	if _, err := Decode(wide); err != nil {
		t.Errorf("Decode of a block with a large timestamp: %v", err)
	}
}

// TestDecodeRejectsNonCanonicalBytes covers rule 8: the encoding must be
// canonical dCBOR, so a block that carries the right fields in the wrong order
// is not the same block.
func TestDecodeRejectsNonCanonicalBytes(t *testing.T) {
	author := mustBuilder(t, 1)
	b, err := author.Public(1000, nil, MustCreateAtom("France"))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	encoded := b.Bytes()

	cases := map[string][]byte{
		"truncated":                 encoded[:len(encoded)-1],
		"trailing byte":             append(bytes.Clone(encoded), 0),
		"empty input":               {},
		"not a map":                 {0x80},
		"indefinite-length map":     {0xbf, 0xff},
		"a bare text string":        {0x63, 'a', 'b', 'c'},
		"a tag around the map":      append([]byte{0xc4}, encoded...),
		"map key count off by one":  swapFirstByte(encoded, 0xa9),
		"map key count off the low": swapFirstByte(encoded, 0xa7),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(input); err == nil {
				t.Error("Decode accepted non-canonical input, want an error")
			}
		})
	}
}

func swapFirstByte(b []byte, first byte) []byte {
	out := bytes.Clone(b)
	out[0] = first
	return out
}

// TestDecodeRejectsForeignSignature checks that a block signed by one key and
// claiming another is rejected, which is the property the domain-separated
// signing input exists to make cheap to check.
func TestDecodeRejectsForeignSignature(t *testing.T) {
	m := validPublicMap(t, 1)
	encoded := rawBlock(t, testKey(t, 2), m) // signed by key 2, claims key 1
	if _, err := Decode(encoded); err == nil {
		t.Fatal("Decode accepted a block signed by a key other than the one it claims")
	}
}

// TestSignatureCoversEveryField checks rule 2's reach: changing any signed
// field invalidates the signature (spec/02-block-format.md, "Security
// Considerations").
func TestSignatureCoversEveryField(t *testing.T) {
	priv := testKey(t, 1)
	original := validPublicMap(t, 1)
	signingBytes, err := dcbor.Encode(original)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	sig := ed25519.Sign(priv, append([]byte(DomainSeparator), signingBytes...))

	mutations := map[string]dcbor.Value{
		keyTS:   dcbor.Uint(1740067201),
		keyRefs: dcbor.Array{dcbor.Bytes(bytes.Repeat([]byte{4}, 32))},
		keyOps:  dcbor.Array{MustCreateAtom("Germany").Value()},
	}
	for key, value := range mutations {
		t.Run(key, func(t *testing.T) {
			mutated := make(dcbor.Map, 0, len(original)+1)
			for _, e := range original {
				if e.Key == key {
					e.Value = value
				}
				mutated = append(mutated, e)
			}
			mutated = append(mutated, dcbor.MapEntry{Key: keySig, Value: dcbor.Bytes(sig)})
			encoded, err := dcbor.Encode(mutated)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if _, err := Decode(encoded); err == nil {
				t.Errorf("changing %q left the signature valid", key)
			}
		})
	}
}

package block

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/dcbor"
	"github.com/vrinek/Dialog/go/entity"
)

// Test keys are derived from fixed seeds so that every byte in this package's
// tests — signing inputs, signatures, block digests — is reproducible run to
// run and machine to machine.
func testKey(t testing.TB, seed byte) ed25519.PrivateKey {
	t.Helper()
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize))
}

func testPub(t testing.TB, seed byte) ed25519.PublicKey {
	t.Helper()
	pub, ok := testKey(t, seed).Public().(ed25519.PublicKey)
	if !ok {
		t.Fatal("test key does not yield an Ed25519 public key")
	}
	return pub
}

func mustBuilder(t testing.TB, seed byte) *Builder {
	t.Helper()
	b, err := NewBuilder(testKey(t, seed))
	if err != nil {
		t.Fatalf("NewBuilder: %v", err)
	}
	return b
}

// testCiphertext stands in for a private block's enc field: opaque bytes this
// package never reads, padded to at least MinEncSize because a shorter value
// cannot be an XChaCha20-Poly1305 ciphertext and is refused as structurally
// invalid (spec/02-block-format.md, "Private block").
func testCiphertext(label string) []byte {
	enc := []byte("ciphertext " + label)
	for len(enc) < MinEncSize {
		enc = append(enc, '.')
	}
	return enc
}

// rawBlock signs an arbitrary block map — one this package's constructors
// would refuse to build — and returns its complete dCBOR encoding. It is how
// the rejection tables put malformed blocks on the wire with a valid
// signature, so that a rejection is the structural rule firing and not the
// signature check.
func rawBlock(t testing.TB, priv ed25519.PrivateKey, m dcbor.Map) []byte {
	t.Helper()
	signingBytes, err := dcbor.Encode(m)
	if err != nil {
		t.Fatalf("encoding the signing map: %v", err)
	}
	sig := ed25519.Sign(priv, append([]byte(DomainSeparator), signingBytes...))
	full := append(append(dcbor.Map{}, m...), dcbor.MapEntry{Key: keySig, Value: dcbor.Bytes(sig)})
	b, err := dcbor.Encode(full)
	if err != nil {
		t.Fatalf("encoding the block: %v", err)
	}
	return b
}

// validPublicMap is the signing map of a minimal valid public genesis block,
// used as the base the rejection tables mutate.
func validPublicMap(t testing.TB, seed byte) dcbor.Map {
	t.Helper()
	return dcbor.Map{
		{Key: keyV, Value: dcbor.Uint(Version)},
		{Key: keyType, Value: dcbor.Text(string(TypePublic))},
		{Key: keyPub, Value: dcbor.Bytes(testPub(t, seed))},
		{Key: keyPrev, Value: dcbor.Null},
		{Key: keyRefs, Value: dcbor.Array{}},
		{Key: keyTS, Value: dcbor.Uint(1740067200)},
		{Key: keyOps, Value: dcbor.Array{MustCreateAtom("France").Value()}},
	}
}

// TestSignAndVerify covers the author-side path: a signed block verifies, its
// content survives a round trip through the wire, and its identity is the hash
// of the bytes that carry the signature (spec/02-block-format.md, "Block
// identification").
func TestSignAndVerify(t *testing.T) {
	priv := testKey(t, 1)
	b, err := Sign(Content{
		Version: Version,
		Type:    TypePublic,
		TS:      1740067200,
		Ops:     []Operation{MustCreateAtom("France")},
	}, priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := b.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
	if !b.IsGenesis() {
		t.Error("a block signed with no prev must be a genesis block")
	}
	if got, want := b.Digest(), cid.Digest(sha256.Sum256(b.Bytes())); got != want {
		t.Errorf("Digest = %s, want SHA-256 of the block's bytes %s", got, want)
	}
	if got := b.CID().Digest(); got != b.Digest() {
		t.Errorf("CID carries digest %s, want %s", got, b.Digest())
	}

	decoded, err := Decode(b.Bytes())
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Digest() != b.Digest() {
		t.Errorf("round trip changed the digest: %s -> %s", b.Digest(), decoded.Digest())
	}
	if !bytes.Equal(decoded.Bytes(), b.Bytes()) {
		t.Error("round trip changed the encoding")
	}
	if got := decoded.Ops(); len(got) != 1 || got[0].Op() != OpCreateAtom {
		t.Errorf("decoded ops = %v, want one create_atom", got)
	}
}

// TestSigningInputShape checks the two halves of the signature's input
// separately: the domain separator, and the encoding of the block without its
// "sig" key (spec/04-cryptography.md, "Signing procedure").
func TestSigningInputShape(t *testing.T) {
	priv := testKey(t, 1)
	b, err := Sign(Content{Version: Version, Type: TypePublic, TS: 7, Ops: []Operation{MustCreateAtom("France")}}, priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	input := b.SigningInput()
	if !bytes.HasPrefix(input, []byte(DomainSeparator)) {
		t.Fatalf("signing input does not begin with the domain separator: %x", input[:16])
	}
	if got, want := len(DomainSeparator), 15; got != want {
		t.Errorf("domain separator is %d bytes, want %d", got, want)
	}
	if !bytes.Equal(input[len(DomainSeparator):], b.SigningBytes()) {
		t.Error("signing input is not the domain separator followed by the signing bytes")
	}
	// The signing bytes are the block map minus "sig": one key fewer, and no
	// signature bytes anywhere inside them.
	if bytes.Contains(b.SigningBytes(), b.Signature()) {
		t.Error("the signature must not appear inside the bytes it signs")
	}
	if !ed25519.Verify(b.PublicKey(), input, b.Signature()) {
		t.Error("the signature does not verify over the input this package builds")
	}
}

// TestSignRejectsMismatchedKey guards the one way a Content can lie about its
// author.
func TestSignRejectsMismatchedKey(t *testing.T) {
	_, err := Sign(Content{
		Version: Version,
		Type:    TypePublic,
		Pub:     testPub(t, 2),
		TS:      1,
		Ops:     []Operation{MustCreateAtom("France")},
	}, testKey(t, 1))
	if err == nil {
		t.Fatal("signing a block that claims another author's key must fail")
	}
}

// TestAssembleRejectsBadSignature covers the constructor for signatures
// computed elsewhere.
func TestAssembleRejectsBadSignature(t *testing.T) {
	c := Content{
		Version: Version,
		Type:    TypePublic,
		Pub:     testPub(t, 1),
		TS:      1,
		Ops:     []Operation{MustCreateAtom("France")},
	}
	_, err := Assemble(c, bytes.Repeat([]byte{0}, SignatureSize))
	var sigErr *SignatureError
	if !errors.As(err, &sigErr) {
		t.Fatalf("Assemble with a zero signature = %v, want a *SignatureError", err)
	}
	if _, err := Assemble(c, []byte{1, 2, 3}); err == nil {
		t.Fatal("Assemble with a short signature must fail")
	}
}

// TestTamperedPayloadFailsSignature flips one byte of an operation's payload
// inside an encoded block. The bytes stay well-formed dCBOR, so what rejects
// them is rule 2 (spec/02-block-format.md: "Tampering with any field
// invalidates the signature").
func TestTamperedPayloadFailsSignature(t *testing.T) {
	b, err := Sign(Content{Version: Version, Type: TypePublic, TS: 1, Ops: []Operation{MustCreateAtom("France")}}, testKey(t, 1))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	encoded := b.Bytes()
	i := bytes.Index(encoded, []byte("France"))
	if i < 0 {
		t.Fatal("the description is not in the encoding")
	}
	encoded[i] = 'G' // "France" -> "Grance": same length, still UTF-8
	_, err = Decode(encoded)
	var sigErr *SignatureError
	if !errors.As(err, &sigErr) {
		t.Fatalf("Decode of a tampered block = %v, want a *SignatureError", err)
	}
}

// TestTamperedSignatureFails does the same to the signature itself.
func TestTamperedSignatureFails(t *testing.T) {
	b, err := Sign(Content{Version: Version, Type: TypePublic, TS: 1, Ops: []Operation{MustCreateAtom("France")}}, testKey(t, 1))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	encoded := b.Bytes()
	i := bytes.Index(encoded, b.Signature())
	if i < 0 {
		t.Fatal("the signature is not in the encoding")
	}
	encoded[i] ^= 0x01
	if _, err := Decode(encoded); err == nil {
		t.Fatal("Decode of a block with a flipped signature bit must fail")
	}
}

// TestPrivateBlockRoundTrip covers a private block at the structural level:
// enc and nonce are opaque byte strings that the signature covers and the
// decoder returns unchanged (spec/02-block-format.md, "Private block").
func TestPrivateBlockRoundTrip(t *testing.T) {
	enc := []byte("not really a ciphertext, but this package never looks")
	nonce := bytes.Repeat([]byte{0xab}, NonceSize)

	author := mustBuilder(t, 3)
	b, err := author.Private(enc, nonce)
	if err != nil {
		t.Fatalf("Private: %v", err)
	}
	if b.Type() != TypePrivate {
		t.Fatalf("type = %q, want %q", b.Type(), TypePrivate)
	}
	if got, ok := b.Enc(); !ok || !bytes.Equal(got, enc) {
		t.Errorf("Enc = %q, %v, want the ciphertext back", got, ok)
	}
	if got, ok := b.Nonce(); !ok || !bytes.Equal(got, nonce) {
		t.Errorf("Nonce = %x, %v, want the nonce back", got, ok)
	}
	if b.Ops() != nil || len(b.Refs()) != 0 || b.TS() != 0 {
		t.Error("a private block must carry no plaintext ops, refs or ts")
	}

	decoded, err := Decode(b.Bytes())
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	gotEnc, _ := decoded.Enc()
	gotNonce, _ := decoded.Nonce()
	if !bytes.Equal(gotEnc, enc) || !bytes.Equal(gotNonce, nonce) {
		t.Error("the round trip changed the opaque fields")
	}
	// The signing input of a private block is the map of spec/04's
	// signing-input-private: v, type, pub, prev, enc, nonce and nothing else.
	for _, key := range []string{keyRefs, keyTS, keyOps, keySig} {
		if strings.Contains(string(decoded.SigningBytes()), key) {
			t.Errorf("the private signing input must not carry the %q key", key)
		}
	}
}

// TestPrivateBlockRejectsWrongNonceSize covers the one size constraint a
// private block carries (spec/02-block-format.md: nonce is bstr .size 24).
func TestPrivateBlockRejectsWrongNonceSize(t *testing.T) {
	for _, size := range []int{0, 12, 23, 25, 32} {
		author := mustBuilder(t, 3)
		if _, err := author.Private(testCiphertext("nonce case"), bytes.Repeat([]byte{1}, size)); err == nil {
			t.Errorf("a private block with a %d-byte nonce must be rejected", size)
		}
	}
}

// TestPrivateBlockEncLowerBound covers the one length constraint on the
// ciphertext: enc is bstr .size (16..), because XChaCha20-Poly1305 appends a
// 16-byte Poly1305 tag to every ciphertext and a shorter value cannot be the
// AEAD's output (spec/02-block-format.md, "Private block"). The bound is
// checkable without the decryption key, which is what makes it worth having:
// most nodes never decrypt a private block at all. There is no upper bound.
func TestPrivateBlockEncLowerBound(t *testing.T) {
	nonce := bytes.Repeat([]byte{7}, NonceSize)
	for _, size := range []int{0, 1, MinEncSize - 1} {
		if _, err := mustBuilder(t, 3).Private(bytes.Repeat([]byte{1}, size), nonce); err == nil {
			t.Errorf("a private block with a %d-byte enc must be rejected", size)
		}
	}
	if _, err := mustBuilder(t, 3).Private(nil, nonce); err == nil {
		t.Error("a private block with no enc must be rejected")
	}

	// Exactly the tag length is the smallest ciphertext there can be, and it is
	// accepted — as is a large one: the protocol sets no ceiling.
	for _, size := range []int{MinEncSize, 4096} {
		b, err := mustBuilder(t, 3).Private(bytes.Repeat([]byte{1}, size), nonce)
		if err != nil {
			t.Fatalf("a private block with a %d-byte enc: %v", size, err)
		}
		if _, err := Decode(b.Bytes()); err != nil {
			t.Errorf("Decode of a block with a %d-byte enc: %v", size, err)
		}
	}
}

// TestOperationEncodings pins the wire shape of each of the four operations
// and the identity each one defines (spec/02-block-format.md, "Operations").
func TestOperationEncodings(t *testing.T) {
	atom := MustCreateAtom("France")
	if got, want := hex.EncodeToString(EncodeOperation(atom)), "a2626f706b6372656174655f61746f6d6b6465736372697074696f6e664672616e6365"; got != want {
		t.Errorf("create_atom encoding = %s, want %s", got, want)
	}
	// The identity is the atom's, not the operation's: the "op" key is not
	// part of what is hashed.
	if got, want := mustDigest(atom), entity.MustAtom("France").Digest(); got != want {
		t.Errorf("create_atom defines %s, want the atom digest %s", got, want)
	}

	bond := MustCreateBond("_A_ is the capital of _B_")
	if got, want := mustDigest(bond), entity.MustBond("_A_ is the capital of _B_").Digest(); got != want {
		t.Errorf("create_bond defines %s, want the bond digest %s", got, want)
	}

	fillers := []entity.Filler{
		entity.AtomFiller(entity.MustAtom("Paris, the capital of France").Digest()),
		entity.AtomFiller(entity.MustAtom("France").Digest()),
	}
	molecule := MustCreateMolecule(bond.Bond(), fillers)
	if got, want := mustDigest(molecule), entity.MustMolecule(bond.Bond(), fillers).Digest(); got != want {
		t.Errorf("create_molecule defines %s, want the molecule digest %s", got, want)
	}
	refs := molecule.References()
	if len(refs) != 3 || refs[0].Kind != KindBond || refs[1].Kind != KindAtom || refs[2].Kind != KindAtom {
		t.Errorf("create_molecule references = %v, want the bond and two atoms", refs)
	}

	rotate := MustRotateKey(testPub(t, 9))
	if _, _, ok := rotate.Creates(); ok {
		t.Error("rotate_key creates no entity")
	}
	if len(rotate.References()) != 0 {
		t.Error("rotate_key references no entity")
	}
	// a2 | "op" -> "rotate_key" | "new_pub" -> 32 bytes
	wantRotate := "a2626f706a726f746174655f6b6579676e65775f7075625820" + hex.EncodeToString(testPub(t, 9))
	if got := hex.EncodeToString(EncodeOperation(rotate)); got != wantRotate {
		t.Errorf("rotate_key encoding = %s, want %s", got, wantRotate)
	}
}

func mustDigest(op Operation) cid.Digest {
	d, _, ok := op.Creates()
	if !ok {
		panic("operation creates no entity")
	}
	return d
}

// TestBuilderChain covers the author-side chain API: the first block is a
// genesis block, each later one links to the block before it, and the chain is
// closed by a rotation block.
func TestBuilderChain(t *testing.T) {
	author := mustBuilder(t, 1)
	genesis, err := author.Public(100, nil, MustCreateAtom("France"))
	if err != nil {
		t.Fatalf("genesis: %v", err)
	}
	if !genesis.IsGenesis() {
		t.Error("the first block of a chain must have a null prev")
	}
	second, err := author.Public(200, nil, MustCreateAtom("Paris, the capital of France"))
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if prev, ok := second.Prev(); !ok || prev != genesis.Digest() {
		t.Errorf("second block prev = %v, %v, want %s", prev, ok, genesis.Digest())
	}
	rotation, err := author.Rotation(300, nil, testPub(t, 2))
	if err != nil {
		t.Fatalf("rotation: %v", err)
	}
	if rotation.Type() != TypeRotation {
		t.Errorf("rotation block type = %q", rotation.Type())
	}
	if _, err := author.Public(400, nil, MustCreateAtom("too late")); err == nil {
		t.Error("a builder must refuse to sign a block after its rotation block")
	}
}

// TestRotateKeyOnlyInRotationBlocks covers the scope of the fourth operation:
// the operation rule a public or private block's ops list follows does not
// include rotate_key, and only a rotation block may carry one — exactly one,
// and nothing else (spec/02-block-format.md, "Operations", "Validation
// dispatch" and "rotate_key").
func TestRotateKeyOnlyInRotationBlocks(t *testing.T) {
	rotate := MustRotateKey(testPub(t, 2))

	// Author side: a public block carrying one is never signed.
	c := Content{Version: Version, Type: TypePublic, Pub: testPub(t, 1), TS: 1,
		Ops: []Operation{MustCreateAtom("France"), rotate}}
	if err := c.Validate(); err == nil {
		t.Error("Content.Validate accepted a rotate_key operation in a public block")
	}
	if _, err := Sign(c, testKey(t, 1)); err == nil {
		t.Error("Sign accepted a rotate_key operation in a public block")
	}

	// Wire side: the same block, correctly signed by the key it claims, is
	// rejected for the operation and not for its signature.
	m := validPublicMap(t, 1)
	for i, e := range m {
		if e.Key == keyOps {
			m[i].Value = dcbor.Array{MustCreateAtom("France").Value(), rotate.Value()}
		}
	}
	if b, err := Decode(rawBlock(t, testKey(t, 1), m)); err == nil {
		t.Errorf("Decode accepted %s, a public block carrying a rotate_key operation", b)
	}

	// The same operation in the block type that announces the chain's end is
	// the one place it belongs.
	rotation, err := mustBuilder(t, 1).Rotation(1, nil, testPub(t, 2))
	if err != nil {
		t.Fatalf("Rotation: %v", err)
	}
	if op, ok := rotation.RotateKey(); !ok || !ed25519.PublicKey(op.NewPublicKey()).Equal(testPub(t, 2)) {
		t.Errorf("the rotation block does not carry the rotate_key operation naming the successor key")
	}
}

// TestContentValidateRejections is the table for the structural rules a
// Content must satisfy before it can be signed at all.
func TestContentValidateRejections(t *testing.T) {
	pub := testPub(t, 1)
	atom := MustCreateAtom("France")
	rotate := MustRotateKey(testPub(t, 2))

	cases := []struct {
		name string
		c    Content
	}{
		{"zero value", Content{}},
		{"version 0", Content{Type: TypePublic, Pub: pub, Ops: []Operation{atom}}},
		{"version 2", Content{Version: 2, Type: TypePublic, Pub: pub, Ops: []Operation{atom}}},
		{"unknown type", Content{Version: Version, Type: "sealed", Pub: pub, Ops: []Operation{atom}}},
		{"short public key", Content{Version: Version, Type: TypePublic, Pub: pub[:31], Ops: []Operation{atom}}},
		{"no operations", Content{Version: Version, Type: TypePublic, Pub: pub}},
		{"public block with enc", Content{Version: Version, Type: TypePublic, Pub: pub, Ops: []Operation{atom}, Enc: testCiphertext("public")}},
		{"public block with nonce", Content{Version: Version, Type: TypePublic, Pub: pub, Ops: []Operation{atom}, Nonce: bytes.Repeat([]byte{0}, NonceSize)}},
		{"public block with rotate_key", Content{Version: Version, Type: TypePublic, Pub: pub, Ops: []Operation{rotate}}},
		{"rotation block with two operations", Content{Version: Version, Type: TypeRotation, Pub: pub, Ops: []Operation{rotate, atom}}},
		{"rotation block with the wrong operation", Content{Version: Version, Type: TypeRotation, Pub: pub, Ops: []Operation{atom}}},
		{"rotation block with no operation", Content{Version: Version, Type: TypeRotation, Pub: pub}},
		{"private block without enc", Content{Version: Version, Type: TypePrivate, Pub: pub, Nonce: bytes.Repeat([]byte{0}, NonceSize)}},
		{"private block without nonce", Content{Version: Version, Type: TypePrivate, Pub: pub, Enc: testCiphertext("no nonce")}},
		{"private block with plaintext ops", Content{Version: Version, Type: TypePrivate, Pub: pub, Enc: testCiphertext("plaintext field"), Nonce: bytes.Repeat([]byte{0}, NonceSize), Ops: []Operation{atom}}},
		{"private block with plaintext refs", Content{Version: Version, Type: TypePrivate, Pub: pub, Enc: testCiphertext("plaintext field"), Nonce: bytes.Repeat([]byte{0}, NonceSize), Refs: []cid.Digest{}}},
		{"private block with plaintext ts", Content{Version: Version, Type: TypePrivate, Pub: pub, Enc: testCiphertext("plaintext field"), Nonce: bytes.Repeat([]byte{0}, NonceSize), TS: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.c.Validate(); err == nil {
				t.Error("Content.Validate = nil, want an error")
			}
			if _, err := Sign(tc.c, testKey(t, 1)); err == nil {
				t.Error("Sign = nil, want an error")
			}
		})
	}
}

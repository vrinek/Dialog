// Package block implements Dialog's Layer 1: the signed, append-only blocks
// that carry ontology operations, as specified in spec/02-block-format.md and
// spec/04-cryptography.md.
//
// A Block is immutable and always signed. The only ways to obtain one are Sign
// (author side), Assemble (a signature computed elsewhere) and Decode (the
// wire), and each of the three verifies the Ed25519 signature before it
// returns. A *Block that exists therefore satisfies spec/02's validation rule
// 2 by construction; the rules that need context — chain linkage, reachability
// of the entities an operation references, fork detection — are the business
// of Validate and ValidateChain, which read blocks from a Source.
//
// # Identity
//
// A block's identity is the hash of its complete encoding, signature included
// (spec/02-block-format.md, "Block identification"):
//
//	Digest(block) = SHA-256(dCBOR(block))
//	CID(block)    = 0x01 || 0x71 || 0x12 || 0x20 || Digest(block)
//
// The signature is inside the hashed bytes, so a block's digest is not
// computable from its content alone. The signature itself is over a different
// byte string — the block without its "sig" key, behind a domain separator; see
// SigningInput.
//
// Other blocks refer to a block by its raw 32-byte digest, never by its CID:
// that is what the prev field and each entry of refs carry
// (spec/03-encoding.md, "Internal references").
//
// # Private blocks
//
// A private block's refs, ts and ops are encrypted into its enc field. This
// package treats enc and nonce as opaque byte strings: it signs them, hashes
// them, and validates everything that does not require reading them (rules 1,
// 2, 3, 8 and 9 of spec/02-block-format.md, "Validation"). Encryption and
// decryption belong to the privacy package, and the rules that need the
// plaintext — 4, 5 and 6 — are reported as unvalidatable until it exists,
// which is what spec/02 says of any node that lacks the key.
package block

import (
	"bytes"
	"crypto/ed25519"
	"fmt"
	"slices"

	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/dcbor"
)

// Version is the protocol version this package implements. A block carrying
// any other value in its v field is rejected (spec/02-block-format.md,
// "Validation" rule 1).
const Version uint64 = 1

// DomainSeparator prefixes the bytes a block signature covers, keeping a
// Dialog signature from being replayed as a signature over anything else
// (spec/04-cryptography.md, "Signing procedure"). It is 15 bytes of UTF-8.
const DomainSeparator = "dialog-v1-block"

// Sizes fixed by spec/02-block-format.md and spec/04-cryptography.md.
const (
	// PublicKeySize is the size of a raw Ed25519 public key.
	PublicKeySize = ed25519.PublicKeySize
	// SignatureSize is the size of a raw Ed25519 signature.
	SignatureSize = ed25519.SignatureSize
	// NonceSize is the size of a private block's XChaCha20 nonce.
	NonceSize = 24
)

// Block map keys (spec/02-block-format.md).
const (
	keyV     = "v"
	keyType  = "type"
	keyPub   = "pub"
	keySig   = "sig"
	keyPrev  = "prev"
	keyRefs  = "refs"
	keyTS    = "ts"
	keyOps   = "ops"
	keyEnc   = "enc"
	keyNonce = "nonce"
)

// A Type is the value of a block's type field. It selects the block's
// structure and its validation rules (spec/02-block-format.md, "Validation
// dispatch").
type Type string

// The three block types. There are no others; a block carrying any other type
// value is rejected.
const (
	// TypePublic is a block whose refs, ts and ops are in the clear.
	TypePublic Type = "public"
	// TypePrivate is a block whose refs, ts and ops are encrypted into enc.
	TypePrivate Type = "private"
	// TypeRotation is a block holding exactly one rotate_key operation, which
	// ends the current key's chain.
	TypeRotation Type = "rotation"
)

// Valid reports whether t is one of the three block types.
func (t Type) Valid() bool {
	return t == TypePublic || t == TypePrivate || t == TypeRotation
}

// hasPlaintextPayload reports whether a block of this type carries refs, ts
// and ops in the clear. Only a private block does not.
func (t Type) hasPlaintextPayload() bool { return t == TypePublic || t == TypeRotation }

// Content is a block without its signature: everything the signature covers.
// It is the author-side shape — fill one in and hand it to Sign — and it is
// what Block.Content returns.
//
// A Content is validated by every function that consumes it, so the zero value
// and any half-filled value are refused rather than signed.
type Content struct {
	// Version is the protocol version. It MUST be Version.
	Version uint64
	// Type is the block type.
	Type Type
	// Pub is the author's raw 32-byte Ed25519 public key.
	Pub ed25519.PublicKey
	// Prev is the digest of the previous block in this author's chain, or nil
	// for a genesis block (spec/02-block-format.md: prev MUST be null for the
	// genesis block and MUST NOT be null for any other).
	Prev *cid.Digest
	// Refs lists the CID-providing blocks this block's operations depend on.
	// It is empty for a private block, whose refs are inside Enc.
	Refs []cid.Digest
	// TS is the author's self-reported Unix timestamp, in seconds. It is
	// untrusted and MUST NOT decide validity (spec/02-block-format.md,
	// "Security Considerations"). It is zero for a private block.
	TS uint64
	// Ops are the block's operations, in order. It is nil for a private
	// block, whose operations are inside Enc.
	Ops []Operation
	// Enc is a private block's ciphertext over refs, ts and ops. This package
	// treats it as opaque; the privacy package produces and consumes it.
	Enc []byte
	// Nonce is the 24-byte XChaCha20 nonce of a private block.
	Nonce []byte
}

// Clone returns a deep copy of c. Operations are immutable values, so the
// slice is copied but its elements are shared.
func (c Content) Clone() Content {
	c.Pub = slices.Clone(c.Pub)
	if c.Prev != nil {
		prev := *c.Prev
		c.Prev = &prev
	}
	c.Refs = slices.Clone(c.Refs)
	c.Ops = slices.Clone(c.Ops)
	c.Enc = slices.Clone(c.Enc)
	c.Nonce = slices.Clone(c.Nonce)
	return c
}

// Validate reports whether c is a structurally well-formed block content: the
// field set of its type, a recognized version, key and nonce sizes, a
// non-empty ops list, and the rotation block's single rotate_key operation
// (spec/02-block-format.md, the three CDDL definitions and "Validation
// dispatch").
//
// It is the check Sign, Assemble and Decode all run. It cannot check anything
// that needs other blocks — that is Validate's job.
func (c Content) Validate() error {
	if c.Version != Version {
		return fmt.Errorf("block: unrecognized protocol version %d, want %d", c.Version, Version)
	}
	if !c.Type.Valid() {
		return fmt.Errorf("block: unknown block type %q; it must be %q, %q or %q", string(c.Type), TypePublic, TypePrivate, TypeRotation)
	}
	if len(c.Pub) != PublicKeySize {
		return fmt.Errorf("block: %q is %d bytes, want %d", keyPub, len(c.Pub), PublicKeySize)
	}

	if c.Type.hasPlaintextPayload() {
		if c.Enc != nil {
			return fmt.Errorf("block: a %s block must not carry an %q field; it belongs to a private block", c.Type, keyEnc)
		}
		if c.Nonce != nil {
			return fmt.Errorf("block: a %s block must not carry a %q field; it belongs to a private block", c.Type, keyNonce)
		}
		if len(c.Ops) == 0 {
			return fmt.Errorf("block: a %s block has no operations; %q must hold at least one", c.Type, keyOps)
		}
		for i, op := range c.Ops {
			if op == nil {
				return fmt.Errorf("block: operation %d is nil", i)
			}
		}
	} else {
		if c.Refs != nil {
			return fmt.Errorf("block: a private block must not carry a plaintext %q field; refs are encrypted into %q", keyRefs, keyEnc)
		}
		if c.TS != 0 {
			return fmt.Errorf("block: a private block must not carry a plaintext %q field; the timestamp is encrypted into %q", keyTS, keyEnc)
		}
		if c.Ops != nil {
			return fmt.Errorf("block: a private block must not carry a plaintext %q field; operations are encrypted into %q", keyOps, keyEnc)
		}
		if c.Enc == nil {
			return fmt.Errorf("block: a private block is missing its %q field", keyEnc)
		}
		if len(c.Nonce) != NonceSize {
			return fmt.Errorf("block: %q is %d bytes, want %d", keyNonce, len(c.Nonce), NonceSize)
		}
	}

	switch c.Type {
	case TypeRotation:
		// "It MUST contain exactly one rotate_key operation and no other
		// operations" (spec/02-block-format.md, "Rotation block").
		if len(c.Ops) != 1 {
			return fmt.Errorf("block: a rotation block carries %d operations; it must contain exactly one %s operation and no others", len(c.Ops), OpRotateKey)
		}
		if _, ok := c.Ops[0].(RotateKey); !ok {
			return fmt.Errorf("block: the operation of a rotation block is %s; it must be %s", c.Ops[0].Op(), OpRotateKey)
		}
	default:
		// "A rotate_key operation MUST appear only in a block whose type is
		// rotation" (spec/02-block-format.md, "Operations" and "rotate_key"):
		// the operation rule the other two block types use does not include it,
		// so a chain ends where the type field says it ends. The loop is
		// vacuous for a private block, whose operations live inside enc; the
		// same rule applies to the payload a holder of the key decrypts.
		for i, op := range c.Ops {
			if _, ok := op.(RotateKey); ok {
				return fmt.Errorf("block: operation %d of a %s block is %s; a rotate_key operation may appear only in a rotation block", i, c.Type, OpRotateKey)
			}
		}
	}
	return nil
}

// SigningValue returns the block's signing input as a dCBOR value: the block
// map with every field except sig (spec/04-cryptography.md, "Signature
// input").
func (c Content) SigningValue() (dcbor.Value, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c.signingValue(), nil
}

// signingValue builds the map. Entry order is irrelevant — dcbor.Encode sorts
// the keys into the canonical bytewise order.
func (c Content) signingValue() dcbor.Map {
	m := dcbor.Map{
		{Key: keyV, Value: dcbor.Uint(c.Version)},
		{Key: keyType, Value: dcbor.Text(string(c.Type))},
		{Key: keyPub, Value: dcbor.Bytes(slices.Clone(c.Pub))},
		{Key: keyPrev, Value: prevValue(c.Prev)},
	}
	if c.Type.hasPlaintextPayload() {
		refs := make(dcbor.Array, 0, len(c.Refs))
		for _, r := range c.Refs {
			refs = append(refs, dcbor.Bytes(r.Bytes()))
		}
		ops := make(dcbor.Array, 0, len(c.Ops))
		for _, op := range c.Ops {
			ops = append(ops, op.Value())
		}
		m = append(m,
			dcbor.MapEntry{Key: keyRefs, Value: refs},
			dcbor.MapEntry{Key: keyTS, Value: dcbor.Uint(c.TS)},
			dcbor.MapEntry{Key: keyOps, Value: ops},
		)
	} else {
		m = append(m,
			dcbor.MapEntry{Key: keyEnc, Value: dcbor.Bytes(slices.Clone(c.Enc))},
			dcbor.MapEntry{Key: keyNonce, Value: dcbor.Bytes(slices.Clone(c.Nonce))},
		)
	}
	return m
}

// SigningBytes returns dCBOR(block without "sig"), the bytes the domain
// separator is prepended to (spec/04-cryptography.md, "Signing procedure").
func (c Content) SigningBytes() ([]byte, error) {
	v, err := c.SigningValue()
	if err != nil {
		return nil, err
	}
	return dcbor.Encode(v)
}

// SigningInput returns the exact byte string an Ed25519 signature over this
// block covers:
//
//	signing_input = "dialog-v1-block" || dCBOR(block without "sig")
func (c Content) SigningInput() ([]byte, error) {
	b, err := c.SigningBytes()
	if err != nil {
		return nil, err
	}
	return append([]byte(DomainSeparator), b...), nil
}

// prevValue encodes the prev field: a 32-byte digest, or null for a genesis
// block.
func prevValue(prev *cid.Digest) dcbor.Value {
	if prev == nil {
		return dcbor.Null
	}
	return dcbor.Bytes(prev.Bytes())
}

// A Block is a signed Dialog block. It is immutable, and its signature has
// been verified against its pub field.
type Block struct {
	content Content
	sig     [SignatureSize]byte
	enc     []byte // canonical dCBOR of the complete block, sig included
}

// Sign builds the block described by c and signs it with priv.
//
// If c.Pub is nil it is filled in from priv; otherwise it must be priv's
// public key, so that a block cannot be signed by a key it does not claim.
func Sign(c Content, priv ed25519.PrivateKey) (*Block, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("block: private key is %d bytes, want %d", len(priv), ed25519.PrivateKeySize)
	}
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok { // unreachable for an ed25519.PrivateKey of the right size
		return nil, fmt.Errorf("block: private key does not yield an Ed25519 public key")
	}
	c = c.Clone()
	switch {
	case c.Pub == nil:
		c.Pub = slices.Clone(pub)
	case !bytes.Equal(c.Pub, pub):
		return nil, fmt.Errorf("block: content claims public key %x but the signing key's is %x", c.Pub, pub)
	}
	input, err := c.SigningInput()
	if err != nil {
		return nil, err
	}
	return assemble(c, ed25519.Sign(priv, input))
}

// Assemble builds a Block from a content and a signature computed elsewhere,
// verifying the signature against c.Pub. It is the constructor for blocks
// whose signing happened outside this process — a hardware key, a test vector,
// another implementation.
func Assemble(c Content, sig []byte) (*Block, error) {
	return assemble(c.Clone(), sig)
}

// assemble takes ownership of c.
func assemble(c Content, sig []byte) (*Block, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if len(sig) != SignatureSize {
		return nil, fmt.Errorf("block: %q is %d bytes, want %d", keySig, len(sig), SignatureSize)
	}
	input, err := c.SigningInput()
	if err != nil {
		return nil, err
	}
	if !ed25519.Verify(ed25519.PublicKey(c.Pub), input, sig) {
		return nil, &SignatureError{Pub: slices.Clone(c.Pub)}
	}

	b := &Block{content: c}
	copy(b.sig[:], sig)
	full := append(c.signingValue(), dcbor.MapEntry{Key: keySig, Value: dcbor.Bytes(slices.Clone(sig))})
	b.enc, err = dcbor.Encode(full)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// A SignatureError reports a block whose signature does not verify against its
// own pub field (spec/02-block-format.md, "Validation" rule 2).
type SignatureError struct {
	// Pub is the public key the block claims.
	Pub ed25519.PublicKey
}

func (e *SignatureError) Error() string {
	return fmt.Sprintf("block: signature does not verify against the block's public key %x", []byte(e.Pub))
}

// Content returns a copy of everything the block's signature covers.
func (b *Block) Content() Content { return b.content.Clone() }

// Type returns the block's type.
func (b *Block) Type() Type { return b.content.Type }

// Version returns the block's protocol version.
func (b *Block) Version() uint64 { return b.content.Version }

// PublicKey returns a copy of the author's Ed25519 public key.
func (b *Block) PublicKey() ed25519.PublicKey { return slices.Clone(b.content.Pub) }

// Signature returns a copy of the block's Ed25519 signature.
func (b *Block) Signature() []byte { return slices.Clone(b.sig[:]) }

// Prev returns the digest of the previous block in this author's chain. ok is
// false for a genesis block, whose prev field is null.
func (b *Block) Prev() (d cid.Digest, ok bool) {
	if b.content.Prev == nil {
		return cid.Digest{}, false
	}
	return *b.content.Prev, true
}

// IsGenesis reports whether the block is the first of its chain, i.e. whether
// its prev field is null.
func (b *Block) IsGenesis() bool { return b.content.Prev == nil }

// Refs returns a copy of the block's foreign block references. It is empty for
// a private block, whose refs are encrypted.
func (b *Block) Refs() []cid.Digest { return slices.Clone(b.content.Refs) }

// TS returns the block's self-reported Unix timestamp. It is untrusted, and
// MUST NOT be used for validation decisions (spec/02-block-format.md).
func (b *Block) TS() uint64 { return b.content.TS }

// Ops returns a copy of the block's operations, in order. It is nil for a
// private block, whose operations are encrypted.
func (b *Block) Ops() []Operation { return slices.Clone(b.content.Ops) }

// Enc returns a copy of a private block's ciphertext, and false for the other
// two types. The bytes are opaque to this package.
func (b *Block) Enc() (enc []byte, ok bool) {
	if b.content.Type != TypePrivate {
		return nil, false
	}
	return slices.Clone(b.content.Enc), true
}

// Nonce returns a copy of a private block's XChaCha20 nonce, and false for the
// other two types.
func (b *Block) Nonce() (nonce []byte, ok bool) {
	if b.content.Type != TypePrivate {
		return nil, false
	}
	return slices.Clone(b.content.Nonce), true
}

// RotateKey returns the rotation block's single rotate_key operation. ok is
// false for any other block type.
func (b *Block) RotateKey() (op RotateKey, ok bool) {
	if b.content.Type != TypeRotation {
		return RotateKey{}, false
	}
	op, ok = b.content.Ops[0].(RotateKey)
	return op, ok
}

// Bytes returns a copy of the block's canonical dCBOR encoding, signature
// included. These are the bytes that go on the wire and the bytes the block's
// digest is taken over.
func (b *Block) Bytes() []byte { return slices.Clone(b.enc) }

// Digest returns SHA-256(dCBOR(block)) — the block's identity, and the value
// another block's prev or refs field carries
// (spec/02-block-format.md, "Block identification").
func (b *Block) Digest() cid.Digest { return cid.SumDigest(b.enc) }

// CID returns the block's external 36-byte content identifier.
func (b *Block) CID() cid.CID { return b.Digest().CID() }

// SigningBytes returns dCBOR(block without "sig"), the bytes behind the domain
// separator in the signature's input.
func (b *Block) SigningBytes() []byte {
	sb, err := b.content.SigningBytes()
	if err != nil { // unreachable: the content validated when the block was built
		panic(err)
	}
	return sb
}

// SigningInput returns "dialog-v1-block" || dCBOR(block without "sig").
func (b *Block) SigningInput() []byte {
	return append([]byte(DomainSeparator), b.SigningBytes()...)
}

// Verify re-checks the block's signature. Every constructor has already done
// so; Verify exists for callers that want the check written down, and for
// validation rule 2.
func (b *Block) Verify() error {
	if !ed25519.Verify(ed25519.PublicKey(b.content.Pub), b.SigningInput(), b.sig[:]) {
		return &SignatureError{Pub: b.PublicKey()}
	}
	return nil
}

// SameAuthor reports whether two blocks carry the same pub key.
func (b *Block) SameAuthor(other *Block) bool {
	return bytes.Equal(b.content.Pub, other.content.Pub)
}

// String renders the block for logs and test failures.
func (b *Block) String() string {
	prev := "genesis"
	if d, ok := b.Prev(); ok {
		prev = "prev " + d.String()[:16]
	}
	return fmt.Sprintf("block(%s, pub %x, %s, %s)", b.content.Type, b.content.Pub[:8], prev, b.CID())
}

package privacy

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"io"
	"math/big"
	"slices"
)

// WrapInfo is the HKDF info string that separates a key-wrapping key from any
// other key derived from the same X25519 agreement
// (spec/04-cryptography.md, "Key management").
const WrapInfo = "dialog-v1-key-wrap"

// WrappedKeySize is the size of a wrapped content key as this package writes
// it: a 24-byte nonce, the 32-byte content key, and the 16-byte Poly1305 tag.
//
// The wrap format is this implementation's, not the protocol's: spec/04 writes
// the step as "wrapped_key = Encrypt(wrapping_key, chain_symmetric_key)" and
// names no algorithm, nonce, AAD or serialization. See todos/046; what is
// implemented here is the defensible reading — the AEAD the protocol already
// mandates, with the nonce it needs carried in front of the ciphertext.
const WrappedKeySize = NonceSize + KeySize + TagSize

// X25519PublicFromEd25519 converts an author's Ed25519 identity key to the
// X25519 key agreement key derived from it, by the birational map of RFC 7748
// §4.1 (spec/04-cryptography.md, "Key management"):
//
//	u = (1 + y) / (1 - y)  (mod 2^255 - 19)
//
// where y is the Edwards y coordinate the Ed25519 public key encodes, little
// endian, with the sign bit of x in the top bit and no part in the map. This is
// libsodium's crypto_sign_ed25519_pk_to_curve25519.
//
// The arithmetic is over public data — a published key — so a variable-time
// big.Int implementation leaks nothing. A key that encodes y = 1, or a y that
// is not reduced modulo p, has no image and is refused.
func X25519PublicFromEd25519(pub ed25519.PublicKey) (*ecdh.PublicKey, error) {
	if len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("privacy: an Ed25519 public key is %d bytes, want %d", len(pub), ed25519.PublicKeySize)
	}
	le := make([]byte, ed25519.PublicKeySize)
	copy(le, pub)
	le[len(le)-1] &= 0x7f // the top bit is the sign of x, not part of y

	y := littleEndianInt(le)
	if y.Cmp(curveP) >= 0 {
		return nil, fmt.Errorf("privacy: Ed25519 public key %x is not a canonical point encoding: y is not reduced modulo 2^255-19", []byte(pub))
	}
	one := big.NewInt(1)
	denominator := new(big.Int).Sub(one, y)
	denominator.Mod(denominator, curveP)
	inverse := new(big.Int).ModInverse(denominator, curveP)
	if inverse == nil {
		return nil, fmt.Errorf("privacy: Ed25519 public key %x has no X25519 image: y = 1 is the identity, whose Montgomery u is at infinity", []byte(pub))
	}
	u := new(big.Int).Add(one, y)
	u.Mul(u, inverse)
	u.Mod(u, curveP)
	return ecdh.X25519().NewPublicKey(littleEndianBytes(u, ed25519.PublicKeySize))
}

// X25519PrivateFromEd25519 converts an Ed25519 private key to the X25519
// private key that agrees with X25519PublicFromEd25519 of its public key.
//
// The scalar is the low half of SHA-512 over the Ed25519 seed, clamped as RFC
// 8032 clamps it before scalar multiplication — libsodium's
// crypto_sign_ed25519_sk_to_curve25519, which spec/04-cryptography.md names as
// a reference implementation. The specification cites RFC 7748 §4.1 for the
// conversion, and that section maps *points*, not scalars; the scalar half of
// the conversion is only pinned by the reference implementation it names. See
// todos/047.
func X25519PrivateFromEd25519(priv ed25519.PrivateKey) (*ecdh.PrivateKey, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("privacy: an Ed25519 private key is %d bytes, want %d", len(priv), ed25519.PrivateKeySize)
	}
	h := sha512.Sum512(priv.Seed())
	scalar := h[:32]
	scalar[0] &= 248
	scalar[31] &= 127
	scalar[31] |= 64
	return ecdh.X25519().NewPrivateKey(scalar)
}

// WrappingKey derives the key that wraps a chain's content key for one
// recipient (spec/04-cryptography.md, "Key management"):
//
//	shared_secret = X25519(Ed25519_to_X25519(own_sk), Ed25519_to_X25519(peer_pk))
//	wrapping_key  = HKDF-SHA-256(salt: empty, ikm: shared_secret,
//	                             info: "dialog-v1-key-wrap", length: 32)
//
// The agreement is static-static and symmetric: an author calls it with their
// own private key and the recipient's public key, the recipient with theirs and
// the author's, and both arrive at the same 32 bytes. Nothing else varies, so
// one pair of keys has exactly one wrapping key, for every chain and every
// block — which is why each wrap takes a fresh nonce.
//
// The empty salt is the specification's word; RFC 5869 substitutes HashLen
// zeros for an absent salt, and HMAC pads either to the same block of zeros, so
// the two readings coincide.
func WrappingKey(own ed25519.PrivateKey, peer ed25519.PublicKey) ([]byte, error) {
	ownX, err := X25519PrivateFromEd25519(own)
	if err != nil {
		return nil, err
	}
	peerX, err := X25519PublicFromEd25519(peer)
	if err != nil {
		return nil, err
	}
	shared, err := ownX.ECDH(peerX)
	if err != nil {
		// crypto/ecdh rejects an all-zero agreement, which is what a low-order
		// peer key produces.
		return nil, fmt.Errorf("privacy: X25519 agreement with %x failed: %w", []byte(peer), err)
	}
	key, err := hkdf.Key(sha256.New, shared, []byte{}, WrapInfo, KeySize)
	if err != nil {
		return nil, fmt.Errorf("privacy: deriving the wrapping key: %w", err)
	}
	return key, nil
}

// Wrap encrypts a chain's content key for one recipient, so that the recipient
// can read the chain's private blocks.
//
// The returned value is WrappedKeySize bytes: the 24-byte nonce followed by the
// XChaCha20-Poly1305 ciphertext of the content key under the wrapping key, with
// no additional data. Only the wrapping-key derivation is specified — see
// WrappedKeySize and todos/046 — so this layout is an implementation choice,
// and so is anything that carries it: the protocol says the distribution
// mechanism is out of scope (spec/04-cryptography.md, "Key management").
//
// The nonce is read from random, or from crypto/rand.Reader when random is
// nil. A fresh one per wrap is required, not optional: one pair of identities
// has one wrapping key for all time, so a repeated nonce would repeat a
// keystream.
func Wrap(key Key, authorPriv ed25519.PrivateKey, recipient ed25519.PublicKey, random io.Reader) ([]byte, error) {
	wrappingKey, err := WrappingKey(authorPriv, recipient)
	if err != nil {
		return nil, err
	}
	nonce, err := randomBytes(random, NonceSize)
	if err != nil {
		return nil, fmt.Errorf("privacy: generating a key-wrap nonce: %w", err)
	}
	aead, err := newXChaCha(wrappingKey)
	if err != nil {
		return nil, err
	}
	wrapped := make([]byte, 0, WrappedKeySize)
	wrapped = append(wrapped, nonce...)
	return aead.Seal(wrapped, nonce, key[:], nil), nil
}

// Unwrap recovers a chain's content key from a wrapped copy addressed to the
// caller. A wrapped key that does not authenticate — the wrong recipient, the
// wrong author, a tampered blob — is ErrAuthentication.
func Unwrap(wrapped []byte, recipientPriv ed25519.PrivateKey, author ed25519.PublicKey) (Key, error) {
	var k Key
	if len(wrapped) != WrappedKeySize {
		return k, fmt.Errorf("privacy: a wrapped key is %d bytes, want %d", len(wrapped), WrappedKeySize)
	}
	wrappingKey, err := WrappingKey(recipientPriv, author)
	if err != nil {
		return k, err
	}
	aead, err := newXChaCha(wrappingKey)
	if err != nil {
		return k, err
	}
	plaintext, err := aead.Open(nil, wrapped[:NonceSize], wrapped[NonceSize:], nil)
	if err != nil {
		return k, fmt.Errorf("%w: this wrapped key does not open for this recipient and author", ErrAuthentication)
	}
	return ParseKey(plaintext)
}

// A Recipient is a chain's content key wrapped for one reader, together with
// the Ed25519 identity it was wrapped for.
//
// The pairing is this package's convenience, not a protocol structure: a reader
// who is handed a pile of wrapped keys has no other way to tell which is theirs,
// and the protocol defines no envelope to say so (spec/04-cryptography.md,
// "Key management": the distribution mechanism is out of scope).
type Recipient struct {
	// Pub is the reader's Ed25519 public key — their Dialog identity.
	Pub ed25519.PublicKey
	// Wrapped is the wrapped content key, WrappedKeySize bytes.
	Wrapped []byte
}

// WrapFor wraps the content key once for each recipient. Recipients are
// returned in the order given, and a recipient listed twice is wrapped twice,
// under different nonces.
func WrapFor(key Key, authorPriv ed25519.PrivateKey, recipients []ed25519.PublicKey, random io.Reader) ([]Recipient, error) {
	out := make([]Recipient, 0, len(recipients))
	for i, pub := range recipients {
		wrapped, err := Wrap(key, authorPriv, pub, random)
		if err != nil {
			return nil, fmt.Errorf("privacy: recipient %d: %w", i, err)
		}
		out = append(out, Recipient{Pub: slices.Clone(pub), Wrapped: wrapped})
	}
	return out, nil
}

// Open recovers the content key from a wrapped copy, given the recipient's own
// private key and the author's public key.
func (r Recipient) Open(recipientPriv ed25519.PrivateKey, author ed25519.PublicKey) (Key, error) {
	return Unwrap(r.Wrapped, recipientPriv, author)
}

// curveP is 2^255 - 19, the field of Curve25519.
var curveP = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(19))

// littleEndianInt reads a little-endian byte string as an integer.
func littleEndianInt(b []byte) *big.Int {
	be := slices.Clone(b)
	slices.Reverse(be)
	return new(big.Int).SetBytes(be)
}

// littleEndianBytes writes an integer as a little-endian byte string of
// exactly n bytes.
func littleEndianBytes(v *big.Int, n int) []byte {
	out := make([]byte, n)
	v.FillBytes(out)
	slices.Reverse(out)
	return out
}

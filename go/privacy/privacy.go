// Package privacy implements the private-block encryption of
// spec/04-cryptography.md: the AEAD that hides a block's refs, ts and ops, and
// the per-recipient wrapping that shares the key which opens it.
//
// # What a private block hides
//
// A private block keeps only its chain-management fields in the clear — v,
// type, pub, sig and prev — so that any node can check its signature and its
// place in the author's chain. Everything else is encrypted into enc:
//
//	plaintext  = dCBOR({"refs": refs, "ts": ts, "ops": ops})
//	aad        = dCBOR({"v": v, "type": "private", "pub": pub, "prev": prev})
//	enc        = XChaCha20-Poly1305(key, nonce, plaintext, aad)
//
// The AAD is every plaintext field but sig, enc and nonce, which binds the
// ciphertext to the block's position in its chain: an enc lifted from one block
// into another fails authentication before it is ever decoded. The block's
// signature covers enc and nonce in turn, so the two directions meet.
//
// # Where the boundary runs
//
// This package holds keys and this package alone. The block package treats enc
// and nonce as opaque bytes; it signs them, hashes them, and validates the six
// rules that need no key. What comes back out of Open is a block.Payload, which
// block.ValidatePayload takes for the other four (4, 5, 6 and 10). OpenAndValidate
// runs both halves and is the ordinary way to validate a private block one
// holds the key for.
//
// # Keys
//
// A chain's content key is symmetric and per chain, not per block
// (spec/04-cryptography.md, "Encryption scheme"), and every block of the chain
// takes a fresh nonce. Sharing that key with a reader is Wrap's business: an
// X25519 agreement between the author's key and the reader's, both converted
// from their Ed25519 identities, feeding HKDF-SHA-256. How the wrapped key then
// travels is out of scope for the protocol and for this package
// (spec/04-cryptography.md, "Key management").
//
// The key's lifecycle is the caller's to decide, within what v1 allows
// (spec/04-cryptography.md, "Content key lifecycle"): a successor chain MAY
// reuse the key of the chain it succeeds or take a fresh one, and nothing in a
// block records which. This package takes the key as a parameter everywhere and
// holds no opinion — a Builder that succeeds a rotation is handed whichever key
// its author chose. What v1 does not offer is re-keying, revocation or a key
// identifier, so a reader who has the key reads the chain for as long as the
// chain lasts.
package privacy

import (
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"slices"

	"golang.org/x/crypto/chacha20poly1305"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
	"github.com/vrinek/Dialog/go/dcbor"
)

// Sizes fixed by spec/04-cryptography.md, "Encryption scheme".
const (
	// KeySize is the size of a chain's symmetric content key: 256 bits.
	KeySize = chacha20poly1305.KeySize
	// NonceSize is the size of a private block's nonce: 192 bits, the
	// extended nonce of XChaCha20-Poly1305. It is block.NonceSize.
	NonceSize = chacha20poly1305.NonceSizeX
	// TagSize is the size of the Poly1305 authentication tag the AEAD appends
	// to every ciphertext. It is block.MinEncSize: a shorter enc field cannot
	// be a ciphertext at all.
	TagSize = chacha20poly1305.Overhead
)

// The plaintext block keys the AAD covers. They are the same strings the block
// package writes; spec/04-cryptography.md, "Encryption procedure", fixes the
// set as "all plaintext block fields (excluding sig, enc, and nonce)".
const (
	keyV    = "v"
	keyType = "type"
	keyPub  = "pub"
	keyPrev = "prev"
)

// ErrAuthentication reports that a ciphertext did not authenticate: the wrong
// key, a tampered enc or nonce, or an AAD that does not match the block the
// ciphertext arrived in. spec/04-cryptography.md, "Decryption procedure",
// requires the block to be rejected in every one of those cases, and the
// AEAD cannot tell them apart — that is what a MAC is.
var ErrAuthentication = errors.New("privacy: the ciphertext did not authenticate")

// A Key is a chain's 256-bit symmetric content key. The same key encrypts every
// private block of the chain; the nonce is what differs between blocks
// (spec/04-cryptography.md, "Encryption scheme").
//
// One key per chain does not mean one chain per key: two chains may be
// encrypted under the same key, and a successor chain may inherit the key of
// the chain it succeeds (spec/04-cryptography.md, "Content key lifecycle").
// Anyone holding a Key reads that chain entire — there is no forward secrecy
// and no way to revoke a reader in v1 — which is why String does not print it.
type Key [KeySize]byte

// GenerateKey returns a fresh content key read from random, or from
// crypto/rand.Reader when random is nil.
func GenerateKey(random io.Reader) (Key, error) {
	var k Key
	b, err := randomBytes(random, KeySize)
	if err != nil {
		return k, fmt.Errorf("privacy: generating a content key: %w", err)
	}
	copy(k[:], b)
	return k, nil
}

// ParseKey returns the content key held in b, which must be exactly KeySize
// bytes.
func ParseKey(b []byte) (Key, error) {
	var k Key
	if len(b) != KeySize {
		return k, fmt.Errorf("privacy: a content key is %d bytes, want %d", len(b), KeySize)
	}
	copy(k[:], b)
	return k, nil
}

// Bytes returns a copy of the key's raw bytes.
func (k Key) Bytes() []byte { return slices.Clone(k[:]) }

// String deliberately does not print the key. A content key in a log is the
// chain in the clear, past and future, which spec/04-cryptography.md,
// "Security Considerations", forbids putting in a durable store not meant for
// key material.
func (k Key) String() string { return "privacy.Key(redacted)" }

// A Header is the part of a private block the AAD covers: every plaintext
// field except sig, enc and nonce (spec/04-cryptography.md, "Encryption
// procedure"). The type field is not among them because it is fixed — an AAD
// is only ever computed for a private block.
type Header struct {
	// Version is the block's v field.
	Version uint64
	// Pub is the author's raw 32-byte Ed25519 public key.
	Pub ed25519.PublicKey
	// Prev is the digest of the previous block in the author's chain, or nil
	// for a genesis block.
	Prev *cid.Digest
}

// HeaderOf returns the header of a private block. It is an error to ask for
// the header of a block of any other type: no other type has an AAD, because
// no other type encrypts anything.
func HeaderOf(b *block.Block) (Header, error) {
	if b == nil {
		return Header{}, fmt.Errorf("privacy: HeaderOf called with a nil block")
	}
	if b.Type() != block.TypePrivate {
		return Header{}, fmt.Errorf("privacy: %s is a %s block; only a private block carries an encrypted payload", b.CID(), b.Type())
	}
	h := Header{Version: b.Version(), Pub: b.PublicKey()}
	if prev, ok := b.Prev(); ok {
		h.Prev = &prev
	}
	return h, nil
}

// validate reports whether the header can be encoded.
func (h Header) validate() error {
	if h.Version != block.Version {
		return fmt.Errorf("privacy: unrecognized protocol version %d, want %d", h.Version, block.Version)
	}
	if len(h.Pub) != block.PublicKeySize {
		return fmt.Errorf("privacy: %q is %d bytes, want %d", keyPub, len(h.Pub), block.PublicKeySize)
	}
	return nil
}

// AAD returns the Additional Authenticated Data of a private block:
//
//	dCBOR({"v": v, "type": "private", "pub": pub, "prev": prev})
//
// It binds the ciphertext to the block's metadata, so that a payload cannot be
// moved to another position in a chain, to another author's chain, or to
// another protocol version and still open. dCBOR fixes the key order, so the
// encoding is unambiguous (spec/04-cryptography.md, "Encryption procedure").
func (h Header) AAD() ([]byte, error) {
	if err := h.validate(); err != nil {
		return nil, err
	}
	prev := dcbor.Value(dcbor.Null)
	if h.Prev != nil {
		prev = dcbor.Bytes(h.Prev.Bytes())
	}
	return dcbor.Encode(dcbor.Map{
		{Key: keyV, Value: dcbor.Uint(h.Version)},
		{Key: keyType, Value: dcbor.Text(string(block.TypePrivate))},
		{Key: keyPub, Value: dcbor.Bytes(slices.Clone(h.Pub))},
		{Key: keyPrev, Value: prev},
	})
}

// Seal encrypts a private block's payload under key, returning the enc and
// nonce fields of the block that carries it.
//
// The nonce is read from random — crypto/rand.Reader when random is nil — and
// MUST be unique for every block encrypted under one key: reusing one breaks
// confidentiality outright (spec/04-cryptography.md, "Security
// Considerations"). A 192-bit random nonce makes a collision negligible, which
// is why the extended-nonce variant is the one the protocol specifies.
//
// The header must describe the block the ciphertext will be signed into,
// prev included; SealBlock takes that off the caller's hands.
func Seal(h Header, key Key, p block.Payload, random io.Reader) (enc, nonce []byte, err error) {
	aad, err := h.AAD()
	if err != nil {
		return nil, nil, err
	}
	plaintext, err := p.Encode()
	if err != nil {
		return nil, nil, err
	}
	aead, err := newXChaCha(key[:])
	if err != nil { // unreachable: Key is KeySize by construction
		return nil, nil, err
	}
	if nonce, err = randomBytes(random, NonceSize); err != nil {
		return nil, nil, fmt.Errorf("privacy: generating a nonce: %w", err)
	}
	return aead.Seal(nil, nonce, plaintext, aad), nonce, nil
}

// SealBlock encrypts the payload and signs the private block that carries it
// onto the builder's chain.
//
// The AAD covers prev, so the ciphertext is bound to the position the block is
// about to take: this reads that position from the builder rather than asking
// the caller to keep the two in step.
func SealBlock(b *block.Builder, key Key, p block.Payload, random io.Reader) (*block.Block, error) {
	if b == nil {
		return nil, fmt.Errorf("privacy: SealBlock called with a nil builder")
	}
	h := Header{Version: block.Version, Pub: b.PublicKey()}
	if tip, ok := b.Tip(); ok {
		h.Prev = &tip
	}
	enc, nonce, err := Seal(h, key, p, random)
	if err != nil {
		return nil, err
	}
	return b.Private(enc, nonce)
}

// Open decrypts a private block and returns its payload.
//
// Authentication failure — a wrong key, a tampered enc or nonce, or any change
// to the fields the AAD covers — is ErrAuthentication, and the block MUST be
// rejected (spec/04-cryptography.md, "Decryption procedure"). A plaintext that
// authenticates but is not canonical dCBOR, or is not a well-formed payload, is
// a different error and an equally firm rejection: authenticity is not
// well-formedness, and the strict decoding rules apply to what comes out of the
// AEAD exactly as they apply to what arrives on the wire.
//
// What Open does not do is validate the payload against the rest of the graph.
// That is block.ValidatePayload, or OpenAndValidate for both at once.
func Open(b *block.Block, key Key) (block.Payload, error) {
	h, err := HeaderOf(b)
	if err != nil {
		return block.Payload{}, err
	}
	aad, err := h.AAD()
	if err != nil {
		return block.Payload{}, err
	}
	enc, _ := b.Enc()
	nonce, _ := b.Nonce()
	if len(nonce) != NonceSize { // unreachable: a *Block has been validated
		return block.Payload{}, fmt.Errorf("privacy: nonce is %d bytes, want %d", len(nonce), NonceSize)
	}
	aead, err := newXChaCha(key[:])
	if err != nil { // unreachable: Key is KeySize by construction
		return block.Payload{}, err
	}
	plaintext, err := aead.Open(nil, nonce, enc, aad)
	if err != nil {
		return block.Payload{}, fmt.Errorf("%w: block %s cannot be opened with this key", ErrAuthentication, b.CID())
	}
	p, err := block.DecodePayload(plaintext)
	if err != nil {
		return block.Payload{}, fmt.Errorf("privacy: block %s: %w", b.CID(), err)
	}
	return p, nil
}

// A KeyRing is a set of chain content keys, and the way a caller lends them to
// the block package's reference resolution: it implements block.Decrypter, so
// that a private block's own private ancestors — and any private block its refs
// name that the ring can open — define the entities its operations reference.
//
// A block is tried against every key in the ring, in order, and the first that
// authenticates wins. This is what the protocol expects of a node holding
// several keys: a private block's plaintext fields name no key, and trial
// decryption is exact because the 16-byte Poly1305 tag makes a wrong key a
// certain and cheap miss (spec/04-cryptography.md, "Content key lifecycle").
// A ring is small — one key per private chain a node follows — and a caller
// that tracks the mapping from a chain's pub to its key itself can hand
// block.Options a Decrypter that does the lookup instead.
type KeyRing struct{ keys []Key }

// NewKeyRing returns a ring holding the given keys.
func NewKeyRing(keys ...Key) *KeyRing { return &KeyRing{keys: slices.Clone(keys)} }

// Add adds a key to the ring.
func (r *KeyRing) Add(keys ...Key) { r.keys = append(r.keys, keys...) }

// Len returns the number of keys in the ring.
func (r *KeyRing) Len() int { return len(r.keys) }

// DecryptPayload implements block.Decrypter. A block no key in the ring opens
// is reported as unreadable (ok false), not as an error: not holding a key is
// the ordinary condition of most nodes for most blocks. A block that opens but
// whose plaintext is malformed is an error — that block is broken, not foreign.
func (r *KeyRing) DecryptPayload(b *block.Block) (block.Payload, bool, error) {
	if b == nil || b.Type() != block.TypePrivate {
		return block.Payload{}, false, nil
	}
	for _, key := range r.keys {
		p, err := Open(b, key)
		if err == nil {
			return p, true, nil
		}
		if !errors.Is(err, ErrAuthentication) {
			return block.Payload{}, false, err
		}
	}
	return block.Payload{}, false, nil
}

// OpenAndValidate is the whole of a private block's validation for a node that
// holds the key: block.Validate for the rules that need no plaintext (1, 2, 3,
// 7, 8 and 9), then Open, then block.ValidatePayload for the four that do (4,
// 5, 6 and 10).
//
// Reference resolution needs to read the author's earlier private blocks, since
// that is where a private chain's own entities are defined, so this key is lent
// to it as a KeyRing unless the caller has supplied a Decrypter of their own —
// a node following several private chains passes a ring holding all of their
// keys.
//
// The returned report is the two merged, with nothing left in Unchecked — a
// key holder checks every rule — and without the notice block.Validate leaves
// for the node that cannot.
func OpenAndValidate(b *block.Block, key Key, src block.Source, opts *block.Options) (block.Payload, *block.Report, error) {
	if opts == nil || opts.Decrypter == nil {
		withRing := block.Options{Decrypter: NewKeyRing(key)}
		if opts != nil {
			withRing.ScanLimit = opts.ScanLimit
		}
		opts = &withRing
	}
	structural, err := block.Validate(b, src, opts)
	if err != nil {
		return block.Payload{}, nil, err
	}
	p, err := Open(b, key)
	if err != nil {
		return block.Payload{}, nil, err
	}
	payload, err := block.ValidatePayload(b, p, src, opts)
	if err != nil {
		return block.Payload{}, nil, err
	}

	report := &block.Report{Forks: structural.Forks, Scanned: payload.Scanned}
	for _, w := range structural.Warnings {
		if w.Rule == 0 && w.Msg == block.PrivateBlockNotice {
			continue // answered by the pass that just ran
		}
		report.Warnings = append(report.Warnings, w)
	}
	report.Warnings = append(report.Warnings, payload.Warnings...)
	report.Forks = append(report.Forks, payload.Forks...)
	return p, report, nil
}

// newXChaCha returns the AEAD the protocol specifies, XChaCha20-Poly1305, for
// a 32-byte key (spec/04-cryptography.md, "Encryption scheme").
func newXChaCha(key []byte) (cipher.AEAD, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("privacy: %w", err)
	}
	return aead, nil
}

// randomBytes reads n bytes from random, or from crypto/rand.Reader when
// random is nil. An injectable source is what makes a deterministic test
// possible; production code passes nil.
func randomBytes(random io.Reader, n int) ([]byte, error) {
	if random == nil {
		random = rand.Reader
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(random, b); err != nil {
		return nil, err
	}
	return b, nil
}

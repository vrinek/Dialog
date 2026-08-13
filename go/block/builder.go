package block

import (
	"crypto/ed25519"
	"fmt"
	"slices"

	"github.com/vrinek/Dialog/go/cid"
)

// A Builder is the author side of a chain: it holds one signing key, remembers
// the tip of the chain that key is writing, and signs each new block onto it.
//
// The first block a Builder produces is a genesis block (prev null); every
// later one links to the block before it. A rotation block ends the chain, and
// the Builder refuses to sign anything after it — spec/02-block-format.md
// requires that no further block signed by the old key be accepted, and an
// author has no reason to produce one.
//
// A Builder is not safe for concurrent use.
type Builder struct {
	priv        ed25519.PrivateKey
	pub         ed25519.PublicKey
	tip         *cid.Digest
	started     bool
	rotated     bool
	genesisRefs []cid.Digest
}

// NewBuilder returns a Builder signing with priv.
func NewBuilder(priv ed25519.PrivateKey) (*Builder, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("block: private key is %d bytes, want %d", len(priv), ed25519.PrivateKeySize)
	}
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok { // unreachable for a key of the right size
		return nil, fmt.Errorf("block: private key does not yield an Ed25519 public key")
	}
	return &Builder{priv: slices.Clone(priv), pub: slices.Clone(pub)}, nil
}

// PublicKey returns the author's public key — the identity of the chain this
// Builder writes.
func (b *Builder) PublicKey() ed25519.PublicKey { return slices.Clone(b.pub) }

// Tip returns the digest of the last block the Builder signed. ok is false
// before the genesis block.
func (b *Builder) Tip() (cid.Digest, bool) {
	if b.tip == nil {
		return cid.Digest{}, false
	}
	return *b.tip, true
}

// Succeeds records that this Builder's chain continues the chain that ended
// with the given rotation block, so that its genesis block references the
// rotation block in refs. spec/02-block-format.md, "rotate_key", makes that
// reference a SHOULD — it is what makes key succession verifiable — and this
// is how an author honours it.
//
// It is an error to call Succeeds after the genesis block has been signed, or
// with a block that is not a rotation block naming this Builder's key.
func (b *Builder) Succeeds(rotation *Block) error {
	if b.started {
		return fmt.Errorf("block: Succeeds must be called before the genesis block is signed")
	}
	op, ok := rotation.RotateKey()
	if !ok {
		return fmt.Errorf("block: %s is not a rotation block, so no chain succeeds it", rotation)
	}
	if !slices.Equal(op.NewPublicKey(), b.pub) {
		return fmt.Errorf("block: rotation block names %x as the new key, not this builder's %x", op.NewPublicKey()[:8], b.pub[:8])
	}
	d := rotation.Digest()
	if !slices.Contains(b.genesisRefs, d) {
		b.genesisRefs = append(b.genesisRefs, d)
	}
	return nil
}

// Public signs a public block carrying ops onto the chain.
func (b *Builder) Public(ts uint64, refs []cid.Digest, ops ...Operation) (*Block, error) {
	return b.build(Content{Type: TypePublic, TS: ts, Refs: refs, Ops: ops})
}

// Rotation signs the rotation block that ends this chain and names newPub as
// the author's next key. The Builder signs nothing after it.
func (b *Builder) Rotation(ts uint64, refs []cid.Digest, newPub ed25519.PublicKey) (*Block, error) {
	op, err := NewRotateKey(newPub)
	if err != nil {
		return nil, err
	}
	blk, err := b.build(Content{Type: TypeRotation, TS: ts, Refs: refs, Ops: []Operation{op}})
	if err != nil {
		return nil, err
	}
	b.rotated = true
	return blk, nil
}

// Private signs a private block carrying an already-encrypted payload. The
// ciphertext and nonce come from the privacy package; this package signs and
// links them without reading them. The ciphertext must be at least MinEncSize
// bytes — the Poly1305 tag alone is that long — and the nonce exactly
// NonceSize.
func (b *Builder) Private(enc, nonce []byte) (*Block, error) {
	return b.build(Content{Type: TypePrivate, Enc: enc, Nonce: nonce})
}

// build fills in the fields the Builder owns — version, public key and prev —
// signs the block and advances the tip.
func (b *Builder) build(c Content) (*Block, error) {
	if b.rotated {
		return nil, fmt.Errorf("block: this chain ended with a rotation block; further blocks signed by %x must not be accepted", b.pub[:8])
	}
	c.Version = Version
	c.Pub = slices.Clone(b.pub)
	if b.tip != nil {
		prev := *b.tip
		c.Prev = &prev
	} else if len(b.genesisRefs) > 0 {
		// The genesis block of a successor chain references the rotation block
		// that ended the previous one.
		c.Refs = append(slices.Clone(b.genesisRefs), c.Refs...)
	}
	blk, err := Sign(c, b.priv)
	if err != nil {
		return nil, err
	}
	d := blk.Digest()
	b.tip, b.started = &d, true
	return blk, nil
}

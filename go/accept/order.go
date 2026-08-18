package accept

import (
	"bytes"
	"errors"
	"fmt"
	"slices"

	"github.com/vrinek/Dialog/go/block"
	"github.com/vrinek/Dialog/go/cid"
)

// A position locates a block in its lineage: which lineage, and how many blocks
// precede it there. It is the "block order" of
// spec/05-processing-model.md, "Assertion order":
//
//	Block order is the position of the block a meta-molecule was published in
//	within its author chain: the sequence the prev field defines, from the
//	genesis block to the tip.
//
// Two positions are comparable only within one lineage. Nothing orders the
// blocks of two different authors against each other, and nothing here tries:
// "the assertions of two different authors are not ordered against each other"
// (same section).
type position struct {
	// lineage is the digest of the block the lineage begins at — the genesis
	// block of the oldest chain in it. It identifies the lineage and nothing
	// else; it is not a key.
	lineage cid.Digest
	// index counts blocks from that genesis block, which is index 0.
	index int
}

// A lineage is one logical author's whole block sequence: a chain, extended
// backwards through every key succession this node can verify. "Block order
// continues across a key rotation: every block of a successor chain comes after
// every block of the chain it succeeds, the two being joined by the reference
// the successor's genesis block carries" (spec/05-processing-model.md,
// "Assertion order").
//
// Joining the two chains is an identity decision, and "author identity (mapping
// multiple keys to a single author) is implementation-scoped"
// (spec/05-processing-model.md, "Chain succession"). This is the choice this
// implementation makes, and it is deliberately narrow:
//
//   - It affects ORDER ONLY. "Continuing the order across a rotation is an
//     ordering rule and not an identity rule" (spec/05-processing-model.md,
//     "Assertion order"): filtering stays strictly per key, so a successor
//     chain's entities reach a view when the successor key is subscribed and
//     not because the key before it was. What carries a subscription across a
//     rotation is the L1 SHOULD to auto-subscribe to the successor chain
//     (spec/05-processing-model.md, "Chain succession (key rotation)", step 3);
//     a node that follows it calls Subscriptions.Subscribe with the new key.
//   - It rests on the same evidence L1 validates a succession with — a public
//     genesis block whose refs name a rotation block that appoints its key
//     (spec/02-block-format.md, "Verifiable succession") — and on nothing else.
//   - An ambiguous succession joins nothing. More than one claimant, and the
//     order through the junction is ambiguous too, so the two chains stay
//     separate lineages and the ambiguity is surfaced as a conflict.
type blockOrder struct {
	src block.Source
	pos map[cid.Digest]position
	// reported keeps an ambiguity from being surfaced twice, keyed by the
	// block the ambiguity was found at.
	reported  map[cid.Digest]bool
	conflicts []Conflict
}

func newBlockOrder(src block.Source) *blockOrder {
	return &blockOrder{
		src:      src,
		pos:      make(map[cid.Digest]position),
		reported: make(map[cid.Digest]bool),
	}
}

// of returns the position of a block, walking prev to the start of its lineage
// and memoizing every block on the way, so that placing a whole chain costs one
// walk rather than one per block.
//
// The block and its ancestors must be in the source. A block whose entities are
// in L2 is by definition a block the node validated and holds at L1 — "for each
// valid block in L1" (spec/05-processing-model.md, "Accumulation rules") — so a
// missing one is an inconsistency between the two layers rather than a state
// this package can interpret, and it is reported as an error wrapping
// block.ErrNotFound.
func (o *blockOrder) of(d cid.Digest) (position, error) {
	if p, ok := o.pos[d]; ok {
		return p, nil
	}
	var (
		path []cid.Digest // from d backwards, exclusive of the block that anchors it
		seen = make(map[cid.Digest]bool)
		cur  = d
		base position
	)
	for {
		if p, ok := o.pos[cur]; ok {
			base = p
			break
		}
		if seen[cur] {
			// Impossible for honest content-addressed blocks — a prev cycle
			// would need a SHA-256 preimage — but a Source is an interface,
			// and a buggy one must not spin this loop forever.
			return position{}, fmt.Errorf("accept: the chain reaching block %s revisits %s; prev links must form a list, not a cycle", d, cur)
		}
		seen[cur] = true
		b, err := o.src.Block(cur)
		if err != nil {
			return position{}, fmt.Errorf("accept: block order needs block %s, which the source does not hold: %w", cur, err)
		}
		if got := b.Digest(); got != cur {
			return position{}, fmt.Errorf("accept: the source returned block %s for digest %s", got, cur)
		}
		path = append(path, cur)
		if prev, ok := b.Prev(); ok {
			cur = prev
			continue
		}
		rotation, ok, err := o.rotationBehind(b)
		if err != nil {
			return position{}, err
		}
		if ok {
			cur = rotation
			continue
		}
		// b begins its lineage: it is a genesis block that succeeds nothing
		// this node can verify.
		base = position{lineage: cur, index: 0}
		o.pos[cur] = base
		path = path[:len(path)-1]
		break
	}
	for i := len(path) - 1; i >= 0; i-- {
		base = position{lineage: base.lineage, index: base.index + 1}
		o.pos[path[i]] = base
	}
	return o.pos[d], nil
}

// rotationBehind returns the rotation block a genesis block succeeds, if this
// node can verify one and it is unambiguous.
//
// The evidence is the one spec/02-block-format.md, "Verifiable succession",
// defines: a public genesis block whose refs name a rotation block whose
// rotate_key operation appoints the genesis block's own key. The new key's
// signature covers that reference, which is what makes the succession checkable
// from the blocks alone.
func (o *blockOrder) rotationBehind(genesis *block.Block) (cid.Digest, bool, error) {
	if genesis.Type() != block.TypePublic {
		// Only a public block can begin a successor chain, and a private
		// block's refs are inside its ciphertext anyway.
		return cid.Digest{}, false, nil
	}
	var claims []cid.Digest
	for _, ref := range genesis.Refs() {
		b, err := o.src.Block(ref)
		if err != nil {
			if errors.Is(err, block.ErrNotFound) {
				continue // a ref this node does not hold proves nothing
			}
			return cid.Digest{}, false, fmt.Errorf("accept: reading ref %s of block %s: %w", ref, genesis.Digest(), err)
		}
		op, ok := b.RotateKey()
		if !ok {
			continue
		}
		if !bytes.Equal(op.NewPublicKey(), genesis.PublicKey()) {
			continue
		}
		claims = append(claims, ref)
	}
	switch len(claims) {
	case 0:
		return cid.Digest{}, false, nil
	case 1:
	default:
		// One genesis block claiming to continue two chains. Whichever it
		// meant, the order through the junction is not determined, so it
		// joins nothing.
		o.reportAmbiguity(genesis.Digest(), claims)
		return cid.Digest{}, false, nil
	}
	rotation := claims[0]
	rb, err := o.src.Block(rotation)
	if err != nil {
		return cid.Digest{}, false, fmt.Errorf("accept: reading rotation block %s: %w", rotation, err)
	}
	// "If more than one genesis block references the same rotation block, the
	// succession is ambiguous" (spec/05-processing-model.md, "Chain
	// succession"). Answering that needs the refs graph read backwards; a
	// source that cannot do it simply cannot detect this case, and says so by
	// not implementing block.Referrers.
	successors, fork, err := block.Successors(rb, o.src)
	if err == nil && fork != nil {
		o.reportAmbiguity(rotation, successors)
		return cid.Digest{}, false, nil
	}
	return rotation, true, nil
}

// reportAmbiguity records an ambiguous succession once, whichever way it was
// reached. at is the block the ambiguity was found at, and blocks are the
// claimants it is between.
func (o *blockOrder) reportAmbiguity(at cid.Digest, blocks []cid.Digest) {
	if o.reported[at] {
		return
	}
	o.reported[at] = true

	var (
		involved digestSet
		signers  keySet
	)
	involved.add(at)
	for _, d := range blocks {
		involved.add(d)
	}
	for _, d := range involved.list() {
		b, err := o.src.Block(d)
		if err != nil {
			continue
		}
		if k, ok := keyOf(b.PublicKey()); ok {
			signers.add(k)
		}
	}
	o.conflicts = append(o.conflicts, Conflict{
		Kind:      ConflictAmbiguousSuccession,
		Blocks:    involved.list(),
		Declarers: signers.public(),
	})
}

// compareDigests orders digests bytewise.
func compareDigests(a, b cid.Digest) int { return bytes.Compare(a[:], b[:]) }

// insertDigest keeps an ascending, duplicate-free slice.
func insertDigest(s []cid.Digest, d cid.Digest) []cid.Digest {
	i, found := slices.BinarySearchFunc(s, d, compareDigests)
	if found {
		return s
	}
	return slices.Insert(s, i, d)
}
